// Package task defines the download task domain models and storage interfaces.
package task

import (
	"context"
	"time"

	"sheep-get/internal/credentials"
	"sheep-get/internal/hls"
)

// Status represents the download task status.
type Status string

const (
	StatusQueued      Status = "queued"
	StatusDownloading Status = "downloading"
	StatusProcessing  Status = "processing"
	StatusPaused      Status = "paused"
	StatusCompleted   Status = "completed"
	StatusError       Status = "error"
	// StatusCollecting 是 zip 聚合任务在等待 Agent 登记完成员、尚未闭合时的状态。
	// 它不占下载名额，也不会被调度器当成排队任务启动：此时还没有任何一个字节要传输。
	StatusCollecting Status = "collecting"
)

// FailurePhase records which stage of the pipeline failed. 传输与处理是两条不同的失败路径：
// 传输失败可重新传输，处理失败只能复用已下载分片重试处理（ADR-0001/ADR-0004）。
// 它属于任务契约，使界面按任务事实选择重试动作，而不是从错误文案推断失败类型。
type FailurePhase string

const (
	FailurePhaseNone       FailurePhase = ""
	FailurePhaseTransfer   FailurePhase = "transfer"
	FailurePhaseProcessing FailurePhase = "processing"
)

// Chunk represents a segment of a file being downloaded.
type Chunk struct {
	Index      int   `json:"index"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Downloaded int64 `json:"downloaded"`
	Assisted   bool  `json:"assisted"`
	Completed  bool  `json:"completed"`
}

// ProgressDisplay 是单次下载对下载进度窗口展示行为的覆盖，字段为 nil 表示跟随配置中心。
//
// 它按任务落地而不是只存在请求里：这些取舍要到任务开始与完成那一刻才用得上，
// 而完成可能发生在暂停、重启、重试之后。
type ProgressDisplay struct {
	// ShowProgressWindow 覆盖「自动显示下载进度窗口」：这条任务开始与完成时是否主动呼出窗口。
	ShowProgressWindow *bool `json:"showProgressWindow,omitempty"`
	// KeepCompletedInfo 覆盖「保留完成信息」：完成后是否在窗口里留下这一条。
	KeepCompletedInfo *bool `json:"keepCompletedInfo,omitempty"`
	// FocusProgressWindowOnComplete 覆盖「完成后聚焦进度窗口」：完成时是否把窗口拉到最前。
	FocusProgressWindowOnComplete *bool `json:"focusProgressWindowOnComplete,omitempty"`
}

// ZipMember 是 zip 聚合任务里的一个成员文件。
//
// 成员不是独立任务：它没有自己的任务 ID、不广播自己的进度，只在所属 zip 任务里留下
// 续传与打包所需的元数据。Headers 是成员自己的请求上下文，事件载荷会把整个成员列表剥掉。
type ZipMember struct {
	URL          string  `json:"url"`
	FinalURL     string  `json:"finalUrl,omitempty"`
	EntryPath    string  `json:"entryPath"`
	Filename     string  `json:"filename"`
	StagingName  string  `json:"stagingName"`
	TotalBytes   int64   `json:"totalBytes"`
	Downloaded   int64   `json:"downloaded"`
	Done         bool    `json:"done"`
	Error        string  `json:"error,omitempty"`
	Resumable    bool    `json:"resumable,omitempty"`
	ETag         string  `json:"etag,omitempty"`
	LastModified string  `json:"lastModified,omitempty"`
	Chunks       []Chunk `json:"chunks,omitempty"`
	// Headers 是该成员的请求上下文（Referer/Cookie/Authorization…）。它只用于重试与续传，
	// 绝不进入任务事件：成员列表在广播前会被剥掉，避免把敏感头逐帧发给界面。
	Headers        map[string]string `json:"headers,omitempty"`
	DirectTransfer bool              `json:"directTransfer,omitempty"`
	ProxyAddr      string            `json:"proxyAddr,omitempty"`
}

// ZipBundle 是 zip 聚合任务的批次事实。一个批次只对应一个 Task：成员登记、下载与打包
// 都围绕它进行，任务列表与进度窗口因此只出现一条记录。
type ZipBundle struct {
	ZipID string `json:"zipId"`
	// RequestedName 是调用方请求的 zip 名字（清洗后），用于判断后续成员是否仍在同一批次；
	// Name 是实际落点上的文件名（可能因占用而带上序号副本）。
	RequestedName string      `json:"requestedName,omitempty"`
	Name          string      `json:"name"`
	Sealed        bool        `json:"sealed"`
	FilesTotal    int         `json:"filesTotal"`
	FilesDone     int         `json:"filesDone"`
	KnownBytes    int64       `json:"knownBytes"`
	PackagedFiles int         `json:"packagedFiles,omitempty"`
	Members       []ZipMember `json:"members,omitempty"`
}

// Task represents a download task in SheepGet.
type Task struct {
	ID  string `json:"id"`
	URL string `json:"url"`
	// FinalURL 是重定向收敛后的最终下载直链。
	// 当 URL 指向镜像分发网关或多级 302 重定向时，分块下载与续传请求直接连接 FinalURL，
	// 避免并发分块被轮询到不同镜像节点导致 ETag 变动或 412 错误；同时保留原始 URL 供界面展示与追溯。
	FinalURL string `json:"finalUrl,omitempty"`
	// PageURL 是该下载任务的来源网页地址（如浏览器扩展捕获时所在的源页面）。
	// 当下载链接失效或用户需要时，可直接在浏览器中重新打开该源网页。
	PageURL   string `json:"pageUrl,omitempty"`
	Filename  string `json:"filename"`
	Directory string `json:"directory"`
	// CategoryID 是该下载任务命中的分类 ID（如 "builtin-video"、"custom-game"）。
	// 在任务创建时由后端的分类规则或文件信息窗口的用户选择判定并固化在任务上，
	// 作为历史归属的单一事实来源，界面直接读取该字段，不再重复推算。
	CategoryID     string       `json:"categoryId,omitempty"`
	TempDir        string       `json:"tempDir,omitempty"`
	TotalBytes     int64        `json:"totalBytes"`
	Downloaded     int64        `json:"downloaded"`
	Speed          int64        `json:"speed"` // bytes per second
	Status         Status       `json:"status"`
	FailurePhase   FailurePhase `json:"failurePhase,omitempty"`
	ErrorMsg       string       `json:"errorMsg,omitempty"`
	MaxConcurrency int          `json:"maxConcurrency"`
	Resumable      bool         `json:"resumable"`
	// DirectTransfer 标记本次下载是否强制走直连（当全局配置了代理但所有代理探测/传输失败后直连成功时置为 true）。
	DirectTransfer bool `json:"directTransfer,omitempty"`
	// ProxyAddr 记录本次下载在多代理轮询回退中锁定的可用自定义代理地址（为空表示使用默认首选或直连）。
	ProxyAddr    string `json:"proxyAddr,omitempty"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	// Duration 是媒体文件的时长（秒），0 表示还没识别出来或不是媒体文件。
	// 它在下载完成后由本地文件解析写入：那时文件已经落地，读它不需要任何网络请求。
	Duration  float64   `json:"duration,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// ElapsedNano 记录任务累计的实际下载与传输耗时（纳秒）。
	// 仅统计处于活跃下载/传输阶段的时间，不计入排队、暂停、网络重连等待或失败停滞时间。
	// 任务完成后固化为最终耗时；界面据此格式化为动态耗时时长（如 45s、5m20s、1h05m20s）。
	ElapsedNano int64 `json:"elapsedNano,omitempty"`

	// Chunks for multi-connection download state
	Chunks []Chunk `json:"chunks,omitempty"`

	// Media 是 HLS 任务选定的媒体来源（清单地址、清晰度、独立音轨、时长、分片数）。
	// 为空表示这是一次普通 HTTP 传输：一次请求得到的就是成品，没有处理阶段。
	// 续传、重试与重启都只靠它重新取回清单，不依赖上一次运行留下的内存状态。
	Media *hls.Source `json:"media,omitempty"`
	// SegmentsDone/SegmentsTotal 是 HLS 传输的分片进度，界面据此显示「分片 N/M」。
	// 分片下载是多路并发进行的，但字节数进度条本身看不出这一点；有了这个计数，
	// 「正在并发抓取分片」对用户才是可见的。普通 HTTP 传输两个字段都是 0，界面不显示。
	SegmentsDone  int `json:"segmentsDone,omitempty"`
	SegmentsTotal int `json:"segmentsTotal,omitempty"`
	// SegmentDone 是每个分片的完成状态，索引即分片序号（视频清单在前、音轨清单在后拼接）。
	// 界面据此把进度条画成一格一分片的分段条——和普通 HTTP 的多线程分段同样的视觉，
	// 否则 HLS 只有一根从头到尾的实心条，多路并发完全看不出来。普通 HTTP 传输为空。
	SegmentDone []bool `json:"segmentDone,omitempty"`
	// MediaInputs 是本次传输已经落盘、要交给 Media Processor 的输入（分片目录内的文件名）。
	// 传输与处理是两条不同的失败路径（ADR-0004）：有了它，重试处理直接用现成输入，
	// 不必再为取一次清单把网络再走一遍。
	MediaInputs *hls.Inputs `json:"mediaInputs,omitempty"`
	// TransferDone 表示 MediaInputs 已经全部就绪，剩下的只是处理。
	// 它是「传输已完成」这一事实的唯一记录——处理失败后不能靠重新传输来恢复。
	TransferDone bool `json:"transferDone,omitempty"`

	// Bundle 非空表示这是一条 zip 聚合任务：URL 为空，成员与批次事实都在这里。
	// 成员不建独立任务，因此任务列表里只会看到这一条。
	Bundle *ZipBundle `json:"bundle,omitempty"`

	// ProgressDisplay 是本次下载在下载进度窗口中的展示约定（HTTP 直连接口按次传入的覆盖）。
	// 为 nil 时完全跟随配置中心；有值时只覆盖给出的那几项。
	ProgressDisplay *ProgressDisplay `json:"progressDisplay,omitempty"`

	// RequestHeaders carries request context (Referer, Cookie, Authorization, …) required by
	// links whose authorization has expired; applied to every probe and transfer request.
	// Encapsulated in RequestCredentials to ensure sensitive fields are masked by default on serialization.
	RequestHeaders credentials.RequestCredentials `json:"requestHeaders,omitempty"`
}

// IsHLS reports whether this task downloads an HLS playlist and therefore has a processing stage.
func (t *Task) IsHLS() bool {
	return t != nil && t.Media != nil
}

// IsBundle reports whether this task aggregates several files into one zip.
func (t *Task) IsBundle() bool {
	return t != nil && t.Bundle != nil
}

// NeedsProcessing reports whether the task has a stage after transfer (HLS muxing or zip packaging).
func (t *Task) NeedsProcessing() bool {
	return t.IsHLS() || t.IsBundle()
}

// EffectiveURL 返回本次下载实际应当连接的目标地址：若解析出了 FinalURL 则优先使用，否则使用原始 URL。
func (t *Task) EffectiveURL() string {
	if t != nil && t.FinalURL != "" {
		return t.FinalURL
	}
	if t != nil {
		return t.URL
	}
	return ""
}

// Clone returns a copy that shares nothing mutable with the original: the slices written during
// a transfer (Chunks, SegmentDone) and the request credentials are duplicated.
//
// 传输期间任务对象仍在被改写（分片由工作协程并发更新），而持久化与事件广播都会读出整个结构，
// 因此跨模块交出任务之前一律先 Clone：调用方拿到的是自有副本，此后谁也改不动它。
func (t *Task) Clone() *Task {
	if t == nil {
		return nil
	}
	cloned := *t
	if len(t.Chunks) > 0 {
		cloned.Chunks = make([]Chunk, len(t.Chunks))
		copy(cloned.Chunks, t.Chunks)
	}
	if len(t.SegmentDone) > 0 {
		cloned.SegmentDone = make([]bool, len(t.SegmentDone))
		copy(cloned.SegmentDone, t.SegmentDone)
	}
	if t.RequestHeaders != nil {
		cloned.RequestHeaders = t.RequestHeaders.Clone()
	}
	if t.Bundle != nil {
		bundle := *t.Bundle
		if len(t.Bundle.Members) > 0 {
			bundle.Members = make([]ZipMember, len(t.Bundle.Members))
			for i, member := range t.Bundle.Members {
				if len(member.Chunks) > 0 {
					member.Chunks = append([]Chunk(nil), member.Chunks...)
				}
				if member.Headers != nil {
					headers := make(map[string]string, len(member.Headers))
					for name, value := range member.Headers {
						headers[name] = value
					}
					member.Headers = headers
				}
				bundle.Members[i] = member
			}
		}
		cloned.Bundle = &bundle
	}
	return &cloned
}

// EventView 返回一份可以安全广播的副本：普通任务就是 Clone；zip 聚合任务剥掉成员明细，
// 只留下文件计数与大小汇总。成员 URL、请求头与分片状态没有展示价值，却会让每一帧事件
// 随成员数线性膨胀——聚合任务的事件载荷必须与成员数无关。
func (t *Task) EventView() *Task {
	if t == nil {
		return nil
	}
	if t.Bundle == nil {
		return t.Clone()
	}
	view := *t
	bundle := *t.Bundle
	bundle.Members = nil
	view.Bundle = &bundle
	view.Chunks = nil
	view.SegmentDone = nil
	if t.RequestHeaders != nil {
		view.RequestHeaders = t.RequestHeaders.Clone()
	}
	return &view
}

// TaskStore defines the storage interface for persisting and querying tasks.
type TaskStore interface {
	Save(ctx context.Context, t *Task) error
	// SaveMany 持久化一批变更，调用方只需要一次写盘。
	SaveMany(ctx context.Context, tasks []*Task) error
	Get(ctx context.Context, id string) (*Task, error)
	List(ctx context.Context) ([]*Task, error)
	Delete(ctx context.Context, id string) error
}
