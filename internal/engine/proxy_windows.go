//go:build windows

package engine

import (
	"net/http"
	"net/url"

	"sheep-get/internal/sys"
)

func systemProxyFunc() func(*http.Request) (*url.URL, error) {
	return sys.SystemProxyFunc()
}

func isSystemProxyActive() bool {
	return sys.IsSystemProxyActive()
}

func candidateSystemProxyAddr() string {
	return sys.CandidateSystemProxyAddr()
}
