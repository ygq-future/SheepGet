//go:build windows

package sys

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestCleanStartupApproved(t *testing.T) {
	testSubKey := `Software\SheepGetTest_StartupApproved`
	key, _, err := registry.CreateKey(registry.CURRENT_USER, testSubKey, registry.ALL_ACCESS)
	if err != nil {
		t.Fatalf("failed to create test registry key: %v", err)
	}
	defer func() {
		_ = key.Close()
		_ = registry.DeleteKey(registry.CURRENT_USER, testSubKey)
	}()

	disabledData := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if err := key.SetBinaryValue("sheepget", disabledData); err != nil {
		t.Fatalf("failed to write test disabled value: %v", err)
	}
	if err := key.SetBinaryValue("SheepGet", disabledData); err != nil {
		t.Fatalf("failed to write test disabled value: %v", err)
	}

	if err := cleanStartupApprovedAt(registry.CURRENT_USER, testSubKey, "sheepget", "SheepGet", "non_existent"); err != nil {
		t.Fatalf("cleanStartupApprovedAt failed: %v", err)
	}

	if _, _, err := key.GetBinaryValue("sheepget"); err != registry.ErrNotExist {
		t.Errorf("expected sheepget to be deleted, got err: %v", err)
	}
	if _, _, err := key.GetBinaryValue("SheepGet"); err != registry.ErrNotExist {
		t.Errorf("expected SheepGet to be deleted, got err: %v", err)
	}
}
