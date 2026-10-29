package task_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"sheep-get/internal/task"
)

func TestFileTaskStore_ProcessingBundleBecomesPausedOnReload(t *testing.T) {
	storeFile := filepath.Join(t.TempDir(), "tasks.json")
	store, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	ctx := context.Background()

	bundle := &task.Task{
		ID:           "bundle-1",
		Filename:     "pack.zip",
		Directory:    t.TempDir(),
		Status:       task.StatusProcessing,
		TransferDone: true,
		Bundle: &task.ZipBundle{
			ZipID:      "pack-1",
			Name:       "pack.zip",
			Sealed:     true,
			FilesTotal: 1,
			Members: []task.ZipMember{{
				URL:         "https://example.com/a.txt",
				EntryPath:   "pack/a.txt",
				Filename:    "a.txt",
				StagingName: "m00001.part",
				TotalBytes:  5,
				Downloaded:  5,
				Done:        true,
				Headers:     map[string]string{"Authorization": "Bearer secret"},
				Chunks:      []task.Chunk{{Index: 0, Start: 0, End: 4, Downloaded: 5, Completed: true}},
			}},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.Save(ctx, bundle); err != nil {
		t.Fatalf("save bundle: %v", err)
	}

	reloaded, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	got, err := reloaded.Get(ctx, "bundle-1")
	if err != nil {
		t.Fatalf("get reloaded bundle: %v", err)
	}
	if got.Status != task.StatusPaused {
		t.Fatalf("expected processing bundle to become paused, got %s", got.Status)
	}
	if got.Bundle == nil || len(got.Bundle.Members) != 1 {
		t.Fatalf("expected bundle members to survive reload, got %+v", got.Bundle)
	}
	member := got.Bundle.Members[0]
	if member.Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("expected member headers to survive reload, got %+v", member.Headers)
	}
	if len(member.Chunks) != 1 || !member.Chunks[0].Completed {
		t.Fatalf("expected member chunks to survive reload, got %+v", member.Chunks)
	}
}

func TestTaskEventViewStripsBundleMembers(t *testing.T) {
	original := &task.Task{
		ID:     "bundle-2",
		Status: task.StatusDownloading,
		Bundle: &task.ZipBundle{
			ZipID:      "pack-2",
			Name:       "pack.zip",
			FilesTotal: 2,
			FilesDone:  1,
			KnownBytes: 10,
			Members: []task.ZipMember{{
				URL:         "https://example.com/a.txt",
				EntryPath:   "a.txt",
				StagingName: "m00001.part",
				Headers:     map[string]string{"Cookie": "session=1"},
			}},
		},
	}

	view := original.EventView()
	if view.Bundle == nil || view.Bundle.ZipID != "pack-2" {
		t.Fatalf("expected bundle summary in event view, got %+v", view.Bundle)
	}
	if len(view.Bundle.Members) != 0 {
		t.Fatalf("expected event view to strip members, got %d", len(view.Bundle.Members))
	}
	if len(original.Bundle.Members) != 1 {
		t.Fatalf("event view must not mutate the original")
	}

	cloned := original.Clone()
	cloned.Bundle.Members[0].Headers["Cookie"] = "changed"
	if original.Bundle.Members[0].Headers["Cookie"] != "session=1" {
		t.Fatalf("clone must deep-copy member headers")
	}
}
