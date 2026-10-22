package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTMLParser_ExtractTagAndParseAssets(t *testing.T) {
	// 1. Mock server simulating GitHub releases/latest and releases/expanded_assets/{tag}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test-owner/test-repo/releases/latest":
			http.Redirect(w, r, "/test-owner/test-repo/releases/tag/v1.5.0", http.StatusFound)
		case "/test-owner/test-repo/releases/expanded_assets/v1.5.0":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
				<div class="Box">
					<ul>
						<li>
							<a href="/test-owner/test-repo/releases/download/v1.5.0/SheepGet_1.5.0_windows-x64-portable.zip">
								<span>SheepGet_1.5.0_windows-x64-portable.zip</span>
							</a>
						</li>
						<li>
							<a href="/test-owner/test-repo/releases/download/v1.5.0/SheepGet_1.5.0_x64-setup.exe">
								<span>SheepGet_1.5.0_x64-setup.exe</span>
							</a>
						</li>
						<li>
							<a href="/test-owner/test-repo/releases/download/v1.5.0/SheepGet_1.5.0_arm64.dmg">
								<span>SheepGet_1.5.0_arm64.dmg</span>
							</a>
						</li>
					</ul>
				</div>
			`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	releases, err := FetchReleasesViaHTML(context.Background(), ts.Client(), ts.URL, "test-owner/test-repo")
	if err != nil {
		t.Fatalf("FetchReleasesViaHTML failed: %v", err)
	}

	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}

	rel := releases[0]
	if rel.TagName != "v1.5.0" {
		t.Errorf("expected TagName 'v1.5.0', got %s", rel.TagName)
	}
	if len(rel.Assets) != 3 {
		t.Fatalf("expected 3 assets, got %d", len(rel.Assets))
	}

	expectedAssets := []string{
		"SheepGet_1.5.0_windows-x64-portable.zip",
		"SheepGet_1.5.0_x64-setup.exe",
		"SheepGet_1.5.0_arm64.dmg",
	}

	for i, name := range expectedAssets {
		if rel.Assets[i].Name != name {
			t.Errorf("asset %d name expected %s, got %s", i, name, rel.Assets[i].Name)
		}
		expectedPrefix := ts.URL + "/test-owner/test-repo/releases/download/v1.5.0/"
		if rel.Assets[i].BrowserDownloadURL != expectedPrefix+name {
			t.Errorf("asset %d URL expected %s%s, got %s", i, expectedPrefix, name, rel.Assets[i].BrowserDownloadURL)
		}
	}
}
