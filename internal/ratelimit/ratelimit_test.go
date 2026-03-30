package ratelimit

import (
	"fmt"
	"testing"
	"time"
)

func TestAllow_Basic(t *testing.T) {
	l := New(3, time.Second, 10)

	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4:1234") {
			t.Errorf("expected allow on attempt %d", i)
		}
	}

	if l.Allow("1.2.3.4:1234") {
		t.Error("expected deny after rate limit")
	}
}

func TestAllow_DifferentIPs(t *testing.T) {
	l := New(1, time.Second, 10)

	if !l.Allow("1.1.1.1:1234") {
		t.Error("expected allow for IP 1")
	}
	if !l.Allow("2.2.2.2:1234") {
		t.Error("expected allow for IP 2")
	}
}

func TestAllow_WindowReset(t *testing.T) {
	l := New(1, 50*time.Millisecond, 10)

	if !l.Allow("1.2.3.4:1234") {
		t.Error("expected allow")
	}
	if l.Allow("1.2.3.4:1234") {
		t.Error("expected deny")
	}

	time.Sleep(60 * time.Millisecond)

	if !l.Allow("1.2.3.4:1234") {
		t.Error("expected allow after window reset")
	}
}

func TestConnOpen_MaxConns(t *testing.T) {
	l := New(100, time.Second, 2)

	if !l.ConnOpen("1.2.3.4:1234") {
		t.Error("expected conn 1 allowed")
	}
	if !l.ConnOpen("1.2.3.4:1234") {
		t.Error("expected conn 2 allowed")
	}
	if l.ConnOpen("1.2.3.4:1234") {
		t.Error("expected conn 3 denied")
	}
}

func TestConnClose_ReleasesSlot(t *testing.T) {
	l := New(100, time.Second, 1)

	l.ConnOpen("1.2.3.4:1234")
	if l.ConnOpen("1.2.3.4:1234") {
		t.Error("expected deny at max")
	}

	l.ConnClose("1.2.3.4:1234")

	if !l.ConnOpen("1.2.3.4:1234") {
		t.Error("expected allow after close")
	}
}

func TestAllow_Concurrent(t *testing.T) {
	l := New(1000, time.Second, 100)

	done := make(chan struct{}, 100)
	for i := 0; i < 100; i++ {
		i := i
		go func() {
			l.Allow(fmt.Sprintf("1.2.3.%d:1234", i%10))
			done <- struct{}{}
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestAllow_InvalidAddr(t *testing.T) {
	l := New(2, time.Second, 10)

	if !l.Allow("invalidaddr") {
		t.Error("expected allow for invalid addr")
	}
	if !l.Allow("invalidaddr") {
		t.Error("expected allow on second")
	}
	if l.Allow("invalidaddr") {
		t.Error("expected deny after rate limit")
	}
}
