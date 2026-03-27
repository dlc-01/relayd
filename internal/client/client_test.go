package client

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/proto"
)

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

func startFakeServer(t *testing.T, tunnelID, connID string) (ctrlAddr, dataAddr string, dataConnCh chan net.Conn) {
	t.Helper()
	dataConnCh = make(chan net.Conn, 1)

	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dataAddr = dataLn.Addr().String()
	t.Cleanup(func() { dataLn.Close() })

	go func() {
		for {
			conn, err := dataLn.Accept()
			if err != nil {
				return
			}
			dataConnCh <- conn
		}
	}()

	ctrlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctrlAddr = ctrlLn.Addr().String()
	t.Cleanup(func() { ctrlLn.Close() })

	go func() {
		conn, err := ctrlLn.Accept()
		if err != nil {
			return
		}
		msg, err := proto.Read(conn)
		if err != nil || msg.Type != proto.TypeRegister {
			conn.Close()
			return
		}
		proto.Write(conn, proto.Message{Type: proto.TypeOK})
		time.Sleep(50 * time.Millisecond)
		proto.Write(conn, proto.Message{
			Type:     proto.TypeConnect,
			ConnID:   connID,
			TunnelID: tunnelID,
		})
	}()

	return ctrlAddr, dataAddr, dataConnCh
}

func makeConfig(ctrlAddr, dataAddr string, tunnels []config.TunnelConfig) config.ClientConfig {
	return config.ClientConfig{
		ServerControlAddr: ctrlAddr,
		ServerDataAddr:    dataAddr,
		Tunnels:           tunnels,
		Dev:               true,
	}
}

func TestClient_Register_SendsTunnels(t *testing.T) {
	ctrlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ctrlLn.Close()

	registerCh := make(chan proto.Message, 1)
	go func() {
		conn, err := ctrlLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		msg, _ := proto.Read(conn)
		registerCh <- msg
		proto.Write(conn, proto.Message{Type: proto.TypeOK})
		time.Sleep(500 * time.Millisecond)
	}()

	cfg := makeConfig(ctrlLn.Addr().String(), "127.0.0.1:1", []config.TunnelConfig{
		{TunnelID: "web", PublicPort: 10001, LocalAddr: "127.0.0.1:8080"},
		{TunnelID: "app", Host: "app.giveoffer.solutions", LocalAddr: "127.0.0.1:9000"},
	})
	go New(cfg).Run()

	select {
	case msg := <-registerCh:
		if msg.Type != proto.TypeRegister {
			t.Errorf("expected register, got %s", msg.Type)
		}
		if len(msg.Tunnels) != 2 {
			t.Fatalf("expected 2 tunnels, got %d", len(msg.Tunnels))
		}
		if msg.Tunnels[0].TunnelID != "web" || msg.Tunnels[0].PublicPort != 10001 {
			t.Errorf("tunnel[0]: %+v", msg.Tunnels[0])
		}
		if msg.Tunnels[1].TunnelID != "app" || msg.Tunnels[1].Host != "app.giveoffer.solutions" {
			t.Errorf("tunnel[1]: %+v", msg.Tunnels[1])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for register")
	}
}

func TestClient_HandleConnect_TCP(t *testing.T) {
	echoAddr := startEchoService(t)
	ctrlAddr, dataAddr, dataConnCh := startFakeServer(t, "web", "conn-1")

	cfg := makeConfig(ctrlAddr, dataAddr, []config.TunnelConfig{
		{TunnelID: "web", PublicPort: 10001, LocalAddr: echoAddr},
	})
	go New(cfg).Run()

	select {
	case dataConn := <-dataConnCh:
		defer dataConn.Close()
		msg, err := proto.Read(dataConn)
		if err != nil {
			t.Fatalf("read data msg: %v", err)
		}
		if msg.Type != proto.TypeData {
			t.Errorf("expected data, got %s", msg.Type)
		}
		if msg.ConnID != "conn-1" {
			t.Errorf("expected conn-1, got %s", msg.ConnID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestClient_HandleConnect_HTTP(t *testing.T) {
	echoAddr := startEchoService(t)
	ctrlAddr, dataAddr, dataConnCh := startFakeServer(t, "app", "conn-2")

	cfg := makeConfig(ctrlAddr, dataAddr, []config.TunnelConfig{
		{TunnelID: "app", Host: "app.giveoffer.solutions", LocalAddr: echoAddr},
	})
	go New(cfg).Run()

	select {
	case dataConn := <-dataConnCh:
		defer dataConn.Close()
		msg, err := proto.Read(dataConn)
		if err != nil {
			t.Fatalf("read data msg: %v", err)
		}
		if msg.ConnID != "conn-2" {
			t.Errorf("expected conn-2, got %s", msg.ConnID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestClient_EndToEnd_Echo(t *testing.T) {
	echoAddr := startEchoService(t)
	ctrlAddr, dataAddr, dataConnCh := startFakeServer(t, "web", "conn-3")

	cfg := makeConfig(ctrlAddr, dataAddr, []config.TunnelConfig{
		{TunnelID: "web", PublicPort: 10001, LocalAddr: echoAddr},
	})
	go New(cfg).Run()

	var dataConn net.Conn
	select {
	case dataConn = <-dataConnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	defer dataConn.Close()

	_, err := proto.Read(dataConn)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}

	payload := "hello stage3"
	dataConn.Write([]byte(payload))

	dataConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(dataConn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != payload {
		t.Errorf("got %q, want %q", buf, payload)
	}
}

func TestClient_UnknownTunnelID(t *testing.T) {
	ctrlAddr, dataAddr, dataConnCh := startFakeServer(t, "unknown", "conn-99")

	cfg := makeConfig(ctrlAddr, dataAddr, []config.TunnelConfig{
		{TunnelID: "web", PublicPort: 10001, LocalAddr: "127.0.0.1:8080"},
	})
	go New(cfg).Run()

	select {
	case conn := <-dataConnCh:
		conn.Close()
		t.Error("expected no data connection for unknown tunnel_id")
	case <-time.After(500 * time.Millisecond):
		// ок
	}
}
