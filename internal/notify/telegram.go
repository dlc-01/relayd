package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type TelegramNotifier struct {
	token  string
	chatID string
	client *http.Client
	sendFn func(text string) error
}

func NewTelegram(token, chatID string) *TelegramNotifier {
	n := &TelegramNotifier{
		token:  token,
		chatID: chatID,
		client: &http.Client{Timeout: 5 * time.Second},
	}
	n.sendFn = n.send
	return n
}

func (n *TelegramNotifier) Enabled() bool {
	return n.token != "" && n.chatID != ""
}

func (n *TelegramNotifier) send(text string) error {
	return n.sendTo(
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.token),
		text,
	)
}

func (n *TelegramNotifier) sendTo(url, text string) error {
	body, err := json.Marshal(map[string]string{
		"chat_id": n.chatID,
		"text":    text,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	resp, err := n.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram api: status %d", resp.StatusCode)
	}

	return nil
}

func (n *TelegramNotifier) sendAsync(text string) {
	if !n.Enabled() {
		return
	}
	go func() {
		n.sendFn(text)
	}()
}

func (n *TelegramNotifier) ClientConnected(label, tunnels, remote string) {
	n.sendAsync(fmt.Sprintf("✅ [%s] connected\ntunnels: %s\nfrom: %s", label, tunnels, remote))
}

func (n *TelegramNotifier) ClientDisconnected(label, remote string) {
	n.sendAsync(fmt.Sprintf("🔌 [%s] disconnected\nfrom: %s", label, remote))
}

func (n *TelegramNotifier) InvalidToken(remote string) {
	n.sendAsync(fmt.Sprintf("🚨 invalid token attempt\nfrom: %s", remote))
}

func (n *TelegramNotifier) ServerStarted(addr string) {
	n.sendAsync(fmt.Sprintf("🚀 relayd server started\ncontrol: %s", addr))
}
