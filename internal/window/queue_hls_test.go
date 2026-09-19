package window

import (
	"context"
	"errors"
	"testing"

	"sheep-get/internal/config"
	"sheep-get/internal/engine"
	"sheep-get/internal/hls"
	"sheep-get/internal/task"
)

// hlsProbeEngine 是一个只为 HLS 清晰度选择服务的轻量引擎：其余方法要么返回默认值，要么
// 直接报错，只有 ProbeURL / ResolveHLSVariant / AddTaskFromProbe 三个是真正被选清晰度
// 这条链路用到的。
type hlsProbeEngine struct {
	// probe 是 ProbeURL 要返回的结果；交给测试逐个设置。
	probe *engine.ProbeResult
	// resolve 是 ResolveHLSVariant 要返回的来源；交给测试逐个设置。
	resolve *hls.Source
	// resolveErr 让测试模拟「选中的版本解析失败」。
	resolveErr error
	// resolvedURI 记录最后一次被要求解析的清晰度地址，供断言选的是哪一个。
	resolvedURI string
	// createdProbe 记录 AddTaskFromProbe 收到的那份探测，供断言选定清晰度有没有落到任务上。
	createdProbe *engine.ProbeResult
}

func (e *hlsProbeEngine) ProbeURL(context.Context, string, map[string]string) (*engine.ProbeResult, error) {
	return e.probe, nil
}

func (e *hlsProbeEngine) ResolveHLSVariant(_ context.Context, _, variantURI string, _ map[string]string) (*hls.Source, error) {
	e.resolvedURI = variantURI
	return e.resolve, e.resolveErr
}

func (e *hlsProbeEngine) FindDuplicateTask(context.Context, string) (*task.Task, error) {
	return nil, nil
}

func (e *hlsProbeEngine) AddTask(context.Context, string, string, string, int) (*task.Task, error) {
	return nil, errors.New("AddTask should not be called in HLS selection tests")
}

func (e *hlsProbeEngine) AddTaskWithHeaders(context.Context, string, string, string, int, map[string]string) (*task.Task, error) {
	return nil, errors.New("AddTaskWithHeaders should not be called in HLS selection tests")
}

func (e *hlsProbeEngine) AddTaskFromProbe(_ context.Context, urlStr, _, _ string, _ int, _ map[string]string, probe *engine.ProbeResult, _ error) (*task.Task, error) {
	e.createdProbe = probe
	return &task.Task{ID: "task_created", URL: urlStr}, nil
}

func (e *hlsProbeEngine) StartPreDownload(context.Context, string, string, string, int) (*task.Task, error) {
	return nil, nil
}

func (e *hlsProbeEngine) StartPreDownloadWithHeaders(context.Context, string, string, string, int, map[string]string) (*task.Task, error) {
	return nil, nil
}

func (e *hlsProbeEngine) ConfirmPreDownload(context.Context, string, string, string, int) (*task.Task, error) {
	return nil, nil
}

func (e *hlsProbeEngine) CancelPreDownload(context.Context, string) error {
	return nil
}

func (e *hlsProbeEngine) ResolveDuplicate(context.Context, string, string, string, string, int) (*task.Task, error) {
	return nil, nil
}

func (e *hlsProbeEngine) NumberedCopyName(context.Context, string, string) (string, error) {
	return "", nil
}

func (e *hlsProbeEngine) ReuseExistingFile(context.Context, string, string, string) (*task.Task, error) {
	return nil, nil
}

// twoVariantProbe 构造一份有两个清晰度的 HLS 探测结果，供选择类测试复用。
func twoVariantProbe() *engine.ProbeResult {
	return &engine.ProbeResult{
		URL:        "https://example.com/master.m3u8",
		Filename:   "video.m3u8",
		TotalBytes: -1,
		HLS: &engine.HLSProbe{
			IsHLS:       true,
			PlaylistURL: "https://example.com/master.m3u8",
			Variants: []hls.Variant{
				{URI: "https://example.com/720.m3u8", Height: 720, Bandwidth: 2000000},
				{URI: "https://example.com/1080.m3u8", Height: 1080, Bandwidth: 4000000},
			},
		},
	}
}

func TestQueueController_SelectHLSVariant_LandsFactsOnItem(t *testing.T) {
	eng := &hlsProbeEngine{
		probe: twoVariantProbe(),
		resolve: &hls.Source{
			PlaylistURL: "https://example.com/master.m3u8",
			Variant:     hls.Variant{URI: "https://example.com/1080.m3u8", Height: 1080, Bandwidth: 4000000},
			Duration:    125.0,
			TotalBytes:  64 * 1024 * 1024,
		},
	}
	winView := &mockWindowView{}
	qc := newQueueController(eng, &testSettingsProvider{settings: defaultSettingsForTest(t)}, winView, inlineOps)

	resp, err := qc.Enqueue(context.Background(), DownloadRequest{URL: "https://example.com/master.m3u8"})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	if err := qc.SelectHLSVariant(context.Background(), resp.RequestID, "https://example.com/master.m3u8", "https://example.com/1080.m3u8"); err != nil {
		t.Fatalf("SelectHLSVariant failed: %v", err)
	}

	item, err := qc.GetActive()
	if err != nil {
		t.Fatalf("GetActive failed: %v", err)
	}
	if item.QualityLabel == "" {
		t.Errorf("expected a quality label after selection, got empty")
	}
	if item.QualityLabel != "1080P" {
		t.Errorf("expected quality label %q, got %q", "1080P", item.QualityLabel)
	}
	if item.MediaDuration != 125.0 {
		t.Errorf("expected media duration 125, got %v", item.MediaDuration)
	}
	if item.TotalBytes != 64*1024*1024 {
		t.Errorf("expected total bytes from selected variant, got %v", item.TotalBytes)
	}
	if eng.resolvedURI != "https://example.com/1080.m3u8" {
		t.Errorf("expected resolve for 1080 variant, got %q", eng.resolvedURI)
	}
}

func TestQueueController_SelectHLSVariant_SubmitCarriesSelectedSource(t *testing.T) {
	eng := &hlsProbeEngine{
		probe: twoVariantProbe(),
		resolve: &hls.Source{
			PlaylistURL: "https://example.com/master.m3u8",
			Variant:     hls.Variant{URI: "https://example.com/720.m3u8", Height: 720, Bandwidth: 2000000},
			Duration:    125.0,
			TotalBytes:  32 * 1024 * 1024,
		},
	}
	winView := &mockWindowView{}
	qc := newQueueController(eng, &testSettingsProvider{settings: defaultSettingsForTest(t)}, winView, inlineOps)

	resp, err := qc.Enqueue(context.Background(), DownloadRequest{URL: "https://example.com/master.m3u8"})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	if err := qc.SelectHLSVariant(context.Background(), resp.RequestID, "https://example.com/master.m3u8", "https://example.com/720.m3u8"); err != nil {
		t.Fatalf("SelectHLSVariant failed: %v", err)
	}

	item, err := qc.GetActive()
	if err != nil {
		t.Fatalf("GetActive failed: %v", err)
	}
	_, err = qc.Submit(context.Background(), FileInfoSubmission{
		RequestID: resp.RequestID,
		URL:       item.URL,
		Directory: item.Directory,
		Filename:  item.Filename,
		MaxConn:   item.MaxConn,
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if eng.createdProbe == nil {
		t.Fatalf("expected submit to use AddTaskFromProbe with a probe")
	}
	if eng.createdProbe.HLS == nil || eng.createdProbe.HLS.Media == nil {
		t.Fatalf("expected the submitted probe to carry the selected HLS source")
	}
	if got := eng.createdProbe.HLS.Media.Variant.URI; got != "https://example.com/720.m3u8" {
		t.Errorf("expected selected variant %q on task, got %q", "https://example.com/720.m3u8", got)
	}
}

func TestQueueController_SelectHLSVariant_RejectsNonPlaylist(t *testing.T) {
	eng := &hlsProbeEngine{
		probe: &engine.ProbeResult{URL: "https://example.com/file.mp4", Filename: "file.mp4", TotalBytes: 10},
	}
	winView := &mockWindowView{}
	qc := newQueueController(eng, &testSettingsProvider{settings: defaultSettingsForTest(t)}, winView, inlineOps)

	resp, err := qc.Enqueue(context.Background(), DownloadRequest{URL: "https://example.com/file.mp4"})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	err = qc.SelectHLSVariant(context.Background(), resp.RequestID, "https://example.com/file.mp4", "https://example.com/x.m3u8")
	if err == nil {
		t.Fatalf("expected SelectHLSVariant to reject a non-HLS link")
	}
}

// 手动输入的 m3u8：登记时队列项没有 URL（用户是在文件信息窗口里现填的）。选择清晰度必须
// 靠界面把当前链接显式传进来，否则后端拿不到地址、也探不到这份清单。
func TestQueueController_SelectHLSVariant_ManualURLResolves(t *testing.T) {
	eng := &hlsProbeEngine{
		probe: twoVariantProbe(),
		resolve: &hls.Source{
			PlaylistURL: "https://example.com/master.m3u8",
			Variant:     hls.Variant{URI: "https://example.com/1080.m3u8", Height: 1080, Bandwidth: 4000000},
			Duration:    125.0,
			TotalBytes:  64 * 1024 * 1024,
		},
	}
	winView := &mockWindowView{}
	qc := newQueueController(eng, &testSettingsProvider{settings: defaultSettingsForTest(t)}, winView, inlineOps)

	// 手动新建：URL 为空，探测还没发生过。
	resp, err := qc.Enqueue(context.Background(), DownloadRequest{})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	if err := qc.SelectHLSVariant(context.Background(), resp.RequestID, "https://example.com/master.m3u8", "https://example.com/1080.m3u8"); err != nil {
		t.Fatalf("SelectHLSVariant with manual URL failed: %v", err)
	}

	item, err := qc.GetActive()
	if err != nil {
		t.Fatalf("GetActive failed: %v", err)
	}
	if item.QualityLabel != "1080P" {
		t.Errorf("expected quality label %q, got %q", "1080P", item.QualityLabel)
	}
	if item.MediaDuration != 125.0 {
		t.Errorf("expected media duration 125, got %v", item.MediaDuration)
	}
}

// defaultSettingsForTest 提供一个关闭预下载、固定并发默认值的配置，与 setupQueueWithView 一致。
func defaultSettingsForTest(t *testing.T) config.Settings {
	t.Helper()
	settings := config.DefaultSettings(t.TempDir(), t.TempDir())
	settings.Download.PreDownload = false
	settings.Download.DefaultConnectionsPerTask = 4
	return settings
}
