package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Session struct {
	TempToken string    `json:"temp_token"`
	ExpiresAt time.Time `json:"expires_at"`
	Label     string    `json:"label"`
}

func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func Save(path string, s *Session) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func Load(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &s, nil
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func Delete(path string) error {
	return os.Remove(path)
}
