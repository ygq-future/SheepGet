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
