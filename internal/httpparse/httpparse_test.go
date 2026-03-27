package httpparse

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func makeHTTPRequest(host, path string) []byte {
	return []byte(fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: test\r\nAccept: */*\r\n\r\n",
		path, host,
	))
}

func pipeWith(t *testing.T, data []byte) (net.Conn, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	go func() { client.Write(data) }()
	return server, client
}

func TestPeekHost_Simple(t *testing.T) {
	server, client := pipeWith(t, makeHTTPRequest("app.example.com", "/"))
	defer server.Close()
	defer client.Close()

	result, err := PeekHost(server)
	if err != nil {
		t.Fatalf("PeekHost: %v", err)
	}
	if result.Host != "app.example.com" {
		t.Errorf("host: got %q, want %q", result.Host, "app.example.com")
	}
	if len(result.Peeked) == 0 {
		t.Error("peeked should not be empty")
	}
}

func TestPeekHost_WithPort(t *testing.T) {
	server, client := pipeWith(t, makeHTTPRequest("app.example.com:8080", "/"))
	defer server.Close()
	defer client.Close()

	result, err := PeekHost(server)
	if err != nil {
		t.Fatalf("PeekHost: %v", err)
	}
	if result.Host != "app.example.com" {
		t.Errorf("host: got %q, want %q", result.Host, "app.example.com")
	}
}

func TestPeekHost_PreservesBytes(t *testing.T) {
	req := makeHTTPRequest("app.example.com", "/api/test")
	server, client := pipeWith(t, req)
	defer server.Close()
	defer client.Close()

	result, err := PeekHost(server)
	if err != nil {
		t.Fatalf("PeekHost: %v", err)
	}
	if string(result.Peeked) != string(req) {
		t.Error("peeked bytes do not match original request")
	}
}

func TestPeekHost_NoHostHeader(t *testing.T) {
	req := []byte("GET / HTTP/1.0\r\n\r\n")
	server, client := pipeWith(t, req)
	defer server.Close()
	defer client.Close()

	_, err := PeekHost(server)
	if err == nil {
		t.Error("expected error when Host header missing")
	}
}

func TestPeekHost_Timeout(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		_, err := PeekHost(server)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected timeout error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("test timed out")
	}
}

func TestExtractHost_CaseInsensitive(t *testing.T) {
	req := []byte("GET / HTTP/1.1\r\nHOST: upper.example.com\r\n\r\n")
	host, err := extractHost(req)
	if err != nil {
		t.Fatalf("extractHost: %v", err)
	}
	if host != "upper.example.com" {
		t.Errorf("got %q, want %q", host, "upper.example.com")
	}
}
