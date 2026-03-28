package session

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	s := &Session{
		TempToken: "abc123",
		ExpiresAt: time.Now().Add(time.Hour),
		Label:     "vasya",
	}

	if err := Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.TempToken != s.TempToken {
		t.Errorf("token: got %s, want %s", loaded.TempToken, s.TempToken)
	}
	if loaded.Label != s.Label {
		t.Errorf("label: got %s, want %s", loaded.Label, s.Label)
	}
}

func TestIsExpired(t *testing.T) {
	s1 := &Session{ExpiresAt: time.Now().Add(time.Hour)}
	if s1.IsExpired() {
		t.Error("expected not expired")
	}

	s2 := &Session{ExpiresAt: time.Now().Add(-time.Hour)}
	if !s2.IsExpired() {
		t.Error("expected expired")
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	if Exists(path) {
		t.Error("expected false for missing file")
	}

	Save(path, &Session{TempToken: "x", ExpiresAt: time.Now().Add(time.Hour)})

	if !Exists(path) {
		t.Error("expected true after save")
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	Save(path, &Session{TempToken: "x", ExpiresAt: time.Now().Add(time.Hour)})
	if err := Delete(path); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if Exists(path) {
		t.Error("expected file to be deleted")
	}
}
