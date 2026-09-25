package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultGitHubRepo = "ygq-future/SheepGet"
	DefaultTimeout    = 30 * time.Second
)

// Checker checks for updates from GitHub Releases.
type Checker struct {
	repoURL    string
	httpClient *http.Client
}

// CheckerOption configures the Checker.
type CheckerOption func(*Checker)

// WithBaseURL sets a custom releases endpoint URL (primarily for testing).
func WithBaseURL(urlStr string) CheckerOption {
	return func(c *Checker) {
		c.repoURL = urlStr
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) CheckerOption {
	return func(c *Checker) {
		c.httpClient = client
	}
}

// NewChecker creates a new Checker instance.
func NewChecker(opts ...CheckerOption) *Checker {
	c := &Checker{
		repoURL: fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", DefaultGitHubRepo),
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ConfigureProxy configures the HTTP client transport with the specified proxy settings.
func (c *Checker) ConfigureProxy(mode, customAddr string) error {
	transport, err := createTransport(mode, customAddr)
	if err != nil {
		return err
	}
	c.httpClient.Transport = transport
	return nil
}

// FetchLatestRelease fetches the latest GitHub release.
func (c *Checker) FetchLatestRelease(ctx context.Context) (*GitHubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.repoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "SheepGet-AutoUpdater")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request latest release failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d %s", resp.StatusCode, resp.Status)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release payload failed: %w", err)
	}
	return &rel, nil
}

// CheckAppUpdate compares currentVersion with the latest release and matches the suitable asset.
func (c *Checker) CheckAppUpdate(ctx context.Context, currentVersion string, goos, goarch string, isPortable bool) (*AppUpdateResult, error) {
	rel, err := c.FetchLatestRelease(ctx)
	if err != nil {
		return nil, err
	}

	latestVer := strings.TrimPrefix(rel.TagName, "v")
	cmp, err := CompareVersions(latestVer, currentVersion)
	if err != nil {
		return nil, fmt.Errorf("compare versions failed: %w", err)
	}

	res := &AppUpdateResult{
		HasUpdate:      cmp > 0,
		CurrentVersion: currentVersion,
		LatestVersion:  latestVer,
		ReleaseTitle:   rel.Name,
		ReleaseNotes:   rel.Body,
		ReleaseURL:     rel.HTMLURL,
		IsPortable:     isPortable,
	}

	matched := MatchAppAsset(rel.Assets, goos, goarch, isPortable)
	if matched != nil {
		res.AssetName = matched.Name
		res.AssetURL = matched.BrowserDownloadURL
		res.AssetSize = matched.Size
	}

	return res, nil
}

// CheckExtensionUpdate compares currentExtVersion with the latest release and matches the extension asset.
func (c *Checker) CheckExtensionUpdate(ctx context.Context, currentExtVersion string) (*ExtensionUpdateResult, error) {
	rel, err := c.FetchLatestRelease(ctx)
	if err != nil {
		return nil, err
	}

	latestVer := strings.TrimPrefix(rel.TagName, "v")
	cmp, err := CompareVersions(latestVer, currentExtVersion)
	if err != nil {
		return nil, fmt.Errorf("compare versions failed: %w", err)
	}

	res := &ExtensionUpdateResult{
		HasUpdate:      cmp > 0,
		CurrentVersion: currentExtVersion,
		LatestVersion:  latestVer,
		ReleaseTitle:   rel.Name,
		ReleaseNotes:   rel.Body,
	}

	matched := MatchExtensionAsset(rel.Assets)
	if matched != nil {
		res.AssetName = matched.Name
		res.AssetURL = matched.BrowserDownloadURL
		res.AssetSize = matched.Size
	}

	return res, nil
}

// createTransport constructs an http.RoundTripper respecting proxy settings.
func createTransport(mode, customAddr string) (*http.Transport, error) {
	var proxyFunc func(*http.Request) (*url.URL, error)
	switch strings.ToLower(mode) {
	case "direct":
		proxyFunc = nil
	case "custom":
		if customAddr != "" {
			parsed, err := url.Parse(customAddr)
			if err != nil {
				return nil, fmt.Errorf("invalid custom proxy url: %w", err)
			}
			proxyFunc = http.ProxyURL(parsed)
		} else {
			proxyFunc = nil
		}
	default: // "system"
		proxyFunc = http.ProxyFromEnvironment
	}

	return &http.Transport{
		Proxy: proxyFunc,
	}, nil
}
