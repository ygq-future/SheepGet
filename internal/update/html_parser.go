package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

var (
	// tagRegex matches tags from /releases/tag/{tag}
	tagRegex = regexp.MustCompile(`/(?:releases/tag/|tree/)(v?[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[a-zA-Z0-9._-]*)?)`)
	// assetRegex matches href="/<repo>/releases/download/<tag>/<filename>"
	assetRegex = regexp.MustCompile(`(?i)href="((?:https?://[^/]+)?(/[^"]*/releases/download/[^/]+/([^"]+)))"`)
)

// ExtractTagFromRedirect extracts release tag from an HTTP response (Location header or final URL path).
func ExtractTagFromRedirect(resp *http.Response) string {
	if resp == nil {
		return ""
	}

	// 1. Check Location header from 301/302/307/308
	loc := resp.Header.Get("Location")
	if loc != "" {
		if m := tagRegex.FindStringSubmatch(loc); len(m) >= 2 {
			return m[1]
		}
	}

	// 2. Check current request URL if followed redirects
	if resp.Request != nil && resp.Request.URL != nil {
		path := resp.Request.URL.Path
		if m := tagRegex.FindStringSubmatch(path); len(m) >= 2 {
			return m[1]
		}
	}

	return ""
}

// ParseExpandedAssetsHTML parses GitHub's expanded_assets HTML fragment into GitHubAsset structs.
func ParseExpandedAssetsHTML(htmlContent, htmlBaseURL string) []GitHubAsset {
	var assets []GitHubAsset
	seen := make(map[string]bool)

	matches := assetRegex.FindAllStringSubmatch(htmlContent, -1)
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		rawHref := m[1]
		assetName := strings.TrimSpace(m[3])

		// Skip duplicate assets
		if seen[assetName] {
			continue
		}
		seen[assetName] = true

		fullURL := rawHref
		if !strings.HasPrefix(fullURL, "http://") && !strings.HasPrefix(fullURL, "https://") {
			base := strings.TrimRight(htmlBaseURL, "/")
			if !strings.HasPrefix(fullURL, "/") {
				fullURL = "/" + fullURL
			}
			fullURL = base + fullURL
		}

		assets = append(assets, GitHubAsset{
			Name:               assetName,
			BrowserDownloadURL: fullURL,
		})
	}

	return assets
}

// FetchReleasesViaHTML probes GitHub release webpage and expanded assets, entirely bypassing REST API quota.
func FetchReleasesViaHTML(ctx context.Context, client *http.Client, htmlBaseURL, repo string) ([]GitHubRelease, error) {
	if client == nil {
		client = http.DefaultClient
	}
	htmlBase := strings.TrimRight(htmlBaseURL, "/")
	if htmlBase == "" {
		htmlBase = "https://github.com"
	}

	latestURL := fmt.Sprintf("%s/%s/releases/latest", htmlBase, repo)

	// Step 1: Send request to releases/latest with redirects stopped or captured
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create latest release request failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	// Create a shallow copy of client to customize redirect behavior without affecting other calls
	redirectClient := *client
	redirectClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// Stop at the first redirect so we can extract Location directly
		return http.ErrUseLastResponse
	}

	resp, err := redirectClient.Do(req)
	if err != nil {
		// If redirectClient returned error other than ErrUseLastResponse, try normal client
		resp, err = client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch latest release redirect failed: %w", err)
		}
	}
	defer func() { _ = resp.Body.Close() }()

	tag := ExtractTagFromRedirect(resp)
	if tag == "" {
		// Fallback: Read body and search for release tag patterns
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		bodyStr := string(bodyBytes)
		if m := tagRegex.FindStringSubmatch(bodyStr); len(m) >= 2 {
			tag = m[1]
		}
	}

	if tag == "" {
		return nil, fmt.Errorf("could not determine latest release tag from %s (HTTP %d)", latestURL, resp.StatusCode)
	}

	// Step 2: Fetch expanded assets HTML for this tag
	assetsURL := fmt.Sprintf("%s/%s/releases/expanded_assets/%s", htmlBase, repo, tag)
	assetReq, err := http.NewRequestWithContext(ctx, http.MethodGet, assetsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create expanded_assets request failed: %w", err)
	}
	assetReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	assetResp, err := client.Do(assetReq)
	if err != nil {
		return nil, fmt.Errorf("fetch expanded_assets failed: %w", err)
	}
	defer func() { _ = assetResp.Body.Close() }()

	if assetResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expanded_assets endpoint returned HTTP %d", assetResp.StatusCode)
	}

	assetHTMLBytes, err := io.ReadAll(io.LimitReader(assetResp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read expanded_assets body failed: %w", err)
	}

	assets := ParseExpandedAssetsHTML(string(assetHTMLBytes), htmlBase)

	rel := GitHubRelease{
		TagName: tag,
		Name:    fmt.Sprintf("SheepGet %s", tag),
		HTMLURL: fmt.Sprintf("%s/%s/releases/tag/%s", htmlBase, repo, tag),
		Assets:  assets,
	}

	return []GitHubRelease{rel}, nil
}
