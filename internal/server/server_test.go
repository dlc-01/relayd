package server

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/proto"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func startTestServer(t *testing.T) config.ServerConfig {
	t.Helper()
	cfg := config.ServerConfig{
		ControlAddr: freeAddr(t),
		DataAddr:    freeAddr(t),
		PublicAddr:  freeAddr(t),
	}
	s := New(cfg)
	go s.listenControl()
	go s.listenData()
	time.Sleep(50 * time.Millisecond)
	return cfg
}

func startEchoService(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go io.Copy(conn, conn)
		}
	}()
	return ln.Addr().String()
}

func startTestClient(t *testing.T, cfg config.ServerConfig, localAddr string) {
	t.Helper()
	go func() {
		ctrl, err := net.Dial("tcp", cfg.ControlAddr)
		if err != nil {
			return
		}

		proto.Write(ctrl, proto.Message{
			Type:     proto.TypeRegister,
			TunnelID: "test",
		})

		msg, err := proto.Read(ctrl)
		if err != nil || msg.Type != proto.TypeOK {
			return
		}

		for {
			msg, err := proto.Read(ctrl)
			if err != nil || msg.Type != proto.TypeConnect {
				return
			}
			go func(connID string) {
				localConn, err := net.Dial("tcp", localAddr)
				if err != nil {
					return
				}
				dataConn, err := net.Dial("tcp", cfg.DataAddr)
				if err != nil {
					localConn.Close()
					return
				}
				proto.Write(dataConn, proto.Message{
					Type:   proto.TypeData,
					ConnID: connID,
				})
				bridge(localConn, dataConn)
			}(msg.ConnID)
		}
	}()
}

func TestServerRegister(t *testing.T) {
	cfg := startTestServer(t)

	ctrl, err := net.Dial("tcp", cfg.ControlAddr)
	if err != nil {
		t.Fatalf("dial control: %v", err)
	}
	defer ctrl.Close()

	if err := proto.Write(ctrl, proto.Message{
		Type:     proto.TypeRegister,
		TunnelID: "web",
	}); err != nil {
		t.Fatalf("write register: %v", err)
	}

	msg, err := proto.Read(ctrl)
	if err != nil {
		t.Fatalf("read ok: %v", err)
	}
	if msg.Type != proto.TypeOK {
		t.Errorf("expected ok, got %s", msg.Type)
	}
}

func TestServerRejectsInvalidHandshake(t *testing.T) {
	cfg := startTestServer(t)

	ctrl, err := net.Dial("tcp", cfg.ControlAddr)
	if err != nil {
		t.Fatalf("dial control: %v", err)
	}
	defer ctrl.Close()

	// шлём не register
	proto.Write(ctrl, proto.Message{Type: proto.TypeData})

	// сервер должен закрыть соединение
	ctrl.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	_, err = ctrl.Read(buf)
	if err == nil {
		t.Error("expected connection to be closed by server")
	}
}

func TestEndToEnd_Echo(t *testing.T) {
	echoAddr := startEchoService(t)
	cfg := startTestServer(t)
	startTestClient(t, cfg, echoAddr)

	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", cfg.PublicAddr, time.Second)
	if err != nil {
		t.Fatalf("dial public: %v", err)
	}
	defer conn.Close()

	payload := "hello tunnel"
	conn.Write([]byte(payload))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len(payload))
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if !strings.Contains(string(buf), payload) {
		t.Errorf("got: %q, want: %q", buf, payload)
	}
}

func TestEndToEnd_MultipleConnections(t *testing.T) {
	echoAddr := startEchoService(t)
	cfg := startTestServer(t)
	startTestClient(t, cfg, echoAddr)

	time.Sleep(100 * time.Millisecond)

	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", cfg.PublicAddr, time.Second)
		if err != nil {
			t.Fatalf("conn %d: dial: %v", i, err)
		}
		defer conn.Close()

		payload := "ping"
		conn.Write([]byte(payload))

		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, len(payload))
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			t.Fatalf("conn %d: read: %v", i, err)
		}
		if string(buf) != payload {
			t.Errorf("conn %d: got %q, want %q", i, buf, payload)
		}
	}
}
