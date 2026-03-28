package notify

import (
	"testing"
	"time"
)

func TestMultiNotifier_Enabled_AllDisabled(t *testing.T) {
	m := NewMulti(&NoopNotifier{}, &NoopNotifier{})
	if m.Enabled() {
		t.Error("expected disabled when all notifiers disabled")
	}
}

func TestMultiNotifier_Enabled_OneEnabled(t *testing.T) {
	tg := NewTelegram("token", "chat")
	m := NewMulti(&NoopNotifier{}, tg)
	if !m.Enabled() {
		t.Error("expected enabled when at least one notifier enabled")
	}
}

func TestMultiNotifier_Empty(t *testing.T) {
	m := NewMulti()
	if m.Enabled() {
		t.Error("expected disabled for empty multi")
	}
	m.ClientConnected("label", "tunnels", "remote")
	m.ClientDisconnected("label", "remote")
	m.InvalidToken("remote")
	m.ServerStarted("addr")
}

func TestNoopNotifier(t *testing.T) {
	n := &NoopNotifier{}
	if n.Enabled() {
		t.Error("expected disabled")
	}
	n.ClientConnected("a", "b", "c")
	n.ClientDisconnected("a", "b")
	n.InvalidToken("a")
	n.ServerStarted("a")
}

func TestMultiNotifier_CallsAll(t *testing.T) {
	ch := make(chan struct{}, 2)

	tg1 := NewTelegram("token1", "chat1")
	tg2 := NewTelegram("token2", "chat2")

	tg1.sendFn = func(text string) error { ch <- struct{}{}; return nil }
	tg2.sendFn = func(text string) error { ch <- struct{}{}; return nil }

	m := NewMulti(tg1, tg2)
	m.ServerStarted("addr")

	for i := 0; i < 2; i++ {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for call %d", i+1)
		}
	}
}
