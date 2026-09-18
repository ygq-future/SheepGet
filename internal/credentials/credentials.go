// Package credentials provides secure-by-default encapsulation for HTTP request
// credentials (cookies, authorization headers, tokens) according to ADR-0005.
//
// Headers with sensitive tokens or cookies are automatically masked when formatted
// (fmt.Stringer) or serialized (json.Marshaler) to prevent accidental credential leakage
// in logs, error messages, or Wails event broadcasts to the frontend.
// Plaintext headers are only accessible via dedicated transport methods like ApplyToHTTPRequest.
package credentials

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Whitelist of header names permitted in plaintext in logs, events, and debug output.
var whitelistHeaders = map[string]bool{
	"user-agent":      true,
	"referer":         true,
	"accept":          true,
	"range":           true,
	"accept-language": true,
	"accept-encoding": true,
	"origin":          true,
	"host":            true,
	"connection":      true,
	"cache-control":   true,
	"content-type":    true,
	"content-length":  true,
}

// Substrings that mark a header as sensitive (case-insensitive).
var sensitiveSubstrings = []string{
	"cookie",
	"authorization",
	"token",
	"secret",
	"signature",
	"key",
}

// RequestCredentials wraps HTTP headers containing potentially sensitive credentials.
// It is a dedicated map type that implements json.Marshaler and fmt.Stringer to ensure
// default masking on serialization and formatting while maintaining type safety.
type RequestCredentials map[string]string

// New creates a RequestCredentials from a raw header map.
func New(headers map[string]string) RequestCredentials {
	if headers == nil {
		return nil
	}
	rc := make(RequestCredentials, len(headers))
	for k, v := range headers {
		if k != "" {
			rc[k] = v
		}
	}
	return rc
}

// NewFromCookiesAndHeaders creates RequestCredentials combining cookies string and custom headers.
func NewFromCookiesAndHeaders(cookies string, headers map[string]string) RequestCredentials {
	rc := New(headers)
	if rc == nil {
		rc = make(RequestCredentials)
	}
	if strings.TrimSpace(cookies) != "" {
		hasCookie := false
		for k := range rc {
			if strings.EqualFold(k, "cookie") {
				hasCookie = true
				break
			}
		}
		if !hasCookie {
			rc["Cookie"] = strings.TrimSpace(cookies)
		}
	}
	return rc
}

// IsSensitive checks whether a header name contains any sensitive keyword.
func IsSensitive(name string) bool {
	lower := strings.ToLower(name)
	for _, sub := range sensitiveSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// MaskValue computes the masked representation of a sensitive header value.
func MaskValue(name, val string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "cookie") {
		parts := strings.Split(val, ";")
		count := 0
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				count++
			}
		}
		return fmt.Sprintf("[present, %d items, %d chars]", count, len(val))
	}
	if strings.Contains(lower, "authorization") {
		trimmed := strings.TrimSpace(val)
		lowerVal := strings.ToLower(trimmed)
		if strings.HasPrefix(lowerVal, "bearer ") {
			return "Bearer ****"
		}
		if strings.HasPrefix(lowerVal, "basic ") {
			return "Basic ****"
		}
		return "****"
	}
	return "****"
}

// MaskedHeaders returns a copy of the headers where all sensitive headers are replaced
// with their masked representations. Whitelisted headers remain in plaintext.
func (rc RequestCredentials) MaskedHeaders() map[string]string {
	if len(rc) == 0 {
		return map[string]string{}
	}
	masked := make(map[string]string, len(rc))
	for k, v := range rc {
		lower := strings.ToLower(k)
		if IsSensitive(k) {
			masked[k] = MaskValue(k, v)
		} else if whitelistHeaders[lower] {
			masked[k] = v
		} else {
			masked[k] = "****"
		}
	}
	return masked
}

// ApplyToHTTPRequest copies the plaintext headers onto an outgoing HTTP request.
func (rc RequestCredentials) ApplyToHTTPRequest(req *http.Request) {
	if req == nil || len(rc) == 0 {
		return
	}
	for k, v := range rc {
		req.Header.Set(k, v)
	}
}

// RawHeaders returns a shallow copy of the plaintext headers map.
func (rc RequestCredentials) RawHeaders() map[string]string {
	if len(rc) == 0 {
		return nil
	}
	copyMap := make(map[string]string, len(rc))
	for k, v := range rc {
		copyMap[k] = v
	}
	return copyMap
}

// Clone returns a deep copy of the RequestCredentials.
func (rc RequestCredentials) Clone() RequestCredentials {
	if rc == nil {
		return nil
	}
	return New(rc)
}

// MarshalJSON serializes the credentials with sensitive fields automatically masked.
func (rc RequestCredentials) MarshalJSON() ([]byte, error) {
	if rc == nil {
		return []byte("null"), nil
	}
	return json.Marshal(rc.MaskedHeaders())
}

// String implements fmt.Stringer, formatting only masked credentials.
func (rc RequestCredentials) String() string {
	if len(rc) == 0 {
		return "RequestCredentials{}"
	}
	masked := rc.MaskedHeaders()
	keys := make([]string, 0, len(masked))
	for k := range masked {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("RequestCredentials{")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s: %s", k, masked[k])
	}
	return sb.String()
}
