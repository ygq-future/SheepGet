package window

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sheep-get/internal/config"
	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

// 用例复现的是真实下载链接的形态：GitHub Release 资产的 URL 路径里没有文件名后缀，
// 真实文件名（PowerShell-7.6.6-win-x64.msi）藏在 Content-Disposition / URL 参数里。
// 队列在登记时只能拿 URL 猜出一个没有后缀的名字，探测补上真名之后，
// 命中分类与保存目录必须跟着这个真名重算，否则 .msi 会落到「文件」分类。
const githubAssetPath = "/github-production-release-asset/49609581/12e1d8af-2024-4832-b73c-e67dc45d6a11"

func TestQueueProbeRefreshesDestinationAfterFilenameResolved(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	// 用不注入系统代理的客户端，保证探测确实打到本地测试服务器上。
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(&http.Client{}), engine.Config{MaxActiveTasks: 3})
	t.Cleanup(mgr.Close)

	settings := config.DefaultSettings(tmpDir, filepath.Join(tmpDir, "temp"))
	settings.Download.DuplicateURLPolicy = config.DuplicatePolicyPrompt
	provider := &testSettingsProvider{settings: settings}
	qc := NewQueueController(mgr, provider, &mockWindowView{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="PowerShell-7.6.6-win-x64.msi"`)
		_, _ = w.Write([]byte("msi payload"))
	}))
	defer srv.Close()

	ctx := context.Background()
	assetURL := srv.URL + githubAssetPath + "?response-content-disposition=attachment%3B+filename%3DPowerShell-7.6.6-win-x64.msi"
	if _, err := qc.Enqueue(ctx, DownloadRequest{URL: assetURL}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// 探测是异步的：等队列项拿到服务器给出的真实文件名后再断言落点。
	var item *FileInfoItem
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := qc.GetActive()
		if err != nil {
			t.Fatalf("GetActive failed: %v", err)
		}
		if got != nil && strings.Contains(got.Filename, ".msi") {
			item = got
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if item == nil {
		t.Fatal("探测结束后队列项仍没有拿到带后缀的真实文件名")
	}

	if want := filepath.Join(tmpDir, "Software"); item.Directory != want {
		t.Errorf("文件名确定为 %q 后应落到软件分类目录 %q，实际 %q", item.Filename, want, item.Directory)
	}
	if item.CategoryID != "builtin-software" {
		t.Errorf("文件名确定为 %q 后应命中 builtin-software，实际命中 %q", item.Filename, item.CategoryID)
	}
}
