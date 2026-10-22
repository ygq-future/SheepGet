//go:build !windows

package sys

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// SystemProxyFunc returns standard proxy detection from environment variables.
func SystemProxyFunc() func(*http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment
}

// IsSystemProxyActive checks if proxy environment variables are set.
func IsSystemProxyActive() bool {
	return os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("http_proxy") != "" || os.Getenv("https_proxy") != ""
}

// CandidateSystemProxyAddr returns proxy address from environment variables if present.
func CandidateSystemProxyAddr() string {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return val
		}
	}
	return ""
}
