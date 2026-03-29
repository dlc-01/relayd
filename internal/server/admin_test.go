package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dlc-01/relayd/internal/auth"
	"github.com/dlc-01/relayd/internal/config"
)

func newTestServerWithAuth(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.ServerConfig{
		ControlAddr:     freeAddrTB(t),
		DataAddr:        freeAddrTB(t),
		HTTPAddr:        freeAddrTB(t),
		TLSAddr:         freeAddrTB(t),
		TLSDomain:       "example.com",
		ControlCertFile: filepath.Join(dir, "control.crt"),
		ControlKeyFile:  filepath.Join(dir, "control.key"),
		MinPublicPort:   1024,
		MaxPublicPort:   65535,
		PendingTimeout:  30 * time.Second,
		MasterToken:     "master-secret",
		SessionTTL:      time.Hour,
		AdminAddr:       "127.0.0.1:0",
		Dev:             true,
	}
	s := New(cfg)
	return s
}

func TestAdminAuth_NoToken(t *testing.T) {
	s := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/token/list", nil)
	w := httptest.NewRecorder()

	s.adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminAuth_WrongToken(t *testing.T) {
	s := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/token/list", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	s.adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminAuth_TempToken(t *testing.T) {
	s := newTestServerWithAuth(t)
	entry, _ := s.auth.Issue("vasya", time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/token/list", nil)
	req.Header.Set("Authorization", "Bearer "+entry.Token)
	w := httptest.NewRecorder()

	s.adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for temp token, got %d", w.Code)
	}
}

func TestAdminAuth_MasterToken(t *testing.T) {
	s := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/token/list", nil)
	req.Header.Set("Authorization", "Bearer master-secret")
	w := httptest.NewRecorder()

	called := false
	s.adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, req)

	if !called {
		t.Error("expected handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleTokenIssue_OK(t *testing.T) {
	s := newTestServerWithAuth(t)

	body := `{"ttl":"2h","label":"vasya"}`
	req := httptest.NewRequest(http.MethodPost, "/token/issue", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer master-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.adminAuth(http.HandlerFunc(s.handleTokenIssue)).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["token"] == "" {
		t.Error("expected non-empty token")
	}
	if resp["label"] != "vasya" {
		t.Errorf("label: got %s, want vasya", resp["label"])
	}
	if resp["expires_at"] == "" {
		t.Error("expected non-empty expires_at")
	}
}

func TestHandleTokenIssue_InvalidTTL(t *testing.T) {
	s := newTestServerWithAuth(t)

	body := `{"ttl":"notaduration","label":"vasya"}`
	req := httptest.NewRequest(http.MethodPost, "/token/issue", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	s.handleTokenIssue(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTokenIssue_WrongMethod(t *testing.T) {
	s := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/token/issue", nil)
	w := httptest.NewRecorder()

	s.handleTokenIssue(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleTokenIssue_BadJSON(t *testing.T) {
	s := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/token/issue", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()

	s.handleTokenIssue(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTokenList_Empty(t *testing.T) {
	s := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/token/list", nil)
	w := httptest.NewRecorder()

	s.handleTokenList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d items", len(result))
	}
}

func TestHandleTokenList_WithTokens(t *testing.T) {
	s := newTestServerWithAuth(t)
	s.auth.Issue("vasya", time.Hour)
	s.auth.Issue("petya", time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/token/list", nil)
	w := httptest.NewRecorder()

	s.handleTokenList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result []map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestHandleTokenList_WrongMethod(t *testing.T) {
	s := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/token/list", nil)
	w := httptest.NewRecorder()

	s.handleTokenList(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleTokenRevoke_OK(t *testing.T) {
	s := newTestServerWithAuth(t)
	entry, _ := s.auth.Issue("vasya", time.Hour)

	body, _ := json.Marshal(map[string]string{"token": entry.Token})
	req := httptest.NewRequest(http.MethodDelete, "/token/revoke", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleTokenRevoke(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}

	_, err := s.auth.Validate(entry.Token)
	if err == nil {
		t.Error("expected error after revoke")
	}
}

func TestHandleTokenRevoke_NotFound(t *testing.T) {
	s := newTestServerWithAuth(t)

	body, _ := json.Marshal(map[string]string{"token": "nonexistent"})
	req := httptest.NewRequest(http.MethodDelete, "/token/revoke", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleTokenRevoke(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTokenRevoke_WrongMethod(t *testing.T) {
	s := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/token/revoke", nil)
	w := httptest.NewRecorder()

	s.handleTokenRevoke(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleTokenRevoke_BadJSON(t *testing.T) {
	s := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodDelete, "/token/revoke", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()

	s.handleTokenRevoke(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestListenAdmin_DisabledWithoutAuth(t *testing.T) {
	dir := t.TempDir()
	cfg := config.ServerConfig{
		ControlAddr:     freeAddrTB(t),
		DataAddr:        freeAddrTB(t),
		HTTPAddr:        freeAddrTB(t),
		TLSAddr:         freeAddrTB(t),
		TLSDomain:       "example.com",
		ControlCertFile: filepath.Join(dir, "control.crt"),
		ControlKeyFile:  filepath.Join(dir, "control.key"),
		MinPublicPort:   1024,
		MaxPublicPort:   65535,
		PendingTimeout:  30 * time.Second,
		Dev:             true,
	}
	s := New(cfg)

	if s.auth != nil {
		t.Error("expected nil auth when no master token")
	}
}

func TestAdminAuth_BearerPrefix(t *testing.T) {
	s := newTestServerWithAuth(t)

	for _, tc := range []struct {
		header   string
		expected int
	}{
		{"Bearer master-secret", http.StatusOK},
		{"master-secret", http.StatusUnauthorized},
		{"Bearer wrong", http.StatusUnauthorized},
		{"", http.StatusUnauthorized},
	} {
		tc := tc
		t.Run(tc.header, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()

			s.adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(w, req)

			if w.Code != tc.expected {
				t.Errorf("header=%q: expected %d, got %d", tc.header, tc.expected, w.Code)
			}
		})
	}
}

func TestAdminIntegration_IssueAndValidate(t *testing.T) {
	s := newTestServerWithAuth(t)

	body := `{"ttl":"1h","label":"integration-test"}`
	req := httptest.NewRequest(http.MethodPost, "/token/issue", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleTokenIssue(w, req)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	token := resp["token"]

	entry, err := s.auth.Validate(token)
	if err != nil {
		t.Fatalf("validate issued token: %v", err)
	}
	if entry.Label != "integration-test" {
		t.Errorf("label: got %s", entry.Label)
	}

	body2, _ := json.Marshal(map[string]string{"token": token})
	req2 := httptest.NewRequest(http.MethodDelete, "/token/revoke", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	s.handleTokenRevoke(w2, req2)

	if w2.Code != http.StatusNoContent {
		t.Errorf("revoke: expected 204, got %d", w2.Code)
	}

	_, err = s.auth.Validate(token)
	if err == nil {
		t.Error("expected error after revoke")
	}
}

func TestAdmin_NewManagerWithAuth(t *testing.T) {
	s := newTestServerWithAuth(t)

	if s.auth == nil {
		t.Fatal("expected non-nil auth")
	}

	entry, err := s.auth.Validate("master-secret")
	if err != nil {
		t.Fatalf("validate master: %v", err)
	}
	if !entry.IsMaster() {
		t.Error("expected master entry")
	}
}

func newTestAuthManager(t *testing.T) *auth.Manager {
	t.Helper()
	m, err := auth.NewManager("master-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
