package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListVersions(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "testapp")
	os.MkdirAll(filepath.Join(appDir, "1.0.0"), 0755)
	os.MkdirAll(filepath.Join(appDir, "2.0.0"), 0755)
	os.MkdirAll(filepath.Join(appDir, "1.1.0"), 0755)

	versions, err := listVersions(tmpDir, "testapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 3 {
		t.Errorf("expected 3 versions, got %d", len(versions))
	}

	// Should be sorted
	expected := []string{"1.0.0", "1.1.0", "2.0.0"}
	for i, v := range versions {
		if v != expected[i] {
			t.Errorf("expected versions[%d]=%s, got %s", i, expected[i], v)
		}
	}
}

func TestListVersions_ExcludesCurrent(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "testapp")
	os.MkdirAll(filepath.Join(appDir, "1.0.0"), 0755)
	// Create "current" symlink
	os.Symlink("1.0.0", filepath.Join(appDir, "current"))

	versions, err := listVersions(tmpDir, "testapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "current" should not be in the list
	for _, v := range versions {
		if v == "current" {
			t.Error("current should be excluded from versions")
		}
	}
}

func TestGetCurrentVersion(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "testapp")
	os.MkdirAll(filepath.Join(appDir, "1.0.0"), 0755)
	os.Symlink("1.0.0", filepath.Join(appDir, "current"))

	current, err := getCurrentVersion(tmpDir, "testapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if current != "1.0.0" {
		t.Errorf("expected 1.0.0, got %s", current)
	}
}

func TestGetCurrentVersion_NoSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "testapp")
	os.MkdirAll(appDir, 0755)

	current, err := getCurrentVersion(tmpDir, "testapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if current != "" {
		t.Errorf("expected empty string, got %s", current)
	}
}

func TestSwitchVersion(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "testapp")
	os.MkdirAll(filepath.Join(appDir, "1.0.0"), 0755)
	os.MkdirAll(filepath.Join(appDir, "2.0.0"), 0755)

	// Switch to 1.0.0
	err := switchVersion(tmpDir, "testapp", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	current, _ := getCurrentVersion(tmpDir, "testapp")
	if current != "1.0.0" {
		t.Errorf("expected 1.0.0, got %s", current)
	}

	// Switch to 2.0.0
	err = switchVersion(tmpDir, "testapp", "2.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	current, _ = getCurrentVersion(tmpDir, "testapp")
	if current != "2.0.0" {
		t.Errorf("expected 2.0.0, got %s", current)
	}
}

func TestSwitchVersion_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "testapp")
	os.MkdirAll(appDir, 0755)

	err := switchVersion(tmpDir, "testapp", "1.0.0")
	if err == nil {
		t.Error("expected error for non-existent version")
	}
}

func TestListApps(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "app1"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "app2"), 0755)

	apps, err := listApps(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 2 {
		t.Errorf("expected 2 apps, got %d", len(apps))
	}
}
