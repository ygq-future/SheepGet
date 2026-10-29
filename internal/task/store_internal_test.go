package task

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sheep-get/internal/atomicfile"
)

// TestFileTaskStoreReadsDoNotWaitForDiskWrite 固定一条契约：落盘不占用读写方的锁。
//
// 进度落盘是高频动作（每个任务每 500ms 一次），若写盘期间 Get/List 被挡住，界面刷新与所有
// 其他任务的落盘都会排在这块磁盘后面。这里把写盘卡住，要求读写方照常返回，且新的变更在
// 写盘结束前就已在内存里可见。
func TestFileTaskStoreReadsDoNotWaitForDiskWrite(t *testing.T) {
	store, err := NewFileTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	ctx := context.Background()
	if err := store.Save(ctx, &Task{ID: "a", Status: StatusPaused}); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	writing := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	store.writeFile = func(path string, data []byte, perm os.FileMode, fsync bool) error {
		once.Do(func() { close(writing) })
		<-release
		return atomicfile.Write(path, data, perm, fsync)
	}

	saved := make(chan error, 1)
	go func() { saved <- store.Save(ctx, &Task{ID: "b", Status: StatusPaused}) }()

	select {
	case <-writing:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the store to start writing")
	}

	// 写盘正在进行：读取与内存可见性都不能等它。
	read := make(chan error, 1)
	go func() {
		if _, err := store.Get(ctx, "b"); err != nil {
			read <- err
			return
		}
		_, err := store.List(ctx)
		read <- err
	}()
	select {
	case err := <-read:
		if err != nil {
			close(release)
			<-saved
			t.Fatalf("read failed while a write was in progress: %v", err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		<-saved
		t.Fatal("Get/List blocked while a disk write was in progress")
	}

	close(release)
	if err := <-saved; err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// 落盘结果是最后的状态：两个任务都在文件里。
	reloaded, err := NewFileTaskStore(store.filePath)
	if err != nil {
		t.Fatalf("failed to reload store: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := reloaded.Get(ctx, id); err != nil {
			t.Fatalf("expected %s to be persisted, got %v", id, err)
		}
	}
}

// TestFileTaskStoreStaleSnapshotDoesNotOverwriteNewer 固定版本号守卫：先取的快照晚于更新的
// 快照到达写盘时，磁盘不能被旧内容覆盖。
func TestFileTaskStoreStaleSnapshotDoesNotOverwriteNewer(t *testing.T) {
	store, err := NewFileTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	ctx := context.Background()
	if err := store.Save(ctx, &Task{ID: "t", Status: StatusPaused, Downloaded: 1}); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	// 一份还没落盘的旧快照，随后内存又前进了一次。
	store.mu.Lock()
	staleRevision, staleList := store.snapshotLocked()
	store.mu.Unlock()

	if err := store.Save(ctx, &Task{ID: "t", Status: StatusPaused, Downloaded: 2}); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	// 旧快照迟到：它不包含第二次变更，不能把磁盘写回旧内容。
	if err := store.persist(staleRevision, staleList); err != nil {
		t.Fatalf("stale persist failed: %v", err)
	}

	reloaded, err := NewFileTaskStore(store.filePath)
	if err != nil {
		t.Fatalf("failed to reload store: %v", err)
	}
	got, err := reloaded.Get(ctx, "t")
	if err != nil {
		t.Fatalf("failed to read task: %v", err)
	}
	if got.Downloaded != 2 {
		t.Fatalf("stale snapshot overwrote the newer state: Downloaded = %d, want 2", got.Downloaded)
	}
}
