package client

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/proto"
)

func startFakeServer(t *testing.T) (controlAddr, dataAddr string, dataConnCh chan net.Conn) {
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
	controlAddr = ctrlLn.Addr().String()
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
			Type:   proto.TypeConnect,
			ConnID: "test-conn-1",
		})
	}()

	return controlAddr, dataAddr, dataConnCh
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

func TestClient_Register(t *testing.T) {
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
		msg, err := proto.Read(conn)
		if err != nil {
			return
		}
		registerCh <- msg
		proto.Write(conn, proto.Message{Type: proto.TypeOK})
		time.Sleep(500 * time.Millisecond)
	}()

	cfg := config.ClientConfig{
		ServerControlAddr: ctrlLn.Addr().String(),
		ServerDataAddr:    "127.0.0.1:1",
		TunnelID:          "web",
		LocalAddr:         "127.0.0.1:1",
	}

	go New(cfg).Run()

	select {
	case msg := <-registerCh:
		if msg.Type != proto.TypeRegister {
			t.Errorf("expected register, got %s", msg.Type)
		}
		if msg.TunnelID != "web" {
			t.Errorf("expected tunnel_id=web, got %s", msg.TunnelID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for register")
	}
}

func TestClient_HandleConnect(t *testing.T) {
	echoAddr := startEchoService(t)
	ctrlAddr, dataAddr, dataConnCh := startFakeServer(t)

	cfg := config.ClientConfig{
		ServerControlAddr: ctrlAddr,
		ServerDataAddr:    dataAddr,
		TunnelID:          "test",
		LocalAddr:         echoAddr,
	}

	go New(cfg).Run()

	var dataConn net.Conn
	select {
	case dataConn = <-dataConnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for data connection")
	}
	defer dataConn.Close()

	msg, err := proto.Read(dataConn)
	if err != nil {
		t.Fatalf("read data msg: %v", err)
	}
	if msg.Type != proto.TypeData {
		t.Errorf("expected data, got %s", msg.Type)
	}
	if msg.ConnID != "test-conn-1" {
		t.Errorf("expected conn_id=test-conn-1, got %s", msg.ConnID)
	}
}

func TestClient_EndToEnd_Echo(t *testing.T) {
	echoAddr := startEchoService(t)
	ctrlAddr, dataAddr, dataConnCh := startFakeServer(t)

	cfg := config.ClientConfig{
		ServerControlAddr: ctrlAddr,
		ServerDataAddr:    dataAddr,
		TunnelID:          "test",
		LocalAddr:         echoAddr,
	}

	go New(cfg).Run()

	var dataConn net.Conn
	select {
	case dataConn = <-dataConnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for data connection")
	}
	defer dataConn.Close()

	_, err := proto.Read(dataConn)
	if err != nil {
		t.Fatalf("read data handshake: %v", err)
	}

	payload := "hello from client test"
	dataConn.Write([]byte(payload))

	dataConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len(payload))
	_, err = io.ReadFull(dataConn, buf)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != payload {
		t.Errorf("got %q, want %q", buf, payload)
	}
}

func TestClient_LocalServiceUnavailable(t *testing.T) {
	ctrlAddr, dataAddr, dataConnCh := startFakeServer(t)

	cfg := config.ClientConfig{
		ServerControlAddr: ctrlAddr,
		ServerDataAddr:    dataAddr,
		TunnelID:          "test",
		LocalAddr:         "127.0.0.1:19999",
	}

	go New(cfg).Run()

	select {
	case conn := <-dataConnCh:
		conn.Close()
		t.Error("expected no data connection when local service is down")
	case <-time.After(500 * time.Millisecond):
	}
}
