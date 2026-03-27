package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
		ControlAddr:   freeAddr(t),
		DataAddr:      freeAddr(t),
		HTTPAddr:      freeAddr(t),
		MinPublicPort: 1024,
		MaxPublicPort: 65535,
		Dev:           true,
	}
	s := New(cfg)
	go s.listenControl()
	go s.listenData()
	go s.listenHTTP()
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

func startHTTPService(t *testing.T, response string) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, response)
	}))
	t.Cleanup(ts.Close)
	return ts.Listener.Addr().String()
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
		t.Fatal(err)
	}
	defer ctrl.Close()

	proto.Write(ctrl, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port}},
	})

	msg, err := proto.Read(ctrl)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != proto.TypeOK {
		t.Errorf("expected ok, got %s reason=%s", msg.Type, msg.Reason)
	}
}

func TestServer_Register_NoTunnels(t *testing.T) {
	s := newTestServer(t)

	ctrl, _ := net.Dial("tcp", s.cfg.ControlAddr)
	defer ctrl.Close()

	proto.Write(ctrl, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{},
	})

	msg, _ := proto.Read(ctrl)
	if msg.Type != proto.TypeError {
		t.Errorf("expected error, got %s", msg.Type)
	}
}

func TestServer_Register_PortConflict(t *testing.T) {
	s := newTestServer(t)
	port := freePort(t)

	ctrl1, _ := net.Dial("tcp", s.cfg.ControlAddr)
	defer ctrl1.Close()
	proto.Write(ctrl1, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port}},
	})
	proto.Read(ctrl1)

	ctrl2, _ := net.Dial("tcp", s.cfg.ControlAddr)
	defer ctrl2.Close()
	proto.Write(ctrl2, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web2", PublicPort: port}},
	})

	msg, _ := proto.Read(ctrl2)
	if msg.Type != proto.TypeError {
		t.Errorf("expected error, got %s", msg.Type)
	}
	if !strings.Contains(msg.Reason, "already in use") {
		t.Errorf("unexpected reason: %s", msg.Reason)
	}
}

func TestServer_Register_HostConflict(t *testing.T) {
	s := newTestServer(t)

	ctrl1, _ := net.Dial("tcp", s.cfg.ControlAddr)
	defer ctrl1.Close()
	proto.Write(ctrl1, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "app", Host: "app.giveoffer.solutions"}},
	})
	proto.Read(ctrl1)

	ctrl2, _ := net.Dial("tcp", s.cfg.ControlAddr)
	defer ctrl2.Close()
	proto.Write(ctrl2, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "app2", Host: "app.giveoffer.solutions"}},
	})

	msg, _ := proto.Read(ctrl2)
	if msg.Type != proto.TypeError {
		t.Errorf("expected error, got %s", msg.Type)
	}
	if !strings.Contains(msg.Reason, "already in use") {
		t.Errorf("unexpected reason: %s", msg.Reason)
	}
}

func TestServer_EndToEnd_TCP(t *testing.T) {
	s := newTestServer(t)
	echoAddr := startEchoService(t)
	port := freePort(t)

	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "echo", PublicPort: port}},
		map[string]string{"echo": echoAddr},
	)
	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	payload := "hello tcp"
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

func TestServer_EndToEnd_HTTP(t *testing.T) {
	s := newTestServer(t)
	httpAddr := startHTTPService(t, "hello from tunnel")

	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "app", Host: "app.giveoffer.solutions"}},
		map[string]string{"app": httpAddr},
	)
	time.Sleep(100 * time.Millisecond)

	req, err := http.NewRequest("GET", "http://"+s.cfg.HTTPAddr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "app.giveoffer.solutions"

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "hello from tunnel") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestServer_HTTP_NoTunnel_Returns502(t *testing.T) {
	s := newTestServer(t)
	time.Sleep(50 * time.Millisecond)

	req, err := http.NewRequest("GET", "http://"+s.cfg.HTTPAddr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "unknown.giveoffer.solutions"

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 502 {
		t.Errorf("expected 502, got %d", resp.StatusCode)
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
