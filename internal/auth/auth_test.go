package auth

import (
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager("master-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNewManager_EmptyMaster(t *testing.T) {
	_, err := NewManager("", time.Hour)
	if err == nil {
		t.Error("expected error for empty master token")
	}
}

func TestValidate_MasterToken(t *testing.T) {
	m := newTestManager(t)

	entry, err := m.Validate("master-secret")
	if err != nil {
		t.Fatalf("validate master: %v", err)
	}
	if !entry.IsMaster() {
		t.Error("expected master kind")
	}
}

func TestValidate_EmptyToken(t *testing.T) {
	m := newTestManager(t)
	_, err := m.Validate("")
	if err == nil {
		t.Error("expected error for empty token")
	}
}

func TestValidate_InvalidToken(t *testing.T) {
	m := newTestManager(t)
	_, err := m.Validate("wrong-token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestIsMaster(t *testing.T) {
	m := newTestManager(t)
	if !m.IsMaster("master-secret") {
		t.Error("expected true for master token")
	}
	if m.IsMaster("other") {
		t.Error("expected false for non-master")
	}
}

func TestIssueForMaster(t *testing.T) {
	m := newTestManager(t)

	entry, err := m.IssueForMaster("vasya")
	if err != nil {
		t.Fatalf("IssueForMaster: %v", err)
	}
	if entry.Token == "" {
		t.Error("expected non-empty token")
	}
	if entry.Label != "vasya" {
		t.Errorf("expected label vasya, got %s", entry.Label)
	}
	if entry.IsMaster() {
		t.Error("expected temp kind")
	}
	if entry.TTL != time.Hour {
		t.Errorf("expected ttl 1h, got %v", entry.TTL)
	}
}

func TestIssueAndValidate(t *testing.T) {
	m := newTestManager(t)

	entry, err := m.Issue("vasya", time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	validated, err := m.Validate(entry.Token)
	if err != nil {
		t.Fatalf("validate issued: %v", err)
	}
	if validated.Label != "vasya" {
		t.Errorf("expected label vasya, got %s", validated.Label)
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	m := newTestManager(t)

	entry, _ := m.Issue("temp", 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	_, err := m.Validate(entry.Token)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestRevoke(t *testing.T) {
	m := newTestManager(t)

	entry, _ := m.Issue("vasya", time.Hour)
	if err := m.Revoke(entry.Token); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	_, err := m.Validate(entry.Token)
	if err == nil {
		t.Error("expected error after revoke")
	}
}

func TestRevoke_MasterToken(t *testing.T) {
	m := newTestManager(t)
	err := m.Revoke("master-secret")
	if err == nil {
		t.Error("expected error revoking master token")
	}
}

func TestRevoke_NotFound(t *testing.T) {
	m := newTestManager(t)
	err := m.Revoke("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent token")
	}
}

func TestList(t *testing.T) {
	m := newTestManager(t)

	m.Issue("vasya", time.Hour)
	m.Issue("petya", time.Hour)

	list := m.List()
	if len(list) != 2 {
		t.Errorf("expected 2 entries, got %d", len(list))
	}
}

func TestList_ExcludesExpired(t *testing.T) {
	m := newTestManager(t)

	m.Issue("valid", time.Hour)
	m.Issue("expired", 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	list := m.List()
	if len(list) != 1 {
		t.Errorf("expected 1 entry, got %d", len(list))
	}
	if list[0].Label != "valid" {
		t.Errorf("expected valid, got %s", list[0].Label)
	}
}

func TestCleanup(t *testing.T) {
	m := newTestManager(t)

	m.Issue("temp", 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	m.Cleanup()

	m.mu.RLock()
	count := len(m.tokens)
	m.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected 0 tokens after cleanup, got %d", count)
	}
}

func TestIssue_TokensAreUnique(t *testing.T) {
	m := newTestManager(t)

	e1, _ := m.Issue("a", time.Hour)
	e2, _ := m.Issue("b", time.Hour)

	if e1.Token == e2.Token {
		t.Error("expected unique tokens")
	}
}
