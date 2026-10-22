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

	"sheep-get/internal/sys"
)

const (
	DefaultGitHubRepo = "ygq-future/SheepGet"
	DefaultTimeout    = 30 * time.Second
)

// Checker checks for updates from GitHub Releases using REST API with HTML fallback and multi-proxy resilience.
type Checker struct {
	repoURL     string
	htmlBaseURL string
	repo        string
	httpClient  *http.Client
	proxyChain  []string
	activeNode  string
	timeout     time.Duration
}

// CheckerOption configures the Checker.
type CheckerOption func(*Checker)

// WithBaseURL sets a custom releases API endpoint URL (primarily for testing).
func WithBaseURL(urlStr string) CheckerOption {
	return func(c *Checker) {
		c.repoURL = urlStr
	}
}

// WithHTMLBaseURL sets a custom HTML releases base URL (primarily for testing).
func WithHTMLBaseURL(urlStr string) CheckerOption {
	return func(c *Checker) {
		c.htmlBaseURL = strings.TrimRight(urlStr, "/")
	}
}

// WithRepo sets the repository in "owner/repo" format.
func WithRepo(repo string) CheckerOption {
	return func(c *Checker) {
		c.repo = repo
	}
}

// WithHTTPClient sets a custom base HTTP client.
func WithHTTPClient(client *http.Client) CheckerOption {
	return func(c *Checker) {
		c.httpClient = client
	}
}

// WithTimeout sets the request timeout for checking updates.
func WithTimeout(d time.Duration) CheckerOption {
	return func(c *Checker) {
		c.timeout = d
	}
}

// WithProxyChain sets an explicit proxy candidate chain.
func WithProxyChain(chain ...string) CheckerOption {
	return func(c *Checker) {
		c.proxyChain = append([]string(nil), chain...)
	}
}

// NewChecker creates a new Checker instance.
func NewChecker(opts ...CheckerOption) *Checker {
	c := &Checker{
		repo:        DefaultGitHubRepo,
		repoURL:     fmt.Sprintf("https://api.github.com/repos/%s/releases", DefaultGitHubRepo),
		htmlBaseURL: "https://github.com",
		timeout:     DefaultTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{
			Timeout: c.timeout,
		}
	}
	return c
}

// ActiveNode returns the node (proxy URL or "direct") that successfully performed the last update check.
func (c *Checker) ActiveNode() string {
	return c.activeNode
}

// ProxyChain returns the current candidate chain of proxy nodes.
func (c *Checker) ProxyChain() []string {
	return append([]string(nil), c.proxyChain...)
}

// BuildCandidateChain constructs an ordered proxy fallback chain based on mode and custom addresses:
// - "custom": customAddrs... -> candidateSystemProxy (if present) -> "direct"
// - "system" or "none" / "direct": candidateSystemProxy (if present) -> "direct"
func BuildCandidateChain(mode string, customAddrs ...string) []string {
	var chain []string
	sysAddr := strings.TrimSpace(sys.CandidateSystemProxyAddr())

	switch strings.ToLower(mode) {
	case "custom":
		for _, addr := range customAddrs {
			trimmed := strings.TrimSpace(addr)
			if trimmed != "" && !sliceContains(chain, trimmed) {
				chain = append(chain, trimmed)
			}
		}
		if sysAddr != "" && !sliceContains(chain, sysAddr) {
			chain = append(chain, sysAddr)
		}
		if !sliceContains(chain, "direct") {
			chain = append(chain, "direct")
		}
	case "system", "none", "direct", "":
		if sysAddr != "" {
			chain = append(chain, sysAddr)
		}
		if !sliceContains(chain, "direct") {
			chain = append(chain, "direct")
		}
	default:
		chain = append(chain, "direct")
	}
	return chain
}

func sliceContains(slice []string, val string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, val) {
			return true
		}
	}
	return false
}

// ConfigureProxy updates the proxy settings and establishes the candidate proxy fallback chain.
func (c *Checker) ConfigureProxy(mode string, customAddrs ...string) error {
	chain := BuildCandidateChain(mode, customAddrs...)
	c.proxyChain = chain

	// Initialize the primary httpClient using the first candidate in the chain
	firstNode := "direct"
	if len(chain) > 0 {
		firstNode = chain[0]
	}
	client, err := c.clientForNode(firstNode)
	if err != nil {
		return err
	}
	c.httpClient = client
	return nil
}

func (c *Checker) clientForNode(node string) (*http.Client, error) {
	var transport *http.Transport
	var err error

	switch strings.ToLower(node) {
	case "direct", "":
		transport, err = createTransport("direct", "")
	case "system":
		transport, err = createTransport("system", "")
	default:
		transport, err = createTransport("custom", node)
	}

	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
	}, nil
}

// FetchReleases fetches the latest releases from GitHub.
// It iterates through the candidate proxy chain, attempting GitHub REST API first,
// and transparently falls back to zero-quota HTML inspection upon encountering HTTP 403 (Rate Limit).
func (c *Checker) FetchReleases(ctx context.Context) ([]GitHubRelease, error) {
	candidates := c.proxyChain
	if len(candidates) == 0 {
		// Single client mode (e.g. testing with WithHTTPClient without proxy chain)
		return c.fetchWithClient(ctx, c.httpClient)
	}

	var errors []string
	for _, node := range candidates {
		client, err := c.clientForNode(node)
		if err != nil {
			errors = append(errors, fmt.Sprintf("[%s] build client failed: %v", node, err))
			continue
		}

		releases, err := c.fetchWithClient(ctx, client)
		if err == nil && len(releases) > 0 {
			c.activeNode = node
			return releases, nil
		}

		errors = append(errors, fmt.Sprintf("[%s] %v", node, err))
	}

	return nil, fmt.Errorf("all proxy candidates failed: %s", strings.Join(errors, "; "))
}

func (c *Checker) fetchWithClient(ctx context.Context, client *http.Client) ([]GitHubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.repoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "SheepGet-AutoUpdater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request releases failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden {
		// HTTP 403 Rate Limit: TCP/TLS connection is healthy, but API rate limit exceeded.
		// Immediately fallback to HTML probing on this same healthy connection!
		htmlReleases, htmlErr := FetchReleasesViaHTML(ctx, client, c.htmlBaseURL, c.repo)
		if htmlErr == nil && len(htmlReleases) > 0 {
			return htmlReleases, nil
		}
		return nil, fmt.Errorf("api rate limited (403) and html fallback failed: %w", htmlErr)
	}

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

	// Find the desktop application release with the highest semantic version (skips standalone extension tags like ext-v*)
	var rel *GitHubRelease
	var bestAppVer string

	for i := range releases {
		// Standalone extension releases (ext-v*) are not desktop app releases
		if strings.HasPrefix(releases[i].TagName, "ext-v") {
			continue
		}

		v := strings.TrimPrefix(releases[i].TagName, "v")
		if bestAppVer == "" {
			bestAppVer = v
			rel = &releases[i]
			continue
		}

		cmp, err := CompareVersions(v, bestAppVer)
		if err == nil && cmp > 0 {
			bestAppVer = v
			rel = &releases[i]
		}
	}

	if rel == nil {
		return &AppUpdateResult{
			HasUpdate:      false,
			CurrentVersion: currentVersion,
			LatestVersion:  currentVersion,
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

// CheckExtensionUpdate compares currentExtVersion with the highest version extension asset across all releases.
func (c *Checker) CheckExtensionUpdate(ctx context.Context, currentExtVersion string) (*ExtensionUpdateResult, error) {
	releases, err := c.FetchReleases(ctx)
	if err != nil {
		return nil, err
	}

	var bestRel *GitHubRelease
	var bestMatched *GitHubAsset
	var bestVer string

	for i := range releases {
		m := MatchExtensionAsset(releases[i].Assets)
		if m == nil {
			continue
		}

		v := ExtractExtensionVersion(m.Name)
		if v == "" {
			v = strings.TrimPrefix(releases[i].TagName, "v")
			v = strings.TrimPrefix(v, "ext-v")
		}

		if bestVer == "" {
			bestRel = &releases[i]
			bestMatched = m
			bestVer = v
			continue
		}

		cmp, err := CompareVersions(v, bestVer)
		if err == nil && cmp > 0 {
			bestRel = &releases[i]
			bestMatched = m
			bestVer = v
		}
	}

	if bestRel == nil || bestMatched == nil {
		return &ExtensionUpdateResult{
			HasUpdate:      false,
			CurrentVersion: currentExtVersion,
			LatestVersion:  currentExtVersion,
		}, nil
	}

	cmp, err := CompareVersions(bestVer, currentExtVersion)
	if err != nil {
		return nil, fmt.Errorf("compare versions failed: %w", err)
	}
	res := &ExtensionUpdateResult{
		HasUpdate:      cmp > 0,
		CurrentVersion: currentExtVersion,
		LatestVersion:  bestVer,
		ReleaseTitle:   bestRel.Name,
		ReleaseNotes:   bestRel.Body,
		AssetName:      bestMatched.Name,
		AssetURL:       bestMatched.BrowserDownloadURL,
		AssetSize:      bestMatched.Size,
	}

	return res, nil
}

// createTransport constructs an http.RoundTripper respecting proxy settings.
func createTransport(mode, customAddr string) (*http.Transport, error) {
	var proxyFunc func(*http.Request) (*url.URL, error)
	switch strings.ToLower(mode) {
	case "direct", "none":
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
	case "system":
		proxyFunc = sys.SystemProxyFunc()
	default:
		proxyFunc = nil
	}

	return &http.Transport{
		Proxy: proxyFunc,
	}, nil
}
