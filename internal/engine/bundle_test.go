package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sheep-get/internal/task"
)

// 成员下载用 If-Unmodified-Since 校验版本，探测与下载拿到的 Last-Modified 必须一致。
// ServeContent 把传入的 modtime 写进该响应头，这里用固定时间：用 time.Now() 时探测与
// 下载一旦跨秒，成员就会被判成“文件已变”（Windows CI 上已复现）。
var bundleTestModTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

type bundleTestServer struct {
	*httptest.Server
	files map[string][]byte
	fail  atomic.Bool
}

func newBundleTestServer(t *testing.T) *bundleTestServer {
	t.Helper()
	ts := &bundleTestServer{files: make(map[string][]byte)}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		data, ok := ts.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if ts.fail.Load() && strings.HasSuffix(name, "fail.txt") {
			http.Error(w, "boom", http.StatusForbidden)
			return
		}
		http.ServeContent(w, r, name, bundleTestModTime, bytes.NewReader(data))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func newBundleTestManager(t *testing.T, tempDir string) *Manager {
	t.Helper()
	store, err := task.NewFileTaskStore(filepath.Join(tempDir, "tasks.json"))
	if err != nil {
		t.Fatalf("new task store: %v", err)
	}
	m := NewManager(store, NewHTTPDownloader(nil), Config{
		TempDirectory:             filepath.Join(tempDir, "temp"),
		MaxActiveTasks:            1,
		DefaultConnectionsPerTask: 4,
	})
	t.Cleanup(m.Close)
	return m
}

func registerBundleMember(t *testing.T, m *Manager, dir, baseURL, name, zipID, zipName, entry string, final bool) *BundleRegistration {
	t.Helper()
	reg, err := m.RegisterBundleMember(context.Background(), BundleRegisterRequest{
		ZipID:     zipID,
		ZipName:   zipName,
		Directory: dir,
		Final:     final,
		MaxConn:   4,
		Member: BundleMemberRequest{
			URL:       baseURL + "/" + name,
			Filename:  name,
			EntryPath: entry,
		},
	})
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	return reg
}

func waitBundleStatus(t *testing.T, m *Manager, id string, want task.Status, timeout time.Duration) *task.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *task.Task
	for time.Now().Before(deadline) {
		got, err := m.store.Get(context.Background(), id)
		if err == nil {
			last = got
			if got.Status == want {
				return got
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for status %s; last=%+v", want, last)
	return nil
}

func TestBundleAggregatesIntoOneZip(t *testing.T) {
	tempDir := t.TempDir()
	ts := newBundleTestServer(t)
	ts.files["a.txt"] = []byte("alpha")
	ts.files["b.txt"] = []byte("bravo")
	ts.files["c.txt"] = []byte("charlie")

	m := newBundleTestManager(t, tempDir)
	zipID := "test-pack"

	first := registerBundleMember(t, m, tempDir, ts.URL, "a.txt", zipID, "pack", "pack/a.txt", false)
	if first.Task.Status != task.StatusCollecting {
		t.Fatalf("expected collecting after first member, got %s", first.Task.Status)
	}
	if first.Task.Bundle.FilesTotal != 1 || first.Task.Bundle.KnownBytes != 5 || first.Task.TotalBytes != 5 {
		t.Fatalf("unexpected first summary: %+v", first.Task.Bundle)
	}

	second := registerBundleMember(t, m, tempDir, ts.URL, "b.txt", zipID, "pack", "pack/b.txt", false)
	if second.Task.ID != first.Task.ID {
		t.Fatalf("expected same bundle task, got %s and %s", first.Task.ID, second.Task.ID)
	}
	if second.Task.Bundle.FilesTotal != 2 || second.Task.Bundle.KnownBytes != 10 {
		t.Fatalf("unexpected second summary: %+v", second.Task.Bundle)
	}

	third := registerBundleMember(t, m, tempDir, ts.URL, "c.txt", zipID, "pack", "pack/c.txt", true)
	if !third.Task.Bundle.Sealed {
		t.Fatalf("expected batch sealed")
	}

	completed := waitBundleStatus(t, m, first.Task.ID, task.StatusCompleted, 30*time.Second)
	if completed.Bundle.FilesDone != 3 {
		t.Fatalf("expected 3 done members, got %+v", completed.Bundle)
	}

	all, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected exactly one task, got %d", len(all))
	}

	zipPath := filepath.Join(tempDir, "pack.zip")
	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatalf("zip not created: %v", err)
	}
	if completed.TotalBytes != info.Size() || completed.Downloaded != info.Size() {
		t.Fatalf("expected completed size %d, got total=%d downloaded=%d", info.Size(), completed.TotalBytes, completed.Downloaded)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer func() { _ = zr.Close() }()
	if len(zr.File) != 3 {
		t.Fatalf("expected 3 zip entries, got %d", len(zr.File))
	}
	wantNames := []string{"pack/a.txt", "pack/b.txt", "pack/c.txt"}
	wantBodies := []string{"alpha", "bravo", "charlie"}
	for i, f := range zr.File {
		if f.Name != wantNames[i] {
			t.Fatalf("entry %d: expected %s, got %s", i, wantNames[i], f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read entry %s: %v", f.Name, err)
		}
		if string(body) != wantBodies[i] {
			t.Fatalf("entry %s: expected %q, got %q", f.Name, wantBodies[i], body)
		}
	}
}

func TestBundleRegistrationValidation(t *testing.T) {
	tempDir := t.TempDir()
	ts := newBundleTestServer(t)
	ts.files["a.txt"] = []byte("alpha")
	ts.files["b.txt"] = []byte("bravo")

	m := newBundleTestManager(t, tempDir)
	zipID := "validation-pack"
	reg := registerBundleMember(t, m, tempDir, ts.URL, "a.txt", zipID, "pack", "pack/a.txt", false)

	// 同一 URL + 同一路径是幂等重试，不产生第二个成员。
	dup := registerBundleMember(t, m, tempDir, ts.URL, "a.txt", zipID, "pack", "pack/a.txt", false)
	if !dup.Duplicate || dup.Task.Bundle.FilesTotal != 1 {
		t.Fatalf("expected idempotent duplicate, got duplicate=%v files=%d", dup.Duplicate, dup.Task.Bundle.FilesTotal)
	}

	// 同路径不同 URL 是冲突。
	_, err := m.RegisterBundleMember(context.Background(), BundleRegisterRequest{
		ZipID: zipID, ZipName: "pack", Directory: tempDir,
		Member: BundleMemberRequest{URL: ts.URL + "/b.txt", Filename: "b.txt", EntryPath: "pack/a.txt"},
	})
	var coded interface{ HTTPStatus() int }
	if !errors.As(err, &coded) || coded.HTTPStatus() != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %v", err)
	}

	// 同一个 zipId 换名字也是冲突。
	_, err = m.RegisterBundleMember(context.Background(), BundleRegisterRequest{
		ZipID: zipID, ZipName: "other", Directory: tempDir,
		Member: BundleMemberRequest{URL: ts.URL + "/b.txt", Filename: "b.txt", EntryPath: "pack/b.txt"},
	})
	if !errors.As(err, &coded) || coded.HTTPStatus() != http.StatusConflict {
		t.Fatalf("expected name mismatch conflict, got %v", err)
	}

	// 路径穿越与 Windows 保留名必须在登记时拒绝。
	for _, entry := range []string{"../evil.txt", "/abs.txt", "pack/CON", "pack/a b/../c"} {
		_, err := m.RegisterBundleMember(context.Background(), BundleRegisterRequest{
			ZipID: zipID, ZipName: "pack", Directory: tempDir,
			Member: BundleMemberRequest{URL: ts.URL + "/b.txt", Filename: "b.txt", EntryPath: entry},
		})
		if err == nil {
			t.Fatalf("expected path %q to be rejected", entry)
		}
	}

	// 闭合后不能再追加。
	if err := m.SealBundle(context.Background(), reg.Task.ID); err != nil {
		t.Fatalf("seal: %v", err)
	}
	_, err = m.RegisterBundleMember(context.Background(), BundleRegisterRequest{
		ZipID: zipID, ZipName: "pack", Directory: tempDir,
		Member: BundleMemberRequest{URL: ts.URL + "/b.txt", Filename: "b.txt", EntryPath: "pack/b.txt"},
	})
	if !errors.Is(err, ErrBundleSealed) {
		t.Fatalf("expected ErrBundleSealed, got %v", err)
	}
}

func TestBundleFailureAndRetry(t *testing.T) {
	tempDir := t.TempDir()
	ts := newBundleTestServer(t)
	ts.files["ok.txt"] = []byte("ok")
	ts.files["fail.txt"] = []byte("retry-me")

	m := newBundleTestManager(t, tempDir)
	zipID := "retry-pack"
	registerBundleMember(t, m, tempDir, ts.URL, "ok.txt", zipID, "retry", "retry/ok.txt", false)
	reg := registerBundleMember(t, m, tempDir, ts.URL, "fail.txt", zipID, "retry", "retry/fail.txt", false)

	ts.fail.Store(true)
	if err := m.SealBundle(context.Background(), reg.Task.ID); err != nil {
		t.Fatalf("seal: %v", err)
	}

	failed := waitBundleStatus(t, m, reg.Task.ID, task.StatusError, 30*time.Second)
	if failed.FailurePhase != task.FailurePhaseTransfer {
		t.Fatalf("expected transfer failure, got phase=%q error=%q", failed.FailurePhase, failed.ErrorMsg)
	}
	if !strings.Contains(failed.ErrorMsg, "fail.txt") {
		t.Fatalf("expected member name in error, got %q", failed.ErrorMsg)
	}

	ts.fail.Store(false)
	if err := m.Retry(context.Background(), reg.Task.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	completed := waitBundleStatus(t, m, reg.Task.ID, task.StatusCompleted, 30*time.Second)
	if completed.Bundle.FilesDone != 2 {
		t.Fatalf("expected both members done, got %+v", completed.Bundle)
	}

	zr, err := zip.OpenReader(filepath.Join(tempDir, "retry.zip"))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer func() { _ = zr.Close() }()
	if len(zr.File) != 2 {
		t.Fatalf("expected 2 zip entries, got %d", len(zr.File))
	}
}
