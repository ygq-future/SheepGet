//go:build !windows

package engine

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

func systemProxyFunc() func(*http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment
}

func isSystemProxyActive() bool {
	return os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("http_proxy") != "" || os.Getenv("https_proxy") != ""
}

func candidateSystemProxyAddr() string {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return val
		}
	}
	return ""
}
