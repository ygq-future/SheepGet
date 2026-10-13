//go:build windows

package engine

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
)

type winProxyCache struct {
	mu        sync.Mutex
	enabled   bool
	server    string
	override  string
	expiresAt time.Time
}

var globalWinProxyCache winProxyCache

func readWindowsRegistryProxy() (bool, string, string) {
	globalWinProxyCache.mu.Lock()
	defer globalWinProxyCache.mu.Unlock()

	now := time.Now()
	if now.Before(globalWinProxyCache.expiresAt) {
		return globalWinProxyCache.enabled, globalWinProxyCache.server, globalWinProxyCache.override
	}

	enabled := false
	server := ""
	override := ""

	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err == nil {
		defer func() { _ = key.Close() }()
		if val, _, err := key.GetIntegerValue("ProxyEnable"); err == nil && val != 0 {
			enabled = true
		}
		if str, _, err := key.GetStringValue("ProxyServer"); err == nil {
			server = strings.TrimSpace(str)
		}
		if str, _, err := key.GetStringValue("ProxyOverride"); err == nil {
			override = strings.TrimSpace(str)
		}
	}

	globalWinProxyCache.enabled = enabled
	globalWinProxyCache.server = server
	globalWinProxyCache.override = override
	globalWinProxyCache.expiresAt = now.Add(5 * time.Second)

	return enabled, server, override
}

func systemProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		// 1. 优先读取环境变量（HTTP_PROXY/HTTPS_PROXY/NO_PROXY）
		if proxyURL, err := http.ProxyFromEnvironment(req); err != nil || proxyURL != nil {
			return proxyURL, err
		}

		if req == nil || req.URL == nil {
			return nil, nil
		}

		// 2. 环境变量未设置代理时，回退读取 Windows 系统注册表
		enabled, serverStr, overrideStr := readWindowsRegistryProxy()
		if !enabled || serverStr == "" {
			return nil, nil
		}

		host := req.URL.Hostname()
		// 检查 ProxyOverride
		if overrideStr != "" {
			rules := strings.Split(overrideStr, ";")
			for _, r := range rules {
				r = strings.TrimSpace(r)
				if r == "" {
					continue
				}
				if r == "<local>" && (host == "localhost" || host == "127.0.0.1" || !strings.Contains(host, ".")) {
					return nil, nil
				}
				if strings.EqualFold(r, host) {
					return nil, nil
				}
				if strings.HasPrefix(r, "*.") && strings.HasSuffix(strings.ToLower(host), strings.ToLower(r[1:])) {
					return nil, nil
				}
			}
		}

		var rawProxy string
		if strings.Contains(serverStr, "=") {
			pairs := strings.Split(serverStr, ";")
			for _, pair := range pairs {
				parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], req.URL.Scheme) {
					rawProxy = parts[1]
					break
				}
			}
			if rawProxy == "" {
				for _, pair := range pairs {
					parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
					if len(parts) == 2 && strings.EqualFold(parts[0], "http") {
						rawProxy = parts[1]
						break
					}
				}
			}
		} else {
			rawProxy = serverStr
		}

		if rawProxy == "" {
			return nil, nil
		}

		if !strings.HasPrefix(rawProxy, "http://") && !strings.HasPrefix(rawProxy, "https://") && !strings.HasPrefix(rawProxy, "socks5://") {
			rawProxy = "http://" + rawProxy
		}

		return url.Parse(rawProxy)
	}
}

func isSystemProxyActive() bool {
	if os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("http_proxy") != "" || os.Getenv("https_proxy") != "" {
		return true
	}
	enabled, server, _ := readWindowsRegistryProxy()
	return enabled && server != ""
}
