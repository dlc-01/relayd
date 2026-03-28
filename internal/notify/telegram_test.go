package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestTelegram(t *testing.T, handler http.HandlerFunc) (*TelegramNotifier, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	n := NewTelegram("test-token", "123456")
	n.client = ts.Client()
	n.sendFn = func(text string) error {
		return n.sendTo(ts.URL, text)
	}
	return n, ts
}

func captureRequest(t *testing.T) (http.HandlerFunc, <-chan map[string]string) {
	t.Helper()
	ch := make(chan map[string]string, 1)
	handler := func(w http.ResponseWriter, r *http.Request) {
		var received map[string]string
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
		ch <- received
	}
	return handler, ch
}

func TestTelegram_Enabled(t *testing.T) {
	cases := []struct {
		token    string
		chatID   string
		expected bool
	}{
		{"token", "chat", true},
		{"", "chat", false},
		{"token", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		n := NewTelegram(tc.token, tc.chatID)
		if n.Enabled() != tc.expected {
			t.Errorf("token=%q chatID=%q: expected %v", tc.token, tc.chatID, tc.expected)
		}
	}
}

func TestTelegram_Send_OK(t *testing.T) {
	handler, ch := captureRequest(t)
	n, ts := newTestTelegram(t, handler)
	defer ts.Close()

	if err := n.sendFn("hello world"); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case received := <-ch:
		if received["chat_id"] != "123456" {
			t.Errorf("chat_id: got %s", received["chat_id"])
		}
		if received["text"] != "hello world" {
			t.Errorf("text: got %s", received["text"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestTelegram_Send_ServerError(t *testing.T) {
	n, ts := newTestTelegram(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer ts.Close()

	if err := n.sendFn("hello"); err == nil {
		t.Error("expected error for server error")
	}
}

func TestTelegram_Send_NetworkError(t *testing.T) {
	n, ts := newTestTelegram(t, func(w http.ResponseWriter, r *http.Request) {})
	ts.Close()

	if err := n.sendFn("hello"); err == nil {
		t.Error("expected error for network error")
	}
}

func TestTelegram_ClientConnected(t *testing.T) {
	handler, ch := captureRequest(t)
	n, ts := newTestTelegram(t, handler)
	defer ts.Close()

	n.ClientConnected("vasya", "app.giveoffer.solutions", "1.2.3.4:5678")

	select {
	case received := <-ch:
		if !strings.Contains(received["text"], "vasya") {
			t.Errorf("expected vasya in text: %s", received["text"])
		}
		if !strings.Contains(received["text"], "connected") {
			t.Errorf("expected connected in text: %s", received["text"])
		}
		if !strings.Contains(received["text"], "app.giveoffer.solutions") {
			t.Errorf("expected tunnel in text: %s", received["text"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestTelegram_ClientDisconnected(t *testing.T) {
	handler, ch := captureRequest(t)
	n, ts := newTestTelegram(t, handler)
	defer ts.Close()

	n.ClientDisconnected("vasya", "1.2.3.4:5678")

	select {
	case received := <-ch:
		if !strings.Contains(received["text"], "vasya") {
			t.Errorf("expected vasya in text: %s", received["text"])
		}
		if !strings.Contains(received["text"], "disconnected") {
			t.Errorf("expected disconnected in text: %s", received["text"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestTelegram_InvalidToken(t *testing.T) {
	handler, ch := captureRequest(t)
	n, ts := newTestTelegram(t, handler)
	defer ts.Close()

	n.InvalidToken("5.5.5.5:1234")

	select {
	case received := <-ch:
		if !strings.Contains(received["text"], "invalid token") {
			t.Errorf("expected invalid token in text: %s", received["text"])
		}
		if !strings.Contains(received["text"], "5.5.5.5") {
			t.Errorf("expected IP in text: %s", received["text"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestTelegram_ServerStarted(t *testing.T) {
	handler, ch := captureRequest(t)
	n, ts := newTestTelegram(t, handler)
	defer ts.Close()

	n.ServerStarted("0.0.0.0:7000")

	select {
	case received := <-ch:
		if !strings.Contains(received["text"], "started") {
			t.Errorf("expected started in text: %s", received["text"])
		}
		if !strings.Contains(received["text"], "7000") {
			t.Errorf("expected addr in text: %s", received["text"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestTelegram_SendAsync_NoBlock(t *testing.T) {
	slow := make(chan struct{})
	n, ts := newTestTelegram(t, func(w http.ResponseWriter, r *http.Request) {
		<-slow
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})
	defer ts.Close()
	defer close(slow)

	done := make(chan struct{})
	go func() {
		n.sendAsync("test")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("sendAsync should not block")
	}
}
