package server

import (
	"encoding/json"
	"net/http"
	"time"
)

var startTime = time.Now()

type healthResponse struct {
	Status   string `json:"status"`
	Sessions int    `json:"sessions"`
	Tunnels  int    `json:"tunnels"`
	Uptime   string `json:"uptime"`
	Version  string `json:"version"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sessions := len(s.sessions)
	tunnels := len(s.tunnels)
	s.mu.Unlock()

	resp := healthResponse{
		Status:   "ok",
		Sessions: sessions,
		Tunnels:  tunnels,
		Uptime:   time.Since(startTime).Round(time.Second).String(),
		Version:  "v0.2.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
