package sys

import (
	"os"
	"testing"
)

func TestCandidateSystemProxyAddr(t *testing.T) {
	const testProxy = "http://127.0.0.1:9999"
	orig := os.Getenv("HTTP_PROXY")
	defer func() {
		if orig != "" {
			_ = os.Setenv("HTTP_PROXY", orig)
		} else {
			_ = os.Unsetenv("HTTP_PROXY")
		}
	}()

	_ = os.Setenv("HTTP_PROXY", testProxy)
	addr := CandidateSystemProxyAddr()
	if addr != testProxy {
		t.Fatalf("expected %s, got %s", testProxy, addr)
	}

	if !IsSystemProxyActive() {
		t.Fatalf("expected IsSystemProxyActive = true")
	}
}
