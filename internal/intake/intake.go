// Package intake 把一条链接变成一条任务：探测、命名、HLS 清晰度、目标落点、占用与重复裁决、
// 提交。文件信息窗口与直下载入口都只做请求翻译，规则只在这里实现一次。
//
// 并发边界由调用方决定，Intake 自己不持有锁：会联网的方法（Probe、SelectVariant、
// StartPreDownload、Submit）必须在调用方的锁外执行——文件信息窗口的队列锁一旦被网络调用
// 握住，浏览器交接会在 2.5 秒上限处静默失败（ADR-0005 决策 1）。其余方法只读本地任务库与
// 磁盘，可以在锁内执行。
package intake

import (
	"context"
	"errors"
	"strings"

	"sheep-get/internal/config"
	"sheep-get/internal/duplicate"
	"sheep-get/internal/engine"
	"sheep-get/internal/hls"
	"sheep-get/internal/task"
)

// ErrChoiceRequired 表示这次提交没有携带动作，而当前局面必须由用户先选定一项
// （目标位置已有成品文件，且策略没有给出默认动作）。
var ErrChoiceRequired = errors.New("duplicate action required before submitting")

// ErrNotPlaylist 表示这条链接不是 HLS 清单，没有清晰度可选。
var ErrNotPlaylist = errors.New("这个链接不是 HLS 清单，没有清晰度可选")

// ErrVariantResolution 表示选定的清晰度没能解析出可下载的来源。
var ErrVariantResolution = errors.New("所选清晰度解析失败，请重试")

// Engine 是 intake 需要的全部引擎能力：探测与清晰度解析会联网，占用与重复查询只读本地
// 任务库，其余方法把一次决定落成任务、预下载或复用。
type Engine interface {
	ProbeURL(ctx context.Context, urlStr string, headers map[string]string) (*engine.ProbeResult, error)
	ResolveHLSVariant(ctx context.Context, playlistURL, variantURI string, headers map[string]string) (*hls.Source, error)
	FindDuplicateTaskWithVariant(ctx context.Context, urlStr, variantURI string) (*task.Task, error)
	Occupancy(ctx context.Context, reserved engine.Reserved) engine.Occupancy
	AddTaskWithMetadata(ctx context.Context, urlStr, dir, filename string, maxConn int, headers map[string]string, pageURL, categoryID string) (*task.Task, error)
	AddTaskFromProbeWithMetadata(ctx context.Context, urlStr, dir, filename string, maxConn int, headers map[string]string, probe *engine.ProbeResult, probeErr error, pageURL, categoryID string) (*task.Task, error)
	ResolveDuplicateFromProbeWithMetadata(ctx context.Context, taskID, strategy, dir, filename string, maxConn int, probe *engine.ProbeResult, categoryID string) (*task.Task, error)
	ConfirmPreDownload(ctx context.Context, taskID, finalDir, finalFilename string, maxConn int) (*task.Task, error)
	ReuseExistingFile(ctx context.Context, taskID, targetDir, targetFilename string) (*task.Task, error)
	StartPreDownloadFromProbe(ctx context.Context, req engine.PreDownloadRequest) (*task.Task, error)
	CancelPreDownload(ctx context.Context, taskID string) error
	SetTaskPageURL(ctx context.Context, taskID, pageURL string) error
	SetTaskCategoryID(ctx context.Context, taskID, categoryID string) error
}

// SettingsProvider 提供当前生效的配置。落点规则、重复策略与默认并发都从这里取一次。
type SettingsProvider interface {
	Get() config.Settings
}

// Mode 说明这次请求有没有一个让用户做决定的确认窗口。
type Mode int

const (
	// ModeWindow 是文件信息窗口：需要用户决定的局面必须让用户先选，不能替他挑。
	ModeWindow Mode = iota
	// ModeDirect 是直下载入口（扩展直接建任务）：没有窗口可问，需要用户决定的局面取安全
	// 答案——历史任务还活着就接着传，已完成就另存副本，绝不静默覆盖磁盘上的成品。
	ModeDirect
)

// Request 是入口交来的一次下载请求。入口只做请求翻译：这里只有扩展/窗口已知的事实，
// 没有推断。
type Request struct {
	URL        string
	Headers    map[string]string
	Filename   string
	Directory  string
	PageURL    string
	VariantURI string
	MaxConn    int
	Mode       Mode
}

// Target 是一次下载的目标：名字、落点、分类，以及决定它们的事实。
//
// 文件信息窗口的队列项直接嵌入它，于是窗口展示的就是目标本身，不再维护第二份名字与落点；
// 直下载入口把它当中间结果。
type Target struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	// SuggestedFilename 是原名被占用时第一个可用的序号副本名；冲突提示与「使用序号」
	// 因此出自同一次占用判定。
	SuggestedFilename string `json:"suggestedFilename"`
	Directory         string `json:"directory"`
	CategoryID        string `json:"categoryId,omitempty"`
	// CategoryDirectory 是命中分类的默认保存目录（后端按当前默认目录拼好）。界面判断
	// “这条路径是否还是分类默认值”时拿它比较，不再自己拼相对子目录。
	CategoryDirectory string            `json:"categoryDirectory"`
	MaxConn           int               `json:"maxConn"`
	Headers           map[string]string `json:"headers,omitempty"`
	PageURL           string            `json:"pageUrl,omitempty"`
	TotalBytes        int64             `json:"totalBytes"`
	MimeType          string            `json:"mimeType"`
	Resumable         bool              `json:"resumable"`
	// FileConflict 报告目标位置已有同名文件（不包含这一项自己）。
	FileConflict  bool       `json:"fileConflict"`
	DuplicateTask *task.Task `json:"duplicateTask,omitempty"`
	// DuplicateDecision 是这次重复的裁决结果：界面只渲染它给出的选项与默认项。
	// 策略本身不下发给窗口，避免窗口把它当成第二份规则来源。
	DuplicateDecision duplicate.Decision `json:"duplicateDecision"`
	// Variants 多于一项时构成一次选择：界面必须先选定才能确认下载。
	Variants []hls.VariantOption `json:"variants,omitempty"`
	// QualityLabel 是已选清晰度的展示名，为空表示还没选定（或这条链接不是清单）。
	QualityLabel string `json:"qualityLabel,omitempty"`
	// MediaDuration 是清单声明的时长（秒）；0 表示这份清单没给出时长。时长与大小都只来自
	// 能证实的信息，读不到就留空由界面显示未知，不用别处的数字凑一个出来。
	MediaDuration float64 `json:"mediaDuration,omitempty"`

	mode Mode
	// variantURI 是登记时扩展已经选好的清晰度；它决定探测阶段要不要顺手解析这一版。
	variantURI string
	// namedByRequest 记录名字是不是入口给的：用户自己填的名字不被探测结果替换。
	namedByRequest bool
	// dirFromCategory 记录目录是请求给的还是按分类裁出来的：只有后者会在名字变化后重算。
	dirFromCategory bool
	// probe 是登记时那次探测的结果；probeErr 是它的失败原因。提交建任务只认这一份，
	// 探测还在路上时由调用方等它落地。
	probe    *engine.ProbeResult
	probeErr error
	// media 是已经选定的来源。它就是这次下载要处理的东西，提交时随探测结果一起建任务。
	media *hls.Source
}

// Probed 报告这次登记发起的探测是否已经成功落地。
func (t Target) Probed() bool { return t.probe != nil && t.probeErr == nil }

// ProbeSettled 报告这次探测是否已经有结论（成功或失败）。没有结论时调用方应当等它的完成信号。
func (t Target) ProbeSettled() bool { return t.probe != nil || t.probeErr != nil }

// NeedsVariantChoice 报告这条链接有多个清晰度而用户还没选定。界面据此挡住确认操作，
// 直下载入口据此按自己的方式给出答案。
func (t Target) NeedsVariantChoice() bool {
	return t.probe != nil && t.probe.HLS != nil && t.media == nil
}

// Choice 是一次提交的选择。为空的部分沿用目标里已有的决定。
type Choice struct {
	URL         string
	Filename    string
	Directory   string
	CategoryID  string
	MaxConn     int
	Action      duplicate.Action
	ReuseTaskID string
	// PreTaskID 是窗口打开期间已经启动的预下载任务；它与确认动作一起落到最终落点。
	PreTaskID string
}

// Result 是一次提交的结果：落成的任务，以及提交时重新核算过的重复裁决。
// 提交可能带着错误返回（例如需要用户先选动作），此时 Decision 仍要写回界面。
type Result struct {
	Task     *task.Task
	Decision duplicate.Decision
}

// VariantSelection 是选定清晰度的结果：补做的那次探测（可能为空）与选定的来源。
type VariantSelection struct {
	Probe  *engine.ProbeResult
	Source *hls.Source
}

// Intake 是「链接 → 任务」这条链路的实现。它无状态，可以按入口各建一个。
type Intake struct {
	eng      Engine
	settings SettingsProvider
}

// New 构造一个 intake。settings 与 eng 由调用方持有，intake 不缓存它们的快照。
func New(eng Engine, settings SettingsProvider) *Intake {
	return &Intake{eng: eng, settings: settings}
}

// Begin 给出一次请求的初始目标：建议名字、分类落点、占用冲突与重复裁决。
// 它只读本地任务库与磁盘，不联网。reserved 是调用方已经发给其它排队项的名字
// （只有文件信息窗口有这个来源），直下载入口传 nil。
func (in *Intake) Begin(ctx context.Context, req Request, reserved engine.Reserved) Target {
	st := in.settings.Get()
	suggested := engine.CleanFilename(req.Filename)
	t := Target{
		URL:            strings.TrimSpace(req.URL),
		Headers:        req.Headers,
		PageURL:        req.PageURL,
		MaxConn:        req.MaxConn,
		mode:           req.Mode,
		variantURI:     req.VariantURI,
		namedByRequest: suggested != "",
		TotalBytes:     -1,
	}
	if t.MaxConn <= 0 {
		t.MaxConn = st.Download.DefaultConnectionsPerTask
	}

	var dup *task.Task
	if t.URL != "" {
		dup, _ = in.eng.FindDuplicateTaskWithVariant(ctx, t.URL, req.VariantURI)
	}

	filename := suggested
	if filename == "" && t.URL != "" {
		filename = engine.URLFilename(t.URL)
	}
	if filename == "" && dup != nil && dup.Filename != "" {
		filename = dup.Filename
	}
	if filename == "" && t.URL != "" {
		filename = engine.DefaultFilename
	}
	// 清单的成品是处理出来的 MP4：入队时就定下名字与后缀，避免探测前按 .m3u8 判定
	// 导致分类与目录跳变。
	if isPlaylistRequest(req.VariantURI, t.URL, filename) {
		filename = engine.HLSOutputName(t.URL, filename)
	}

	t.Filename = filename
	t.Directory = req.Directory
	t.dirFromCategory = req.Directory == ""
	if filename != "" {
		// 分类只决定目录；手动指定目录时仍然给出命中分类，供界面展示与「记住分类」使用。
		cat, resolvedDir := st.Download.ResolveDestination(filename)
		t.CategoryID, t.CategoryDirectory = cat.ID, resolvedDir
		if t.Directory == "" {
			t.Directory = resolvedDir
		}
	} else if t.Directory == "" {
		t.Directory = st.Download.DefaultDirectory
	}

	occupancy := in.eng.Occupancy(ctx, reserved)
	if filename != "" {
		// 占用判定取一次，它同时给出冲突提示与序号副本名——两者必须是同一个答案。
		t.SuggestedFilename = occupancy.Suggest(t.Directory, t.Filename)
		t.FileConflict = occupancy.Taken(t.Directory, t.Filename)
	}
	t.DuplicateTask = dup
	t.DuplicateDecision = in.decide(st.Download, &t)
	in.applyDefaultAction(&t, occupancy)
	return t
}

// Probe 探测链接，并在请求已经带着清晰度时把它解析成这次下载的事实。
// 联网，调用方必须在锁外调用。
func (in *Intake) Probe(ctx context.Context, t Target) (*engine.ProbeResult, error) {
	probe, err := in.eng.ProbeURL(ctx, t.URL, t.Headers)
	if err != nil || probe == nil || probe.HLS == nil {
		return probe, err
	}
	if t.variantURI != "" {
		src, resolveErr := in.eng.ResolveHLSVariant(ctx, t.URL, t.variantURI, t.Headers)
		if resolveErr != nil {
			return probe, resolveErr
		}
		engine.ApplyHLSSelection(probe, src)
		return probe, nil
	}
	// 直下载入口没有窗口可问：多清晰度时取清单给出的第一个（扩展悬浮条已经替用户选过的
	// 情形会走上面的分支）。文件信息窗口在这里停下，由用户选。
	if t.mode == ModeDirect && probe.HLS.Media == nil && len(probe.HLS.Variants) > 0 {
		src, resolveErr := in.eng.ResolveHLSVariant(ctx, t.URL, probe.HLS.Variants[0].URI, t.Headers)
		if resolveErr != nil {
			return probe, resolveErr
		}
		engine.ApplyHLSSelection(probe, src)
	}
	return probe, nil
}

// ApplyProbe 把探测结果落进目标：真名、HLS 事实、落点、占用与裁决都随之刷新。
// 它只读本地事实，不联网。返回这次事实是否已经允许启动预下载。
func (in *Intake) ApplyProbe(ctx context.Context, t *Target, probe *engine.ProbeResult, probeErr error, reserved engine.Reserved) bool {
	t.probe, t.probeErr = probe, probeErr
	if probe == nil {
		// 探测连兜底文件名都没给（引擎没做出任何结论）：保持登记时的样子，也不启动预下载。
		return false
	}

	st := in.settings.Get()
	t.TotalBytes = probe.TotalBytes
	t.MimeType = probe.ContentType
	t.Resumable = probe.Resumable
	applyHLSFacts(t, probe)

	if t.URL != "" {
		dup, _ := in.eng.FindDuplicateTaskWithVariant(ctx, t.URL, t.variantURIOfMedia())
		t.DuplicateTask = dup
	}

	if t.adoptsProbeFilename(probe) {
		t.Filename = probe.Filename
		// 真名补齐后必须按它重算命中分类与保存目录：登记时只能拿 URL 猜名字，而真实后缀
		// 常常只存在于响应头（例如 GitHub 资产链接的路径里只有一个 GUID，没有后缀）。
		in.refreshDestination(st, t)
	}
	if probe.HLS != nil && len(probe.HLS.Variants) > 1 && probe.HLS.Media != nil {
		t.Filename = engine.HLSVariantFilename(t.Filename, probe.HLS.Media.Variant)
		in.refreshDestination(st, t)
	}

	occupancy := in.eng.Occupancy(ctx, reserved)
	t.SuggestedFilename = occupancy.Suggest(t.Directory, t.Filename)
	t.FileConflict = occupancy.Taken(t.Directory, t.Filename)
	t.DuplicateDecision = in.decide(st.Download, t)
	in.applyDefaultAction(t, occupancy)

	return in.preDownloadReady(t)
}

// SelectVariant 解析用户在窗口里选定的清晰度。手上没有探测结果时（手输链接）先补一次探测，
// 结果随 Selection 交回，提交沿用同一份事实。
// 联网，调用方必须在锁外调用。
func (in *Intake) SelectVariant(ctx context.Context, t Target, playlistURL, variantURI string) (*VariantSelection, error) {
	probe := t.probe
	if probe != nil && probe.URL != playlistURL {
		// 用户改过链接：手上这份元数据已经不属于它了。
		probe = nil
	}
	if probe == nil {
		fresh, err := in.eng.ProbeURL(ctx, playlistURL, t.Headers)
		if err != nil {
			return nil, err
		}
		if fresh == nil || fresh.HLS == nil {
			return nil, ErrNotPlaylist
		}
		probe = fresh
	} else if probe.HLS == nil {
		return nil, ErrNotPlaylist
	}

	src, err := in.eng.ResolveHLSVariant(ctx, playlistURL, variantURI, t.Headers)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, ErrVariantResolution
	}
	return &VariantSelection{Probe: probe, Source: src}, nil
}

// ApplyVariant 把选定的清晰度落进目标，并按它重算名字、落点、占用与裁决。
// 它只读本地事实，不联网。返回这次事实是否已经允许启动预下载。
func (in *Intake) ApplyVariant(ctx context.Context, t *Target, sel *VariantSelection, reserved engine.Reserved) bool {
	if sel == nil || sel.Source == nil {
		return false
	}
	if sel.Probe != nil {
		t.probe, t.probeErr = sel.Probe, nil
	}
	if t.probe == nil {
		return false
	}

	engine.ApplyHLSSelection(t.probe, sel.Source)
	applyHLSFacts(t, t.probe)

	st := in.settings.Get()
	if t.probe.HLS != nil && len(t.probe.HLS.Variants) > 1 {
		t.Filename = engine.HLSVariantFilename(t.Filename, sel.Source.Variant)
		in.refreshDestination(st, t)
	}

	occupancy := in.eng.Occupancy(ctx, reserved)
	t.SuggestedFilename = occupancy.Suggest(t.Directory, t.Filename)
	t.FileConflict = occupancy.Taken(t.Directory, t.Filename)

	lookupURL := t.URL
	if lookupURL == "" {
		lookupURL = sel.Source.PlaylistURL
	}
	if lookupURL != "" {
		// 同一份清单的不同清晰度是不同的下载目标：只认同清晰度的历史任务。
		dup, _ := in.eng.FindDuplicateTaskWithVariant(ctx, lookupURL, sel.Source.Variant.URI)
		t.DuplicateTask = dup
	}
	t.DuplicateDecision = in.decide(st.Download, t)

	return in.preDownloadReady(t)
}

// StartPreDownload 按目标已有的事实建一条预下载任务：名字、落点与请求上下文都取自目标，
// 探测结果直接沿用，不再为同一份元数据多付一次请求。联网，调用方必须在锁外调用。
func (in *Intake) StartPreDownload(ctx context.Context, t Target) (*task.Task, error) {
	req := engine.PreDownloadRequest{
		URL:        t.URL,
		Directory:  t.Directory,
		Filename:   t.Filename,
		MaxConn:    t.MaxConn,
		Headers:    t.Headers,
		PageURL:    t.PageURL,
		CategoryID: t.CategoryID,
	}
	// 探测失败时不把结果当成成功事实递下去：让引擎自己再探一次，由它按失败原因留下
	// 一条看得见的失败任务（A03），而不是把一次 403 变成一次「正常排队」。
	if t.probeErr == nil {
		req.Probe = t.probe
	}
	preTask, err := in.eng.StartPreDownloadFromProbe(ctx, req)
	if err != nil {
		return nil, err
	}
	in.adoptMetadata(ctx, preTask, t.PageURL, t.CategoryID)
	return preTask, nil
}

// CancelPreDownload 收掉一条预下载任务。
func (in *Intake) CancelPreDownload(ctx context.Context, taskID string) error {
	return in.eng.CancelPreDownload(ctx, taskID)
}

// Submit 按最终选择落成任务。它按最终落点重新裁决一次（用户可能改过目录或文件名），
// 并把这次裁决随 Result 交回，界面刷新时看到的是与事实一致的选项。
// 联网，调用方必须在锁外调用。
func (in *Intake) Submit(ctx context.Context, t Target, choice Choice) (Result, error) {
	res := Result{}
	st := in.settings.Get()
	in.applyChoice(&t, st, choice)

	res.Decision = in.decide(st.Download, &t)
	action := choice.Action
	if action == "" {
		action = res.Decision.Default
	}

	// 探测结果只在链接没被改过时才算数：用户改过链接，手上这份元数据已经不属于它了。
	probe := t.probe
	if probe != nil && probe.URL != t.URL {
		probe = nil
	}

	var (
		created *task.Task
		err     error
	)
	switch {
	case action == duplicate.ActionReuse && choice.ReuseTaskID != "":
		created, err = in.eng.ReuseExistingFile(ctx, choice.ReuseTaskID, t.Directory, t.Filename)
	case t.DuplicateTask != nil:
		if action == "" {
			// 局面需要用户先决定怎么处理；界面不能在没有选择的情况下提交。
			return res, ErrChoiceRequired
		}
		created, err = in.eng.ResolveDuplicateFromProbeWithMetadata(ctx, t.DuplicateTask.ID, string(action), t.Directory, t.Filename, t.MaxConn, probe, t.CategoryID)
	case choice.PreTaskID != "":
		created, err = in.eng.ConfirmPreDownload(ctx, choice.PreTaskID, t.Directory, t.Filename, t.MaxConn)
	case probe != nil:
		created, err = in.eng.AddTaskFromProbeWithMetadata(ctx, t.URL, t.Directory, t.Filename, t.MaxConn, t.Headers, probe, t.probeErr, t.PageURL, t.CategoryID)
	default:
		created, err = in.eng.AddTaskWithMetadata(ctx, t.URL, t.Directory, t.Filename, t.MaxConn, t.Headers, t.PageURL, t.CategoryID)
	}
	if err != nil {
		return res, err
	}
	res.Task = created
	// 确认预下载与复用成品这两条出口不接受页面地址与分类：建出来的任务缺这两项时补上，
	// 否则「下载来源」与「记住的分类」会在列表里丢掉。
	in.adoptMetadata(ctx, created, t.PageURL, t.CategoryID)
	return res, nil
}

// applyChoice 把用户在窗口里的最终选择落进目标。名字留空或仍是占位名时沿用目标里
// 已经确定的真名——用户没有改过名字，不该把探测补齐的真名丢掉。
func (in *Intake) applyChoice(t *Target, st config.Settings, choice Choice) {
	if choice.URL != "" {
		t.URL = choice.URL
	}
	if choice.Filename != "" && !isPlaceholderName(choice.Filename, t.URL) {
		t.Filename = choice.Filename
	}
	if choice.Directory != "" {
		t.Directory = choice.Directory
	}
	if choice.MaxConn > 0 {
		t.MaxConn = choice.MaxConn
	}
	if choice.CategoryID != "" {
		t.CategoryID = choice.CategoryID
	}
	if t.CategoryID == "" && t.Filename != "" {
		cat, _ := st.Download.ResolveDestination(t.Filename)
		t.CategoryID = cat.ID
	}
}

// adoptMetadata 把页面地址与分类补到任务上。这两个字段不在确认预下载与复用成品的入参里，
// 缺了就补一次，避免同一条下载在不同出口上丢掉「来源」与「分类」。
func (in *Intake) adoptMetadata(ctx context.Context, t *task.Task, pageURL, categoryID string) {
	if t == nil {
		return
	}
	if categoryID != "" && t.CategoryID != categoryID {
		t.CategoryID = categoryID
		_ = in.eng.SetTaskCategoryID(ctx, t.ID, categoryID)
	}
	if pageURL != "" && t.PageURL == "" {
		t.PageURL = pageURL
		_ = in.eng.SetTaskPageURL(ctx, t.ID, pageURL)
	}
}

// decide 按当前事实给出裁决。没有窗口可问时，需要用户决定的局面在这里补上安全答案，
// 于是两个入口拿到的是同一份结论，窗口只是多一步「让用户改」。
func (in *Intake) decide(download config.DownloadConfig, t *Target) duplicate.Decision {
	decision := duplicate.Decide(duplicate.Facts{
		Policy:              download.DuplicateURLPolicy,
		HasHistory:          t.DuplicateTask != nil,
		HistoryCompleted:    t.DuplicateTask != nil && t.DuplicateTask.Status == task.StatusCompleted,
		DestinationOccupied: engine.DestinationOccupied(t.Directory, t.Filename, t.DuplicateTask),
	})
	if t.mode == ModeDirect && decision.Default == "" && decision.Case == duplicate.CaseDestinationOccupied {
		decision.Default = directAction(t.DuplicateTask)
	}
	return decision
}

// directAction 是没有窗口可问时的答案：历史任务还活着就接着传，已经完成就另存副本。
// 前者保留已下载的分片，后者绝不静默覆盖磁盘上的成品。
func directAction(history *task.Task) duplicate.Action {
	if history != nil && history.Status == task.StatusCompleted {
		return duplicate.ActionCopy
	}
	return duplicate.ActionContinue
}

// applyDefaultAction 让目标名字与默认动作保持一致：默认动作是「序号副本」时，名字必须在
// 登记时就换成副本名，否则界面显示的落点与最终写下去的文件不是同一个。
//
// 直下载入口没有窗口可问：目标位置已有同名成品且没有历史任务时，这里同样取第一个可用的
// 副本名——覆盖一份用户没见过的成品不是「直接下载」应有的意思。
func (in *Intake) applyDefaultAction(t *Target, occupancy engine.Occupancy) {
	if t.Filename == "" {
		return
	}
	if t.DuplicateDecision.Default == duplicate.ActionCopy {
		t.Filename = occupancy.NumberedCopy(t.Directory, t.Filename)
		t.FileConflict = false
		return
	}
	if t.mode == ModeDirect && t.DuplicateTask == nil && t.FileConflict && t.SuggestedFilename != "" {
		t.Filename = t.SuggestedFilename
		t.FileConflict = false
	}
}

// refreshDestination 按名字重算命中分类与保存目录。只有目录本来就是按分类裁出来的才重算：
// 用户自己指定过目录时不改动它。
func (in *Intake) refreshDestination(st config.Settings, t *Target) {
	if !t.dirFromCategory {
		return
	}
	cat, resolvedDir := st.Download.ResolveDestination(t.Filename)
	t.CategoryID, t.Directory, t.CategoryDirectory = cat.ID, resolvedDir, resolvedDir
}

// preDownloadReady 报告当前事实是否允许启动预下载：有冲突、有历史任务、或清单还没选定
// 清晰度时都不行——那几种局面下先传起来的东西未必是用户最后要的那一份。
func (in *Intake) preDownloadReady(t *Target) bool {
	if t.FileConflict || t.DuplicateTask != nil {
		return false
	}
	return !t.NeedsVariantChoice()
}

// variantURIOfMedia 给出已经选定的清晰度地址；还没选时为空，交给调用方按空地址查找。
func (t *Target) variantURIOfMedia() string {
	if t.media != nil {
		return t.media.Variant.URI
	}
	return ""
}

// adoptsProbeFilename 报告探测给出的真名要不要替换当前名字：只有名字还是入口猜出来的
// （用户没填过）才替换；用户填的名字以用户为准。
func (t *Target) adoptsProbeFilename(probe *engine.ProbeResult) bool {
	return probe.Filename != "" && !t.namedByRequest && probe.Filename != t.Filename
}

// applyHLSFacts 把探测出的 HLS 事实落进目标：可选清晰度、已选定的那一版，以及它的大小与
// 时长。大小取自选定的版本而不是清单自己——清单的字节数不是这次下载的大小。
func applyHLSFacts(t *Target, probe *engine.ProbeResult) {
	if probe.HLS == nil {
		return
	}
	t.Variants = probe.HLS.Options
	src := probe.HLS.Media
	if src == nil {
		return
	}
	t.media = src
	t.QualityLabel = src.Variant.Label()
	t.MediaDuration = src.Duration
	t.TotalBytes = src.TotalBytes
}

// isPlaylistRequest 判定这次请求会不会产出 HLS 成品：扩展带来的清晰度、URL 后缀、或已经
// 从 URL 猜到的名字后缀任一命中。
func isPlaylistRequest(variantURI, urlStr, filename string) bool {
	if variantURI != "" {
		return true
	}
	if strings.HasSuffix(strings.ToLower(urlStr), ".m3u8") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(filename), ".m3u8")
}

// isPlaceholderName 报告这个名字是不是「没被用户改过」的占位名：调用方可以据此沿用目标里
// 已经确定的真名。
func isPlaceholderName(name, urlStr string) bool {
	return name == engine.DefaultFilename || name == engine.URLFilename(urlStr)
}
