package credentials

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestRequestCredentials_Masking(t *testing.T) {
	raw := map[string]string{
		"User-Agent":     "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		"Referer":        "https://example.com/download.html",
		"Cookie":         "SESSIONID=xyz123; user_pref=dark; auth=true",
		"Authorization":  "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.token",
		"X-Custom-Token": "secret-api-key-999",
		"X-Api-Key":      "very-sensitive-key",
	}

	creds := New(raw)

	// 1. Check MaskedHeaders
	masked := creds.MaskedHeaders()
	if masked["User-Agent"] != raw["User-Agent"] {
		t.Errorf("expected whitelisted User-Agent to be preserved, got %q", masked["User-Agent"])
	}
	if masked["Referer"] != raw["Referer"] {
		t.Errorf("expected whitelisted Referer to be preserved, got %q", masked["Referer"])
	}
	if masked["Cookie"] != "[present, 3 items, 43 chars]" {
		t.Errorf("unexpected cookie mask: %q", masked["Cookie"])
	}
	if masked["Authorization"] != "Bearer ****" {
		t.Errorf("unexpected authorization mask: %q", masked["Authorization"])
	}
	if masked["X-Custom-Token"] != "****" {
		t.Errorf("unexpected token mask: %q", masked["X-Custom-Token"])
	}
	if masked["X-Api-Key"] != "****" {
		t.Errorf("unexpected key mask: %q", masked["X-Api-Key"])
	}

	// 2. Check JSON Serialization (Must be masked)
	data, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if jsonStr == "" {
		t.Fatal("empty json output")
	}
	// Verify raw sensitive strings are NOT in the json
	if contains(jsonStr, "xyz123") || contains(jsonStr, "eyJhbGci") || contains(jsonStr, "secret-api-key") {
		t.Errorf("sensitive plaintext leaked into JSON: %s", jsonStr)
	}
	if !contains(jsonStr, "[present, 3 items, 43 chars]") {
		t.Errorf("expected masked cookie in JSON, got: %s", jsonStr)
	}
	if !contains(jsonStr, "Bearer ****") {
		t.Errorf("expected masked Bearer in JSON, got: %s", jsonStr)
	}

	// 3. Check fmt.Stringer (Must be masked)
	str := fmt.Sprintf("%v", creds)
	if contains(str, "xyz123") || contains(str, "eyJhbGci") || contains(str, "secret-api-key") {
		t.Errorf("sensitive plaintext leaked into Stringer: %s", str)
	}
	if !contains(str, "Bearer ****") {
		t.Errorf("expected masked Bearer in Stringer, got: %s", str)
	}

	// 4. Check ApplyToHTTPRequest (Must set plaintext headers on outgoing request)
	req, _ := http.NewRequest("GET", "https://example.com/file.zip", nil)
	creds.ApplyToHTTPRequest(req)
	if req.Header.Get("Cookie") != raw["Cookie"] {
		t.Errorf("expected raw cookie on http.Request, got %q", req.Header.Get("Cookie"))
	}
	if req.Header.Get("Authorization") != raw["Authorization"] {
		t.Errorf("expected raw auth on http.Request, got %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Custom-Token") != raw["X-Custom-Token"] {
		t.Errorf("expected raw token on http.Request, got %q", req.Header.Get("X-Custom-Token"))
	}

	// 5. Check RawHeaders
	rawExport := creds.RawHeaders()
	if rawExport["Cookie"] != raw["Cookie"] {
		t.Errorf("expected raw headers export to match original")
	}
}

func TestRequestCredentials_NewFromCookiesAndHeaders(t *testing.T) {
	creds := NewFromCookiesAndHeaders("foo=bar; baz=qux", map[string]string{
		"User-Agent": "TestAgent",
	})
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	creds.ApplyToHTTPRequest(req)
	if req.Header.Get("Cookie") != "foo=bar; baz=qux" {
		t.Errorf("expected cookie to be set, got %q", req.Header.Get("Cookie"))
	}
	if req.Header.Get("User-Agent") != "TestAgent" {
		t.Errorf("expected user-agent to be set, got %q", req.Header.Get("User-Agent"))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
