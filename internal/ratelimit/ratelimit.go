package ratelimit

import (
	"net"
	"sync"
	"time"
)

type IPLimiter struct {
	mu       sync.Mutex
	clients  map[string]*bucket
	rate     int
	window   time.Duration
	maxConns int
}

type bucket struct {
	count   int
	resetAt time.Time
	conns   int
}

func New(rate int, window time.Duration, maxConns int) *IPLimiter {
	l := &IPLimiter{
		clients:  make(map[string]*bucket),
		rate:     rate,
		window:   window,
		maxConns: maxConns,
	}
	go l.cleanup()
	return l
}

func (l *IPLimiter) Allow(addr string) bool {
	ip := extractIP(addr)

	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.getOrCreate(ip)

	if time.Now().After(b.resetAt) {
		b.count = 0
		b.resetAt = time.Now().Add(l.window)
	}

	if b.count >= l.rate {
		return false
	}

	b.count++
	return true
}

func (l *IPLimiter) ConnOpen(addr string) bool {
	ip := extractIP(addr)

	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.getOrCreate(ip)

	if b.conns >= l.maxConns {
		return false
	}

	b.conns++
	return true
}

func (l *IPLimiter) ConnClose(addr string) {
	ip := extractIP(addr)

	l.mu.Lock()
	defer l.mu.Unlock()

	if b, ok := l.clients[ip]; ok && b.conns > 0 {
		b.conns--
	}
}

func (l *IPLimiter) getOrCreate(ip string) *bucket {
	b, ok := l.clients[ip]
	if !ok {
		b = &bucket{resetAt: time.Now().Add(l.window)}
		l.clients[ip] = b
	}
	return b
}

func (l *IPLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for ip, b := range l.clients {
			if now.After(b.resetAt) && b.conns == 0 {
				delete(l.clients, ip)
			}
		}
		l.mu.Unlock()
	}
}

func extractIP(addr string) string {
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return ip
}
