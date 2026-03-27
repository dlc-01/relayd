package server

import (
	"fmt"
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

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.ServerConfig{
		ControlAddr: freeAddr(t),
		DataAddr:    freeAddr(t),
		Dev:         true,
	}
	s := New(cfg)
	go s.listenControl()
	go s.listenData()
	time.Sleep(50 * time.Millisecond)
	return s
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

func connectTestClient(t *testing.T, s *Server, tunnels []proto.TunnelDef, localAddrs map[string]string) {
	t.Helper()
	go func() {
		ctrl, err := net.Dial("tcp", s.cfg.ControlAddr)
		if err != nil {
			return
		}
		proto.Write(ctrl, proto.Message{
			Type:    proto.TypeRegister,
			Tunnels: tunnels,
		})
		msg, err := proto.Read(ctrl)
		if err != nil || msg.Type != proto.TypeOK {
			ctrl.Close()
			return
		}
		for {
			msg, err := proto.Read(ctrl)
			if err != nil || msg.Type != proto.TypeConnect {
				return
			}
			go func(connID, tunnelID string) {
				localAddr, ok := localAddrs[tunnelID]
				if !ok {
					return
				}
				localConn, err := net.Dial("tcp", localAddr)
				if err != nil {
					return
				}
				dataConn, err := net.Dial("tcp", s.cfg.DataAddr)
				if err != nil {
					localConn.Close()
					return
				}
				proto.Write(dataConn, proto.Message{
					Type:   proto.TypeData,
					ConnID: connID,
				})
				bridge(localConn, dataConn)
			}(msg.ConnID, msg.TunnelID)
		}
	}()
}

func TestServer_Register_OK(t *testing.T) {
	s := newTestServer(t)
	port := freePort(t)

	ctrl, err := net.Dial("tcp", s.cfg.ControlAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ctrl.Close()

	proto.Write(ctrl, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port}},
	})

	msg, err := proto.Read(ctrl)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if msg.Type != proto.TypeOK {
		t.Errorf("expected ok, got %s reason=%s", msg.Type, msg.Reason)
	}
}

func TestServer_Register_NoTunnels(t *testing.T) {
	s := newTestServer(t)

	ctrl, err := net.Dial("tcp", s.cfg.ControlAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	proto.Write(ctrl, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{},
	})

	msg, err := proto.Read(ctrl)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != proto.TypeError {
		t.Errorf("expected error, got %s", msg.Type)
	}
}

func TestServer_Register_PortConflict(t *testing.T) {
	s := newTestServer(t)
	port := freePort(t)

	ctrl1, err := net.Dial("tcp", s.cfg.ControlAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl1.Close()

	proto.Write(ctrl1, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port}},
	})
	proto.Read(ctrl1) // ok

	ctrl2, err := net.Dial("tcp", s.cfg.ControlAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl2.Close()

	proto.Write(ctrl2, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web2", PublicPort: port}},
	})

	msg, err := proto.Read(ctrl2)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != proto.TypeError {
		t.Errorf("expected error, got %s", msg.Type)
	}
	if !strings.Contains(msg.Reason, "already in use") {
		t.Errorf("unexpected reason: %s", msg.Reason)
	}
}

func TestServer_EndToEnd_SingleTunnel(t *testing.T) {
	s := newTestServer(t)
	echoAddr := startEchoService(t)
	port := freePort(t)

	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "web", PublicPort: port}},
		map[string]string{"web": echoAddr},
	)
	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatalf("dial public: %v", err)
	}
	defer conn.Close()

	payload := "hello single tunnel"
	conn.Write([]byte(payload))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != payload {
		t.Errorf("got %q, want %q", buf, payload)
	}
}

func TestServer_EndToEnd_MultipleTunnels(t *testing.T) {
	s := newTestServer(t)
	echo1 := startEchoService(t)
	echo2 := startEchoService(t)
	port1 := freePort(t)
	port2 := freePort(t)

	connectTestClient(t, s,
		[]proto.TunnelDef{
			{TunnelID: "web", PublicPort: port1},
			{TunnelID: "api", PublicPort: port2},
		},
		map[string]string{
			"web": echo1,
			"api": echo2,
		},
	)
	time.Sleep(100 * time.Millisecond)

	for _, tc := range []struct {
		port    int
		payload string
	}{
		{port1, "hello web"},
		{port2, "hello api"},
	} {
		tc := tc
		t.Run(fmt.Sprintf("port_%d", tc.port), func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", tc.port), time.Second)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()

			conn.Write([]byte(tc.payload))

			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, len(tc.payload))
			if _, err := io.ReadFull(conn, buf); err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(buf) != tc.payload {
				t.Errorf("got %q, want %q", buf, tc.payload)
			}
		})
	}
}
