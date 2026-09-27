package update

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		repoURL: fmt.Sprintf("https://api.github.com/repos/%s/releases", DefaultGitHubRepo),
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

// FetchReleases fetches the latest releases from GitHub.
// It transparently accepts both a JSON array of releases and a single release object (for /releases/latest or mocks).
func (c *Checker) FetchReleases(ctx context.Context) ([]GitHubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.repoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "SheepGet-AutoUpdater")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request releases failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d %s", resp.StatusCode, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty release payload")
	}

	if trimmed[0] == '[' {
		var list []GitHubRelease
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, fmt.Errorf("decode releases array failed: %w", err)
		}
		return list, nil
	}

	var single GitHubRelease
	if err := json.Unmarshal(trimmed, &single); err != nil {
		return nil, fmt.Errorf("decode single release failed: %w", err)
	}
	return []GitHubRelease{single}, nil
}

// FetchLatestRelease returns the most recent release from FetchReleases.
func (c *Checker) FetchLatestRelease(ctx context.Context) (*GitHubRelease, error) {
	releases, err := c.FetchReleases(ctx)
	if err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found")
	}
	return &releases[0], nil
}

// CheckAppUpdate compares currentVersion with the latest release and matches the suitable asset.
func (c *Checker) CheckAppUpdate(ctx context.Context, currentVersion string, goos, goarch string, isPortable bool) (*AppUpdateResult, error) {
	releases, err := c.FetchReleases(ctx)
	if err != nil {
		return nil, err
	}

	// Find the newest desktop application release (skips standalone extension tags like ext-v*)
	var rel *GitHubRelease
	for i := range releases {
		tagLower := strings.ToLower(releases[i].TagName)
		if strings.HasPrefix(tagLower, "ext-") {
			continue
		}
		rel = &releases[i]
		break
	}

	if rel == nil {
		return &AppUpdateResult{
			HasUpdate:      false,
			CurrentVersion: currentVersion,
			LatestVersion:  currentVersion,
			IsPortable:     isPortable,
		}, nil
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

// CheckExtensionUpdate compares currentExtVersion with the latest release extension asset.
func (c *Checker) CheckExtensionUpdate(ctx context.Context, currentExtVersion string) (*ExtensionUpdateResult, error) {
	releases, err := c.FetchReleases(ctx)
	if err != nil {
		return nil, err
	}

	// Find the newest release containing a browser extension asset
	var rel *GitHubRelease
	var matched *GitHubAsset
	for i := range releases {
		m := MatchExtensionAsset(releases[i].Assets)
		if m != nil {
			rel = &releases[i]
			matched = m
			break
		}
	}

	if rel == nil || matched == nil {
		return &ExtensionUpdateResult{
			HasUpdate:      false,
			CurrentVersion: currentExtVersion,
			LatestVersion:  currentExtVersion,
		}, nil
	}

	extVer := ExtractExtensionVersion(matched.Name)
	if extVer == "" {
		// Fallback to release tag name if asset name does not contain parseable version
		extVer = strings.TrimPrefix(rel.TagName, "v")
	}

	cmp, err := CompareVersions(extVer, currentExtVersion)
	if err != nil {
		return nil, fmt.Errorf("compare versions failed: %w", err)
	}

	res := &ExtensionUpdateResult{
		HasUpdate:      cmp > 0,
		CurrentVersion: currentExtVersion,
		LatestVersion:  extVer,
		ReleaseTitle:   rel.Name,
		ReleaseNotes:   rel.Body,
		AssetName:      matched.Name,
		AssetURL:       matched.BrowserDownloadURL,
		AssetSize:      matched.Size,
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
