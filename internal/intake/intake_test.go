package intake_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sheep-get/internal/config"
	"sheep-get/internal/duplicate"
	"sheep-get/internal/engine"
	"sheep-get/internal/hls"
	"sheep-get/internal/intake"
	"sheep-get/internal/task"
)

// fakeEngine 记录 intake 交给引擎的每一次决定，并按用例配置返回。它只实现 intake.Engine：
// intake 之外的引擎能力（传输、调度）不属于这条链路。
type fakeEngine struct {
	probe      *engine.ProbeResult
	probeErr   error
	source     *hls.Source
	sourceErr  error
	duplicate  *task.Task
	occupancy  engine.Occupancy
	reserved   engine.Reserved
	created    *task.Task
	reusedTask *task.Task

	probeCalls      int
	probeURL        string
	probeHeaders    map[string]string
	resolvedURI     string
	resolvedHeaders map[string]string

	createdURL      string
	createdDir      string
	createdFilename string
	createdMaxConn  int
	createdPageURL  string
	createdCategory string
	createdProbe    *engine.ProbeResult
	createdProbeErr error
	reusedID        string
	reusedPageURL   string
	reusedCategory  string
	// duplicateVariantLookup 记录历史任务查找用的清晰度：清晰度是否参与判定，只能从这里看出来。
	duplicateVariantLookup string
	duplicateID            string
	duplicateAction        duplicate.Action
	duplicateProbe         *engine.ProbeResult
	confirmedID            string
	confirmedDir           string
	confirmedName          string
	confirmedPageURL       string
	confirmedCategory      string
	cancelled              []string
}

func (f *fakeEngine) ProbeURL(_ context.Context, urlStr string, headers map[string]string) (*engine.ProbeResult, error) {
	f.probeCalls++
	f.probeURL = urlStr
	f.probeHeaders = headers
	return f.probe, f.probeErr
}

func (f *fakeEngine) ResolveHLSVariant(_ context.Context, _, variantURI string, headers map[string]string) (*hls.Source, error) {
	f.resolvedURI = variantURI
	f.resolvedHeaders = headers
	return f.source, f.sourceErr
}

func (f *fakeEngine) FindDuplicateTaskWithVariant(_ context.Context, _, variantURI string) (*task.Task, error) {
	f.duplicateVariantLookup = variantURI
	return f.duplicate, nil
}

func (f *fakeEngine) Occupancy(_ context.Context, reserved engine.Reserved) engine.Occupancy {
	f.reserved = reserved
	occ := f.occupancy
	occ.Reserved = reserved
	return occ
}

func (f *fakeEngine) CreateTask(_ context.Context, req engine.CreateRequest) (*task.Task, error) {
	f.createdURL, f.createdDir, f.createdFilename, f.createdMaxConn = req.URL, req.Directory, req.Filename, req.MaxConn
	f.createdPageURL, f.createdCategory = req.PageURL, req.CategoryID
	f.createdProbe, f.createdProbeErr = req.Probe, req.ProbeErr
	if f.created == nil {
		f.created = &task.Task{ID: "task_new"}
	}
	return f.created, nil
}

func (f *fakeEngine) ResolveDuplicate(_ context.Context, req engine.DuplicateRequest) (*task.Task, error) {
	f.duplicateID, f.duplicateAction, f.duplicateProbe = req.TaskID, req.Action, req.Probe
	f.createdDir, f.createdFilename, f.createdMaxConn, f.createdCategory = req.Directory, req.Filename, req.MaxConn, req.CategoryID
	if f.created == nil {
		f.created = &task.Task{ID: "task_dup"}
	}
	return f.created, nil
}

func (f *fakeEngine) ConfirmPreDownload(_ context.Context, req engine.ConfirmRequest) (*task.Task, error) {
	f.confirmedID, f.confirmedDir, f.confirmedName = req.TaskID, req.Directory, req.Filename
	f.confirmedPageURL, f.confirmedCategory = req.PageURL, req.CategoryID
	if f.created == nil {
		f.created = &task.Task{ID: req.TaskID}
	}
	return f.created, nil
}

func (f *fakeEngine) ReuseExistingFile(_ context.Context, req engine.ReuseRequest) (*task.Task, error) {
	f.reusedID = req.TaskID
	f.reusedPageURL, f.reusedCategory = req.PageURL, req.CategoryID
	f.createdDir, f.createdFilename = req.Directory, req.Filename
	if f.reusedTask == nil {
		f.reusedTask = &task.Task{ID: req.TaskID, Status: task.StatusCompleted}
	}
	return f.reusedTask, nil
}

func (f *fakeEngine) CancelPreDownload(_ context.Context, taskID string) error {
	f.cancelled = append(f.cancelled, taskID)
	return nil
}

type settingsProvider struct{ settings config.Settings }

func (p settingsProvider) Get() config.Settings { return p.settings }

// newFixture 装配一条 intake 与它使用的临时目录。默认目录就是 tmp，内置分类因此落在
// tmp/Videos、tmp/Files 这类子目录下。
func newFixture(t *testing.T, policy config.DuplicateURLPolicy) (*intake.Intake, *fakeEngine, string) {
	t.Helper()
	tmpDir := t.TempDir()
	settings := config.DefaultSettings(tmpDir, filepath.Join(tmpDir, "temp"))
	settings.Download.DuplicateURLPolicy = policy
	settings.Download.PreDownload = false
	settings.Download.DefaultConnectionsPerTask = 4
	eng := &fakeEngine{}
	return intake.New(eng, settingsProvider{settings: settings}), eng, tmpDir
}

func TestBegin_NamesTargetAndResolvesCategoryDirectory(t *testing.T) {
	in, _, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)

	target := in.Begin(context.Background(), intake.Request{
		URL: "https://example.com/clip.mp4",
	}, nil)

	if target.Filename != "clip.mp4" {
		t.Errorf("filename = %q, want clip.mp4", target.Filename)
	}
	if want := filepath.Join(tmpDir, "Videos"); target.Directory != want {
		t.Errorf("directory = %q, want %q", target.Directory, want)
	}
	if target.CategoryID != "builtin-video" {
		t.Errorf("category = %q, want builtin-video", target.CategoryID)
	}
	if target.CategoryDirectory != filepath.Join(tmpDir, "Videos") {
		t.Errorf("category directory = %q, want the resolved category folder", target.CategoryDirectory)
	}
	if target.MaxConn != 4 {
		t.Errorf("max conn = %d, want the configured default", target.MaxConn)
	}
}

// 清单的成品是 MP4：登记时就要定下名字与后缀，否则探测前的数秒里分类会按 .m3u8 落到别处。
func TestBegin_NamesPlaylistOutputAsVideo(t *testing.T) {
	in, _, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)

	target := in.Begin(context.Background(), intake.Request{URL: "https://example.com/hls/stream.m3u8"}, nil)

	if target.Filename != "stream.mp4" {
		t.Errorf("filename = %q, want stream.mp4", target.Filename)
	}
	if target.CategoryID != "builtin-video" {
		t.Errorf("category = %q, want builtin-video", target.CategoryID)
	}
	if want := filepath.Join(tmpDir, "Videos"); target.Directory != want {
		t.Errorf("directory = %q, want %q", target.Directory, want)
	}
}

func TestBegin_KeepsUserDirectoryAndStillReportsCategory(t *testing.T) {
	in, _, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	manualDir := filepath.Join(tmpDir, "manual")

	target := in.Begin(context.Background(), intake.Request{
		URL:       "https://example.com/clip.mp4",
		Directory: manualDir,
	}, nil)

	if target.Directory != manualDir {
		t.Errorf("directory = %q, want the user's folder", target.Directory)
	}
	if target.CategoryID != "builtin-video" {
		t.Errorf("category = %q, want the matched category even with a manual folder", target.CategoryID)
	}
}

// GitHub 资产链接的路径里只有一个 GUID，真实文件名与后缀只在响应头里。探测补齐真名后，
// 命中分类与保存目录必须跟着重算，否则 .msi 会落到「文件」分类。
func TestApplyProbe_AdoptsRealNameAndRefreshesDestination(t *testing.T) {
	in, _, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	target := in.Begin(context.Background(), intake.Request{
		URL: "https://example.com/github-production-release-asset/12e1d8af-2024",
	}, nil)
	if target.CategoryID != "builtin-file" {
		t.Fatalf("没有后缀的猜测名应当先落到文件分类，实际 %q", target.CategoryID)
	}

	probe := &engine.ProbeResult{
		URL:        target.URL,
		Filename:   "PowerShell-7.6.6-win-x64.msi",
		TotalBytes: 2048,
	}
	in.ApplyProbe(context.Background(), &target, probe, nil, nil)

	if target.Filename != "PowerShell-7.6.6-win-x64.msi" {
		t.Errorf("filename = %q, want the probed name", target.Filename)
	}
	if target.CategoryID != "builtin-software" {
		t.Errorf("category = %q, want builtin-software", target.CategoryID)
	}
	if want := filepath.Join(tmpDir, "Software"); target.Directory != want {
		t.Errorf("directory = %q, want %q", target.Directory, want)
	}
	if target.TotalBytes != 2048 {
		t.Errorf("total bytes = %d, want 2048", target.TotalBytes)
	}
}

// 用户自己填的名字不被探测结果替换：用户的选择优先于服务器的建议。
func TestApplyProbe_KeepsUserNamedFile(t *testing.T) {
	in, _, _ := newFixture(t, config.DuplicatePolicyPrompt)
	target := in.Begin(context.Background(), intake.Request{
		URL:      "https://example.com/download",
		Filename: "mine.bin",
	}, nil)

	probe := &engine.ProbeResult{URL: target.URL, Filename: "server.bin", TotalBytes: 10}
	in.ApplyProbe(context.Background(), &target, probe, nil, nil)

	if target.Filename != "mine.bin" {
		t.Errorf("filename = %q, want the user's name", target.Filename)
	}
}

// 同一条清单的不同清晰度是不同的下载目标：探测已经带着选定清晰度时，重复查找必须按它判定。
func TestApplyProbe_FindsHistoryBySelectedVariant(t *testing.T) {
	in, eng, _ := newFixture(t, config.DuplicatePolicyPrompt)
	masterURL := "https://example.com/master.m3u8"
	history := &task.Task{
		ID: "task_720", URL: masterURL, Status: task.StatusCompleted,
		Media: &hls.Source{Variant: hls.Variant{URI: "https://example.com/720.m3u8", Height: 720}},
	}
	eng.duplicate = history

	target := in.Begin(context.Background(), intake.Request{
		URL:        masterURL,
		VariantURI: "https://example.com/720.m3u8",
	}, nil)
	probe := &engine.ProbeResult{
		URL: masterURL, TotalBytes: -1,
		HLS: &engine.HLSProbe{
			IsHLS:       true,
			PlaylistURL: masterURL,
			Variants:    []hls.Variant{{URI: "https://example.com/720.m3u8"}, {URI: "https://example.com/1080.m3u8"}},
			Media:       &hls.Source{Variant: hls.Variant{URI: "https://example.com/720.m3u8", Height: 720}},
		},
	}
	in.ApplyProbe(context.Background(), &target, probe, nil, nil)

	if target.DuplicateTask == nil || target.DuplicateTask.ID != "task_720" {
		t.Fatalf("duplicate = %+v, want the 720p history", target.DuplicateTask)
	}
	if eng.duplicateVariantLookup != "https://example.com/720.m3u8" {
		t.Errorf("history lookup variant = %q, want the selected variant", eng.duplicateVariantLookup)
	}
	if target.DuplicateDecision.Case != duplicate.CaseDestinationOccupied && target.DuplicateDecision.Case != duplicate.CaseHistoryUnfinished && target.DuplicateDecision.Case != duplicate.CaseHistoryFileAbsent {
		t.Fatalf("unexpected case %q with history present", target.DuplicateDecision.Case)
	}
}

// 扩展在交接前已经选好清晰度：登记时的历史任务查找就要按它判定，不能只看 URL——
// 那会把另一个清晰度的历史任务当成同一次下载，用户就再也拿不到这一版。
func TestBegin_FindsHistoryByRequestVariant(t *testing.T) {
	in, eng, _ := newFixture(t, config.DuplicatePolicyPrompt)
	const variantURI = "https://example.com/1080.m3u8"

	in.Begin(context.Background(), intake.Request{
		URL:        "https://example.com/master.m3u8",
		VariantURI: variantURI,
	}, nil)

	if eng.duplicateVariantLookup != variantURI {
		t.Errorf("history lookup variant = %q, want %q", eng.duplicateVariantLookup, variantURI)
	}
}

// 直下载入口没有窗口可问：同上局面按「目标位置已有成品」给出安全默认动作，绝不静默覆盖。
func TestBegin_DirectModeDefaultsCompletedHistoryToCopy(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	targetURL := "https://example.com/file.zip"
	archives := filepath.Join(tmpDir, "Archives")
	if err := os.MkdirAll(archives, 0o755); err != nil {
		t.Fatalf("failed to create category folder: %v", err)
	}
	history := &task.Task{
		ID: "task_done", URL: targetURL, Filename: "file.zip",
		Directory: archives, Status: task.StatusCompleted,
	}
	eng.duplicate = history
	if err := os.WriteFile(filepath.Join(archives, "file.zip"), []byte("done"), 0o644); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	target := in.Begin(context.Background(), intake.Request{
		URL: targetURL, Mode: intake.ModeDirect,
	}, nil)

	if target.DuplicateDecision.Case != duplicate.CaseDestinationOccupied {
		t.Fatalf("case = %q, want destination occupied", target.DuplicateDecision.Case)
	}
	if target.DuplicateDecision.Default != duplicate.ActionCopy {
		t.Errorf("default = %q, want copy (someone has to be asked, and nobody is here)", target.DuplicateDecision.Default)
	}
	if target.Filename != "file (1).zip" {
		t.Errorf("filename = %q, want the numbered copy that will actually be written", target.Filename)
	}
	if target.FileConflict {
		t.Error("the copy name is free, so the target must not report a conflict")
	}
}

// 没有历史任务、目标位置又有同名成品：直下载入口取第一个可用的副本名，而不是覆盖别人的文件。
func TestBegin_DirectModeAvoidsOverwritingUnrelatedFile(t *testing.T) {
	in, _, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	videos := filepath.Join(tmpDir, "Videos")
	if err := os.MkdirAll(videos, 0o755); err != nil {
		t.Fatalf("failed to create category folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(videos, "clip.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	target := in.Begin(context.Background(), intake.Request{
		URL: "https://example.com/clip.mp4", Mode: intake.ModeDirect,
	}, nil)

	if target.Filename != "clip (1).mp4" {
		t.Errorf("filename = %q, want the first free numbered copy", target.Filename)
	}
	if target.FileConflict {
		t.Error("the copy name is free, so the target must not report a conflict")
	}
}

// 文件信息窗口有窗口可问：同样的局面必须留给用户决定，不能被入口替用户选。
func TestBegin_WindowModeLeavesOccupiedDestinationToUser(t *testing.T) {
	in, _, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	videos := filepath.Join(tmpDir, "Videos")
	if err := os.MkdirAll(videos, 0o755); err != nil {
		t.Fatalf("failed to create category folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(videos, "clip.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	target := in.Begin(context.Background(), intake.Request{
		URL: "https://example.com/clip.mp4", Mode: intake.ModeWindow,
	}, nil)

	if !target.FileConflict {
		t.Error("expected the conflict to be reported to the window")
	}
	if target.Filename != "clip.mp4" {
		t.Errorf("filename = %q, want the name the user asked for until they choose otherwise", target.Filename)
	}
}

func TestProbe_ResolvesPreselectedVariant(t *testing.T) {
	in, eng, _ := newFixture(t, config.DuplicatePolicyPrompt)
	masterURL := "https://example.com/master.m3u8"
	source := &hls.Source{
		PlaylistURL: masterURL,
		Variant:     hls.Variant{URI: "https://example.com/1080.m3u8", Height: 1080},
		TotalBytes:  80,
	}
	eng.probe = &engine.ProbeResult{
		URL: masterURL, Filename: "master.mp4", TotalBytes: -1,
		HLS: &engine.HLSProbe{
			IsHLS:       true,
			PlaylistURL: masterURL,
			Variants:    []hls.Variant{{URI: "https://example.com/720.m3u8"}, {URI: "https://example.com/1080.m3u8"}},
		},
	}
	eng.source = source

	target := in.Begin(context.Background(), intake.Request{
		URL: masterURL, VariantURI: "https://example.com/1080.m3u8",
	}, nil)
	probe, err := in.Probe(context.Background(), target)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	in.ApplyProbe(context.Background(), &target, probe, nil, nil)

	if eng.resolvedURI != "https://example.com/1080.m3u8" {
		t.Errorf("resolved %q, want the preselected variant", eng.resolvedURI)
	}
	if target.QualityLabel != "1080P" {
		t.Errorf("quality label = %q, want 1080P", target.QualityLabel)
	}
	if target.Filename != "master_1080p.mp4" {
		t.Errorf("filename = %q, want the variant tag on the output name", target.Filename)
	}
	if target.TotalBytes != 80 {
		t.Errorf("total bytes = %d, want the selected source's size", target.TotalBytes)
	}
}

// 直下载入口在扩展没有带清晰度时取清单里的第一版：没有窗口可问，只能照清单顺序落地。
func TestProbe_DirectModePicksFirstVariant(t *testing.T) {
	in, eng, _ := newFixture(t, config.DuplicatePolicyPrompt)
	masterURL := "https://example.com/master.m3u8"
	eng.probe = &engine.ProbeResult{
		URL: masterURL, Filename: "master.mp4", TotalBytes: -1,
		HLS: &engine.HLSProbe{
			IsHLS:       true,
			PlaylistURL: masterURL,
			Variants:    []hls.Variant{{URI: "https://example.com/720.m3u8"}, {URI: "https://example.com/1080.m3u8"}},
		},
	}
	eng.source = &hls.Source{PlaylistURL: masterURL, Variant: hls.Variant{URI: "https://example.com/720.m3u8", Height: 720}}

	target := in.Begin(context.Background(), intake.Request{URL: masterURL, Mode: intake.ModeDirect}, nil)
	probe, err := in.Probe(context.Background(), target)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	if eng.resolvedURI != "https://example.com/720.m3u8" {
		t.Errorf("resolved %q, want the first listed variant", eng.resolvedURI)
	}
	if probe.HLS.Media == nil || probe.HLS.Media.Variant.Height != 720 {
		t.Fatalf("probe media = %+v, want the resolved 720p source", probe.HLS.Media)
	}
}

func TestSelectVariant_RejectsNonPlaylist(t *testing.T) {
	in, eng, _ := newFixture(t, config.DuplicatePolicyPrompt)
	eng.probe = &engine.ProbeResult{URL: "https://example.com/file.mp4", Filename: "file.mp4", TotalBytes: 10}

	target := in.Begin(context.Background(), intake.Request{URL: "https://example.com/file.mp4"}, nil)
	_, err := in.SelectVariant(context.Background(), target, "https://example.com/file.mp4", "https://example.com/x.m3u8")
	if !errors.Is(err, intake.ErrNotPlaylist) {
		t.Fatalf("err = %v, want ErrNotPlaylist", err)
	}
}

// 手输链接在登记时没有探测结果：选定清晰度时补一次探测，结果随 Selection 交回并沿用。
func TestSelectVariant_ProbesManualURL(t *testing.T) {
	in, eng, _ := newFixture(t, config.DuplicatePolicyPrompt)
	manualURL := "https://example.com/master.m3u8"
	eng.probe = &engine.ProbeResult{
		URL: manualURL, Filename: "master.mp4", TotalBytes: -1,
		HLS: &engine.HLSProbe{
			IsHLS:       true,
			PlaylistURL: manualURL,
			Variants:    []hls.Variant{{URI: "https://example.com/720.m3u8"}},
		},
	}
	eng.source = &hls.Source{PlaylistURL: manualURL, Variant: hls.Variant{URI: "https://example.com/720.m3u8", Height: 720}}

	target := in.Begin(context.Background(), intake.Request{}, nil)
	selection, err := in.SelectVariant(context.Background(), target, manualURL, "https://example.com/720.m3u8")
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if selection.Probe == nil || selection.Probe.HLS == nil {
		t.Fatalf("selection probe = %+v, want the probe taken while selecting", selection.Probe)
	}

	ready := in.ApplyVariant(context.Background(), &target, selection, nil)
	if !ready {
		t.Fatal("a selected variant should allow a pre-download")
	}
	if target.QualityLabel != "720P" {
		t.Errorf("quality label = %q, want 720P", target.QualityLabel)
	}
}

func TestSubmit_WindowModeRequiresActionForUnresolvedDuplicate(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	targetURL := "https://example.com/occupied.bin"
	eng.duplicate = &task.Task{
		ID: "task_done", URL: targetURL, Filename: "occupied.bin",
		Directory: tmpDir, Status: task.StatusCompleted,
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "occupied.bin"), []byte("done"), 0o644); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	target := in.Begin(context.Background(), intake.Request{URL: targetURL, Directory: tmpDir}, nil)
	result, err := in.Submit(context.Background(), target, intake.Choice{
		URL: targetURL, Filename: "occupied.bin", Directory: tmpDir, MaxConn: 4,
	})
	if !errors.Is(err, intake.ErrChoiceRequired) {
		t.Fatalf("err = %v, want ErrChoiceRequired", err)
	}
	if result.Decision.Case != duplicate.CaseDestinationOccupied {
		t.Fatalf("decision = %+v, want the occupied case so the window can offer options", result.Decision)
	}
	if len(result.Decision.Options) != 3 {
		t.Fatalf("options = %v, want all three actions", result.Decision.Options)
	}
	if eng.duplicateAction != "" {
		t.Fatalf("engine was asked to act (%q) without a choice", eng.duplicateAction)
	}
}

// 直下载入口没有窗口：同样的局面按安全答案执行，且真的落到那条策略上。
func TestSubmit_DirectModeContinuesUnfinishedHistory(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	targetURL := "https://example.com/big.iso"
	eng.duplicate = &task.Task{
		ID: "task_partial", URL: targetURL, Filename: "big.iso",
		Directory: tmpDir, Status: task.StatusPaused,
	}

	target := in.Begin(context.Background(), intake.Request{URL: targetURL, Mode: intake.ModeDirect}, nil)
	result, err := in.Submit(context.Background(), target, intake.Choice{})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	if eng.duplicateAction != duplicate.ActionContinue || eng.duplicateID != "task_partial" {
		t.Errorf("resolved duplicate as (%q, %q), want continue on task_partial", eng.duplicateID, eng.duplicateAction)
	}
	if result.Task == nil || result.Task.ID != eng.created.ID {
		t.Errorf("result task = %+v, want the task the engine returned", result.Task)
	}
}

func TestSubmit_UsesRegistrationProbeWithoutProbingAgain(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	targetURL := "https://example.com/video.mp4"
	probe := &engine.ProbeResult{URL: targetURL, Filename: "video.mp4", TotalBytes: 1024}

	target := in.Begin(context.Background(), intake.Request{URL: targetURL, Directory: tmpDir}, nil)
	in.ApplyProbe(context.Background(), &target, probe, nil, nil)
	if _, err := in.Submit(context.Background(), target, intake.Choice{
		URL: targetURL, Filename: "video.mp4", Directory: tmpDir, MaxConn: 4,
	}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	if eng.probeCalls != 0 {
		t.Errorf("probe calls = %d, want 0: the registration probe must be reused", eng.probeCalls)
	}
	if eng.createdProbe != probe {
		t.Fatalf("created probe = %p, want the registration probe %p", eng.createdProbe, probe)
	}
	if eng.createdFilename != "video.mp4" || eng.createdDir != tmpDir {
		t.Errorf("created at %q/%q, want %q/%q", eng.createdDir, eng.createdFilename, tmpDir, "video.mp4")
	}
}

// 用户改过链接：手上那份元数据已经不属于新链接，提交必须让引擎自己探一次。
func TestSubmit_InvalidatesProbeWhenURLChanged(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	oldURL := "https://example.com/old.mp4"
	newURL := "https://example.com/new.mp4"

	target := in.Begin(context.Background(), intake.Request{URL: oldURL, Directory: tmpDir}, nil)
	in.ApplyProbe(context.Background(), &target, &engine.ProbeResult{URL: oldURL, Filename: "old.mp4"}, nil, nil)
	if eng.createdProbe != nil {
		t.Fatalf("created probe = %+v before submit", eng.createdProbe)
	}

	if _, err := in.Submit(context.Background(), target, intake.Choice{
		URL: newURL, Filename: "new.mp4", Directory: tmpDir, MaxConn: 4,
	}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	if eng.createdProbe != nil {
		t.Fatalf("created probe = %+v, want nil: metadata from another link must not be reused", eng.createdProbe)
	}
	if eng.createdURL != newURL {
		t.Errorf("created URL = %q, want %q", eng.createdURL, newURL)
	}
}

// 提交前用户改了目录与文件名：裁决按最终落点重算，而不是沿用登记时那份。
func TestSubmit_RecomputesDecisionForFinalDestination(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	targetURL := "https://example.com/item.zip"
	eng.duplicate = &task.Task{
		ID: "task_done", URL: targetURL, Filename: "other.zip",
		Directory: tmpDir, Status: task.StatusCompleted,
	}
	finalDir := filepath.Join(tmpDir, "elsewhere")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(finalDir, "final.zip"), []byte("done"), 0o644); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	target := in.Begin(context.Background(), intake.Request{URL: targetURL}, nil)
	result, err := in.Submit(context.Background(), target, intake.Choice{
		Filename: "final.zip", Directory: finalDir, MaxConn: 2,
		Action: duplicate.ActionRedownload,
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	if result.Decision.Case != duplicate.CaseDestinationOccupied {
		t.Fatalf("decision = %+v, want the occupied case for the final destination", result.Decision)
	}
	if eng.createdDir != finalDir || eng.createdFilename != "final.zip" {
		t.Errorf("created at %q/%q, want the final destination", eng.createdDir, eng.createdFilename)
	}
}

func TestSubmit_ConfirmsPreDownloadWithMetadata(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	targetURL := "https://example.com/pre.bin"

	target := in.Begin(context.Background(), intake.Request{
		URL: targetURL, Directory: tmpDir, PageURL: "https://page.example/",
	}, nil)
	in.ApplyProbe(context.Background(), &target, &engine.ProbeResult{URL: targetURL, Filename: "pre.bin"}, nil, nil)

	if _, err := in.Submit(context.Background(), target, intake.Choice{PreTaskID: "task_pre"}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	if eng.confirmedID != "task_pre" {
		t.Errorf("confirmed %q, want task_pre", eng.confirmedID)
	}
	if eng.confirmedPageURL != "https://page.example/" {
		t.Errorf("confirmed page URL = %q, want the source page", eng.confirmedPageURL)
	}
	if eng.confirmedCategory != "builtin-file" {
		t.Errorf("confirmed category = %q, want builtin-file", eng.confirmedCategory)
	}
}

// 复用别处成品是提交的第四条出口：落点是这次选的，来源页面与分类同样跟着这次选择走。
func TestSubmit_ReusesExistingFileWithMetadata(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	targetURL := "https://example.com/reuse.bin"

	target := in.Begin(context.Background(), intake.Request{
		URL: targetURL, Directory: tmpDir, PageURL: "https://page.example/",
	}, nil)
	in.ApplyProbe(context.Background(), &target, &engine.ProbeResult{URL: targetURL, Filename: "reuse.bin"}, nil, nil)

	if _, err := in.Submit(context.Background(), target, intake.Choice{
		Action: duplicate.ActionReuse, ReuseTaskID: "task_done", MaxConn: 3,
	}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	if eng.reusedID != "task_done" {
		t.Errorf("reused task = %q, want task_done", eng.reusedID)
	}
	if eng.reusedPageURL != "https://page.example/" {
		t.Errorf("reused page URL = %q, want the source page", eng.reusedPageURL)
	}
	if eng.reusedCategory != "builtin-file" {
		t.Errorf("reused category = %q, want builtin-file", eng.reusedCategory)
	}
	if eng.createdDir != tmpDir || eng.createdFilename != "reuse.bin" {
		t.Errorf("reused destination = %q/%q, want the target's", eng.createdDir, eng.createdFilename)
	}
}

func TestStartPreDownload_ReusesRegistrationProbe(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	targetURL := "https://example.com/pre.bin"
	probe := &engine.ProbeResult{URL: targetURL, Filename: "pre.bin", TotalBytes: 99}

	target := in.Begin(context.Background(), intake.Request{URL: targetURL, Directory: tmpDir}, nil)
	in.ApplyProbe(context.Background(), &target, probe, nil, nil)
	if _, err := in.StartPreDownload(context.Background(), target); err != nil {
		t.Fatalf("start pre-download failed: %v", err)
	}

	if eng.createdProbe != probe {
		t.Error("pre-download re-probed instead of reusing the registration probe")
	}
	if eng.createdDir != tmpDir || eng.createdFilename != "pre.bin" {
		t.Errorf("pre-download destination = %q/%q, want the target's", eng.createdDir, eng.createdFilename)
	}
}

// 探测失败时不把失败的探测结果当成成功事实递下去：引擎要自己再探一次，按失败原因留下
// 一条看得见的失败任务（A03）。
func TestStartPreDownload_OmitsFailedProbe(t *testing.T) {
	in, eng, tmpDir := newFixture(t, config.DuplicatePolicyPrompt)
	targetURL := "https://example.com/blocked.bin"

	target := in.Begin(context.Background(), intake.Request{URL: targetURL, Directory: tmpDir}, nil)
	in.ApplyProbe(context.Background(), &target, &engine.ProbeResult{URL: targetURL, Filename: "blocked.bin"}, errors.New("403"), nil)
	if _, err := in.StartPreDownload(context.Background(), target); err != nil {
		t.Fatalf("start pre-download failed: %v", err)
	}

	if eng.createdProbe != nil {
		t.Fatalf("pre-download probe = %+v, want none so the engine re-probes", eng.createdProbe)
	}
}
