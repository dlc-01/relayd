package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type TokenKind int

const (
	KindMaster TokenKind = iota
	KindTemp
)

type Entry struct {
	Token     string
	Label     string
	Kind      TokenKind
	ExpiresAt time.Time
	TTL       time.Duration
}

func (e Entry) IsMaster() bool { return e.Kind == KindMaster }
func (e Entry) IsExpired() bool {
	return e.Kind == KindTemp && time.Now().After(e.ExpiresAt)
}

type Manager struct {
	mu          sync.RWMutex
	masterToken string
	tokens      map[string]*Entry
	defaultTTL  time.Duration
}

func NewManager(masterToken string, defaultTTL time.Duration) (*Manager, error) {
	if masterToken == "" {
		return nil, fmt.Errorf("master token cannot be empty")
	}
	return &Manager{
		masterToken: masterToken,
		tokens:      make(map[string]*Entry),
		defaultTTL:  defaultTTL,
	}, nil
}

func (m *Manager) IsMaster(token string) bool {
	return token == m.masterToken
}

func (m *Manager) Validate(token string) (*Entry, error) {
	if token == "" {
		return nil, fmt.Errorf("token required")
	}
	if token == m.masterToken {
		return &Entry{Token: token, Label: "master", Kind: KindMaster}, nil
	}

	m.mu.RLock()
	entry, ok := m.tokens[token]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("invalid token")
	}
	if entry.IsExpired() {
		m.mu.Lock()
		delete(m.tokens, token)
		m.mu.Unlock()
		return nil, fmt.Errorf("token expired")
	}

	return entry, nil
}

func (m *Manager) IssueForMaster(label string) (*Entry, error) {
	return m.Issue(label, m.defaultTTL)
}

func (m *Manager) Issue(label string, ttl time.Duration) (*Entry, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(raw)

	entry := &Entry{
		Token:     token,
		Label:     label,
		Kind:      KindTemp,
		ExpiresAt: time.Now().Add(ttl),
		TTL:       ttl,
	}

	m.mu.Lock()
	m.tokens[token] = entry
	m.mu.Unlock()

	return entry, nil
}

func (m *Manager) Revoke(token string) error {
	if token == m.masterToken {
		return fmt.Errorf("cannot revoke master token")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.tokens[token]; !ok {
		return fmt.Errorf("token not found")
	}
	delete(m.tokens, token)
	return nil
}

func (m *Manager) List() []*Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]*Entry, 0, len(m.tokens))
	for _, e := range m.tokens {
		if !e.IsExpired() {
			entries = append(entries, e)
		}
	}
	return entries
}

func (m *Manager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for token, entry := range m.tokens {
		if entry.IsExpired() {
			delete(m.tokens, token)
		}
	}
}
