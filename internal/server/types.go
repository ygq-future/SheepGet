package server

// SessionMetadata represents the runtime session information written to session.json
// and returned by the discovery probe endpoint.
type SessionMetadata struct {
	Port         int    `json:"port"`
	SessionToken string `json:"sessionToken"`
	PID          int    `json:"pid"`
	StartedAt    int64  `json:"startedAt"`
}

// PageContext carries metadata about the host page initiating the download.
type PageContext struct {
	PageURL   string `json:"pageUrl"`
	Referrer  string `json:"referrer,omitempty"`
	PageTitle string `json:"pageTitle,omitempty"`
}

// CredentialsPayload wraps cookies and headers transmitted by the browser extension.
type CredentialsPayload struct {
	Cookies string            `json:"cookies,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// HandoverRequest is the payload sent by the browser extension when delegating a download.
type HandoverRequest struct {
	SourceType         string              `json:"sourceType"` // "browser_takeover", "media_bar", "resource_list"
	URL                string              `json:"url"`
	FilenameSuggestion string              `json:"filenameSuggestion,omitempty"`
	TotalBytes         int64               `json:"totalBytes,omitempty"`
	MimeType           string              `json:"mimeType,omitempty"`
	PageContext        PageContext         `json:"pageContext"`
	Credentials        *CredentialsPayload `json:"credentials,omitempty"`
	MediaMeta          any                 `json:"mediaMeta,omitempty"`
	// VariantURI 是扩展悬浮条上已经选好的清晰度（清单里的一个 EXT-X-STREAM-INF 地址）。
	// 有它时桌面端不必再让用户选一次：多清晰度的选择已经发生在交接之前。
	VariantURI string `json:"variantUri,omitempty"`
}

// HandoverResponse is returned synchronously to confirm whether SheepGet accepted the download.
type HandoverResponse struct {
	Accepted    bool   `json:"accepted"`
	QueueItemID string `json:"queueItemId,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// CreateTaskRequest is the payload sent to POST /api/v1/tasks.
//
// 三个进度窗口参数是单次下载的覆盖：不传即跟随配置中心，与从桌面端建立的下载一致。
// 它们只在任务上生效，不影响其它任务，也不改变已保存的全局设置。
type CreateTaskRequest struct {
	URL       string            `json:"url"`
	Filename  string            `json:"filename,omitempty"`
	Directory string            `json:"directory,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	// ZipID 把这次下载登记进一个 zip 聚合批次。为空时就是普通下载，行为完全不变。
	ZipID string `json:"zipId,omitempty"`
	// ZipName 是批次最终 zip 的文件名（可带 .zip）。首个成员生效，后续传了必须一致。
	ZipName string `json:"zipName,omitempty"`
	// ZipPath 是成员在 zip 内的相对路径；为空时用成员文件名。
	ZipPath string `json:"zipPath,omitempty"`
	// ZipFinal 表示这是批次的最后一个成员，登记后立即闭合并开始下载。
	ZipFinal bool `json:"zipFinal,omitempty"`
	// ShowProgressWindow 覆盖「自动显示下载进度窗口」。
	ShowProgressWindow *bool `json:"showProgressWindow,omitempty"`
	// KeepCompletedInfo 覆盖「保留完成信息」。
	KeepCompletedInfo *bool `json:"keepCompletedInfo,omitempty"`
	// FocusProgressWindowOnComplete 覆盖「完成后聚焦进度窗口」；传 false 即这次静默下载：
	// 完成时不呼出进度窗口，也不把用户正在看的窗口抢到前台。
	FocusProgressWindowOnComplete *bool `json:"focusProgressWindowOnComplete,omitempty"`
}

// CreateTaskResponse is returned upon successful task creation.
//
// zip 聚合请求不会为成员创建任务：TaskID 为空，ZipTaskID 指向唯一的批次任务。
type CreateTaskResponse struct {
	TaskID    string `json:"taskId"`
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	Directory string `json:"directory"`
	Status    string `json:"status"`

	ZipTaskID string          `json:"zipTaskId,omitempty"`
	Zip       *ZipTaskState   `json:"zip,omitempty"`
	Member    *ZipMemberState `json:"member,omitempty"`
}

// ZipTaskState 是 zip 聚合批次对外的摘要，界面与 Agent 都只依赖这份状态。
type ZipTaskState struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Directory  string `json:"directory"`
	Files      int    `json:"files"`
	KnownBytes int64  `json:"knownBytes"`
	TotalBytes int64  `json:"totalBytes"`
	Sealed     bool   `json:"sealed"`
	Status     string `json:"status"`
}

// ZipMemberState 是本次登记成员的结果；Duplicate 表示这是一次幂等重试。
type ZipMemberState struct {
	Filename   string `json:"filename"`
	Path       string `json:"path"`
	TotalBytes int64  `json:"totalBytes"`
	Duplicate  bool   `json:"duplicate"`
}

// HLSVariantsRequest 是扩展悬浮条在交接前拉取清晰度列表的请求：清单地址加上取回清单
// 所需的请求上下文（Referer/Cookie）。防盗链或需登录的清单不带上下文只会探出 403。
type HLSVariantsRequest struct {
	URL         string              `json:"url"`
	Credentials *CredentialsPayload `json:"credentials,omitempty"`
}

// HLSVariantOption 是清晰度选项的展示形态，与桌面端文件信息窗口共用同一份命名。
type HLSVariantOption struct {
	URI       string `json:"uri"`
	Label     string `json:"label"`
	Bandwidth int    `json:"bandwidth,omitempty"`
}

// HLSVariantsResponse 是清晰度列表的响应。
type HLSVariantsResponse struct {
	Variants []HLSVariantOption `json:"variants"`
}

// MediaProbeRequest 是扩展面板为「提前显示时长与大小」发起的媒体概览探测。
// 除清单地址外还带嗅探时已知的事实（类型、大小、请求上下文），探测带着它们才准。
type MediaProbeRequest struct {
	URL         string              `json:"url"`
	Filename    string              `json:"filename,omitempty"`
	MimeType    string              `json:"mimeType,omitempty"`
	IsHls       bool                `json:"isHls,omitempty"`
	TotalBytes  int64               `json:"totalBytes,omitempty"`
	Credentials *CredentialsPayload `json:"credentials,omitempty"`
}

// MediaProbeResponse 是概览探测的结果。Variants 大于 1 表示清单有多个清晰度，
// 此时时长与大小一并不给——先选定清晰度才有「这次下载的大小」可言。
type MediaProbeResponse struct {
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	TotalBytes      int64   `json:"totalBytes,omitempty"`
	Variants        int     `json:"variants,omitempty"`
}

// TakeoverConfigSync mirrors the TakeoverConfig sent to the extension, with a version timestamp.
type TakeoverConfigSync struct {
	Version       int64    `json:"version"`
	Extensions    []string `json:"extensions"`
	ExcludedSites []string `json:"excludedSites"`
	PauseShortcut string   `json:"pauseShortcut"`
	ForceShortcut string   `json:"forceShortcut"`
}

// EventMessage represents a server-sent WebSocket event.
type EventMessage struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}
