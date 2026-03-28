package client

import (
	"crypto/tls"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/pin"
	"github.com/dlc-01/relayd/internal/proto"
	"github.com/dlc-01/relayd/internal/tlscerts"
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

func startTLSFakeServer(t *testing.T, tunnelID, connID string) (ctrlAddr, dataAddr string, dataConnCh chan net.Conn) {
	t.Helper()
	dataConnCh = make(chan net.Conn, 1)

	cert, err := tlscerts.SelfSigned("relayd-control")
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	dataLn, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
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

	ctrlLn, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
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

func makeConfig(t *testing.T, ctrlAddr, dataAddr string, tunnels []config.TunnelConfig) config.ClientConfig {
	return config.ClientConfig{
		ServerControlAddr: ctrlAddr,
		ServerDataAddr:    dataAddr,
		Tunnels:           tunnels,
		PinFile:           filepath.Join(t.TempDir(), "server.pin"),
		Dev:               true,
	}
}

func newTestClient(cfg config.ClientConfig) *Client {
	c := New(cfg)
	c.heartbeatInterval = 50 * time.Millisecond
	c.heartbeatTimeout = 50 * time.Millisecond
	c.reconnectInitial = 10 * time.Millisecond
	c.reconnectMax = 100 * time.Millisecond
	return c
}

func TestClient_Register_SendsTunnels(t *testing.T) {
	cert, _ := tlscerts.SelfSigned("relayd-control")
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	ctrlLn, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
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

	cfg := makeConfig(t, ctrlLn.Addr().String(), "127.0.0.1:1", []config.TunnelConfig{
		{TunnelID: "web", PublicPort: 10001, LocalAddr: "127.0.0.1:8080"},
		{TunnelID: "app", Host: "app.example.com", Hosts: []string{"alias.example.com"}, LocalAddr: "127.0.0.1:9000"},
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
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestClient_Reconnect(t *testing.T) {
	cert, _ := tlscerts.SelfSigned("relayd-control")
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	connectCount := 0
	connectCh := make(chan struct{}, 10)

	ctrlLn, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrlLn.Close()

	go func() {
		for {
			conn, err := ctrlLn.Accept()
			if err != nil {
				return
			}
			connectCount++
			connectCh <- struct{}{}
			msg, err := proto.Read(conn)
			if err != nil || msg.Type != proto.TypeRegister {
				conn.Close()
				continue
			}
			proto.Write(conn, proto.Message{Type: proto.TypeOK})
			time.Sleep(50 * time.Millisecond)
			conn.Close()
		}
	}()

	cfg := makeConfig(t, ctrlLn.Addr().String(), "127.0.0.1:1", []config.TunnelConfig{
		{TunnelID: "web", PublicPort: 10001, LocalAddr: "127.0.0.1:8080"},
	})

	go newTestClient(cfg).Run()

	for i := 0; i < 2; i++ {
		select {
		case <-connectCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for connection %d", i+1)
		}
	}

	if connectCount < 2 {
		t.Errorf("expected at least 2 connects, got %d", connectCount)
	}
}

func TestClient_PinVerification(t *testing.T) {
	cert, _ := tlscerts.SelfSigned("relayd-control")
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	ctrlLn, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrlLn.Close()

	serverReady := make(chan struct{})
	go func() {
		close(serverReady)
		for {
			conn, err := ctrlLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				msg, err := proto.Read(c)
				if err != nil || msg.Type != proto.TypeRegister {
					return
				}
				proto.Write(c, proto.Message{Type: proto.TypeOK})
				time.Sleep(2 * time.Second)
			}(conn)
		}
	}()
	<-serverReady

	pinFile := filepath.Join(t.TempDir(), "server.pin")
	cfg := config.ClientConfig{
		ServerControlAddr: ctrlLn.Addr().String(),
		ServerDataAddr:    "127.0.0.1:1",
		Tunnels:           []config.TunnelConfig{{TunnelID: "web", PublicPort: 10001, LocalAddr: "127.0.0.1:8080"}},
		PinFile:           pinFile,
		Dev:               true,
	}

	c := New(cfg)
	connectDone := make(chan struct{})
	go func() {
		c.connect()
		close(connectDone)
	}()

	time.Sleep(200 * time.Millisecond)

	if !pin.Exists(pinFile) {
		t.Fatal("pin file not created")
	}

	fp, _ := tlscerts.Fingerprint(cert)
	saved, err := pin.Load(pinFile)
	if err != nil {
		t.Fatalf("load pin: %v", err)
	}
	if saved != fp {
		t.Errorf("saved pin %q != cert fingerprint %q", saved, fp)
	}
}

func loadPinForTest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func TestClient_HandleConnect_TCP(t *testing.T) {
	echoAddr := startEchoService(t)
	ctrlAddr, dataAddr, dataConnCh := startTLSFakeServer(t, "web", "conn-1")

	cfg := makeConfig(t, ctrlAddr, dataAddr, []config.TunnelConfig{
		{TunnelID: "web", PublicPort: 10001, LocalAddr: echoAddr},
	})
	go New(cfg).Run()

	select {
	case dataConn := <-dataConnCh:
		defer dataConn.Close()
		msg, err := proto.Read(dataConn)
		if err != nil {
			t.Fatalf("read: %v", err)
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

func TestClient_EndToEnd_Echo(t *testing.T) {
	echoAddr := startEchoService(t)
	ctrlAddr, dataAddr, dataConnCh := startTLSFakeServer(t, "web", "conn-3")

	cfg := makeConfig(t, ctrlAddr, dataAddr, []config.TunnelConfig{
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

	payload := "hello tls stage"
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
	ctrlAddr, dataAddr, dataConnCh := startTLSFakeServer(t, "unknown", "conn-99")

	cfg := makeConfig(t, ctrlAddr, dataAddr, []config.TunnelConfig{
		{TunnelID: "web", PublicPort: 10001, LocalAddr: "127.0.0.1:8080"},
	})
	go New(cfg).Run()

	select {
	case conn := <-dataConnCh:
		conn.Close()
		t.Error("expected no data connection for unknown tunnel_id")
	case <-time.After(500 * time.Millisecond):
	}
}
