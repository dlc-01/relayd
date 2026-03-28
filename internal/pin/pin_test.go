package pin

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	pinFile := filepath.Join(dir, "server.pin")
	fp := "abc123def456"

	if err := Save(pinFile, fp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(pinFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != fp {
		t.Errorf("got %q, want %q", got, fp)
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	pinFile := filepath.Join(dir, "server.pin")

	if Exists(pinFile) {
		t.Error("expected false for missing file")
	}

	Save(pinFile, "test")

	if !Exists(pinFile) {
		t.Error("expected true after save")
	}
}

func TestLoad_Missing(t *testing.T) {
	_, err := Load("/nonexistent/path/server.pin")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSave_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	pinFile := filepath.Join(dir, "subdir", "server.pin")

	if err := Save(pinFile, "fingerprint"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !Exists(pinFile) {
		t.Error("expected file to exist after save")
	}
}
