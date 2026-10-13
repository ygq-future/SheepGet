//go:build !windows

package engine

import (
	"net/http"
	"net/url"
	"os"
)

func systemProxyFunc() func(*http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment
}

func isSystemProxyActive() bool {
	return os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("http_proxy") != "" || os.Getenv("https_proxy") != ""
}
