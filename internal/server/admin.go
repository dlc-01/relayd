package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (s *Server) ListenAdmin() {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/metrics", promhttp.Handler())

	if s.auth != nil {
		mux.HandleFunc("/token/issue", s.handleTokenIssue)
		mux.HandleFunc("/token/list", s.handleTokenList)
		mux.HandleFunc("/token/revoke", s.handleTokenRevoke)
	}

	s.log.Infow("admin listening", "addr", s.cfg.AdminAddr)
	if err := http.ListenAndServe(s.cfg.AdminAddr, s.routeAdmin(mux)); err != nil {
		s.log.Errorw("admin server failed", "err", err)
	}
}

func (s *Server) routeAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		s.adminAuth(next).ServeHTTP(w, r)
	})
}

func (s *Server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		entry, err := s.auth.Validate(token)
		if err != nil || !entry.IsMaster() {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleTokenIssue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TTL   string `json:"ttl"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ttl, err := time.ParseDuration(req.TTL)
	if err != nil {
		http.Error(w, "invalid ttl", http.StatusBadRequest)
		return
	}
	entry, err := s.auth.Issue(req.Label, ttl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token":      entry.Token,
		"label":      entry.Label,
		"expires_at": entry.ExpiresAt.Format(time.RFC3339),
	})
}

func (s *Server) handleTokenList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries := s.auth.List()
	type item struct {
		Token     string `json:"token"`
		Label     string `json:"label"`
		ExpiresAt string `json:"expires_at"`
	}
	result := make([]item, 0, len(entries))
	for _, e := range entries {
		result = append(result, item{
			Token:     e.Token,
			Label:     e.Label,
			ExpiresAt: e.ExpiresAt.Format(time.RFC3339),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.auth.Revoke(req.Token); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
