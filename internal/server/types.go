package server

// SessionMetadata represents the runtime session information written to session.json
// so that local extensions and native messaging hosts can discover the active loopback server.
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
}

// HandoverResponse is returned synchronously to confirm whether SheepGet accepted the download.
type HandoverResponse struct {
	Accepted    bool   `json:"accepted"`
	QueueItemID string `json:"queueItemId,omitempty"`
	Reason      string `json:"reason,omitempty"`
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
