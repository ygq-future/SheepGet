package skill

import (
	"os"
	"strings"
	"testing"
)

func TestRender_WithAPIKey(t *testing.T) {
	out, err := Render(TemplateParams{
		Port:   9248,
		APIKey: "my-secret-token",
	})
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}

	content := string(out)
	if !strings.Contains(content, "name: sheep-get") {
		t.Errorf("expected frontmatter name: sheep-get")
	}
	if !strings.Contains(content, "http://127.0.0.1:9248") {
		t.Errorf("expected port 9248 in content")
	}
	if !strings.Contains(content, "X-SheepGet-Token: my-secret-token") {
		t.Errorf("expected api key my-secret-token in header")
	}
	if !strings.Contains(content, "Fixed API Key configured in Settings") {
		t.Errorf("expected fixed API key note")
	}
	if !strings.Contains(content, "description: Download files, media, HLS (.m3u8) streams, or many related small files") {
		t.Errorf("expected zip aggregation trigger in the skill description")
	}
	if !strings.Contains(content, "## Zip Aggregation") {
		t.Errorf("expected zip aggregation section")
	}
	if !strings.Contains(content, `"zipFinal": true`) || !strings.Contains(content, "zipId") {
		t.Errorf("expected zipId/zipFinal guidance")
	}
	if !strings.Contains(content, "Never rename members") {
		t.Errorf("expected member naming constraint")
	}
	if !strings.Contains(content, "Batch boundary: usage scenario, not source or file type") {
		t.Errorf("expected the batch boundary rule")
	}
	if !strings.Contains(content, "Different hosts are still one batch") {
		t.Errorf("expected cross-host batching guidance")
	}
	if !strings.Contains(content, "nested archive") {
		t.Errorf("expected nested archive guidance")
	}
	if !strings.Contains(content, "Never create a second zip task") {
		t.Errorf("expected the no-split rule")
	}
	if !strings.Contains(content, "ask before creating a second zip task") {
		t.Errorf("expected the ambiguity rule")
	}
	if !strings.Contains(content, "Plan before sealing") || !strings.Contains(content, "irreversible") {
		t.Errorf("expected the sealing irreversibility warning")
	}
	if !strings.Contains(content, "Breeze-TTS-2") {
		t.Errorf("expected the cross-host example")
	}
}

func TestRender_WithoutAPIKey(t *testing.T) {
	out, err := Render(TemplateParams{
		Port:   9250,
		APIKey: "",
	})
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}

	content := string(out)
	if !strings.Contains(content, "http://127.0.0.1:9250") {
		t.Errorf("expected port 9250 in content")
	}
	if !strings.Contains(content, "No fixed API Key configured") {
		t.Errorf("expected note about no fixed api key")
	}
}

func TestSync(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Initial Sync
	if err := Sync(tempDir, 9248, "initial-key"); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	target := TargetFile(tempDir)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read written file %s: %v", target, err)
	}
	if !strings.Contains(string(data), "initial-key") {
		t.Errorf("expected initial-key in written file")
	}

	// 2. Update Sync with new key and port
	if err := Sync(tempDir, 9252, "updated-key-12345"); err != nil {
		t.Fatalf("second Sync failed: %v", err)
	}

	data2, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}
	if !strings.Contains(string(data2), "updated-key-12345") {
		t.Errorf("expected updated-key-12345 in updated file")
	}
	if !strings.Contains(string(data2), "http://127.0.0.1:9252") {
		t.Errorf("expected updated port 9252")
	}
	if strings.Contains(string(data2), "initial-key") {
		t.Errorf("old key should no longer be present")
	}
}
