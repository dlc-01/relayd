package server

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/proto"
)

var insecureTLS = &tls.Config{InsecureSkipVerify: true}

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
	dir := t.TempDir()
	cfg := config.ServerConfig{
		ControlAddr:     freeAddr(t),
		DataAddr:        freeAddr(t),
		HTTPAddr:        freeAddr(t),
		TLSAddr:         freeAddr(t),
		TLSDomain:       "example.com",
		ControlCertFile: filepath.Join(dir, "control.crt"),
		ControlKeyFile:  filepath.Join(dir, "control.key"),
		MinPublicPort:   1024,
		MaxPublicPort:   65535,
		PendingTimeout:  30 * time.Second,
		Dev:             true,
	}
	s := New(cfg)
	go s.listenControl()
	go s.listenData()
	go s.listenHTTP()
	go s.listenTLS()
	time.Sleep(50 * time.Millisecond)
	return s
}

func dialControl(t *testing.T, s *Server) net.Conn {
	t.Helper()
	conn, err := tls.Dial("tcp", s.cfg.ControlAddr, insecureTLS)
	if err != nil {
		t.Fatalf("dial control: %v", err)
	}
	return conn
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
		ctrl, err := tls.Dial("tcp", s.cfg.ControlAddr, insecureTLS)
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
				dataConn, err := tls.Dial("tcp", s.cfg.DataAddr, insecureTLS)
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

	ctrl := dialControl(t, s)
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

	ctrl := dialControl(t, s)
	defer ctrl.Close()

	proto.Write(ctrl, proto.Message{Type: proto.TypeRegister, Tunnels: []proto.TunnelDef{}})

	msg, _ := proto.Read(ctrl)
	if msg.Type != proto.TypeError {
		t.Errorf("expected error, got %s", msg.Type)
	}
}

func TestServer_Register_PortConflict(t *testing.T) {
	s := newTestServer(t)
	port := freePort(t)

	ctrl1 := dialControl(t, s)
	defer ctrl1.Close()
	proto.Write(ctrl1, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port}},
	})
	proto.Read(ctrl1)

	ctrl2 := dialControl(t, s)
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

	ctrl1 := dialControl(t, s)
	defer ctrl1.Close()
	proto.Write(ctrl1, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "app", Host: "app.example.com"}},
	})
	proto.Read(ctrl1)

	ctrl2 := dialControl(t, s)
	defer ctrl2.Close()
	proto.Write(ctrl2, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "app2", Host: "app.example.com"}},
	})

	msg, _ := proto.Read(ctrl2)
	if msg.Type != proto.TypeError {
		t.Errorf("expected error, got %s", msg.Type)
	}
	if !strings.Contains(msg.Reason, "already in use") {
		t.Errorf("unexpected reason: %s", msg.Reason)
	}
}

func TestServer_MultiClient_IndependentSessions(t *testing.T) {
	s := newTestServer(t)
	echo1 := startEchoService(t)
	echo2 := startEchoService(t)
	port1 := freePort(t)
	port2 := freePort(t)

	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "web", PublicPort: port1}},
		map[string]string{"web": echo1},
	)
	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "api", PublicPort: port2}},
		map[string]string{"api": echo2},
	)
	time.Sleep(100 * time.Millisecond)

	if s.SessionCount() != 2 {
		t.Errorf("expected 2 sessions, got %d", s.SessionCount())
	}

	for _, tc := range []struct {
		port    int
		payload string
	}{
		{port1, "hello from client A"},
		{port2, "hello from client B"},
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

func TestServer_MultiClient_DisconnectCleansOnlyOwnTunnels(t *testing.T) {
	s := newTestServer(t)
	echo2 := startEchoService(t)
	port1 := freePort(t)
	port2 := freePort(t)

	ctrlA := dialControl(t, s)
	proto.Write(ctrlA, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port1}},
	})
	proto.Read(ctrlA)

	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "api", PublicPort: port2}},
		map[string]string{"api": echo2},
	)
	time.Sleep(100 * time.Millisecond)

	if s.SessionCount() != 2 {
		t.Fatalf("expected 2 sessions before disconnect, got %d", s.SessionCount())
	}

	ctrlA.Close()
	time.Sleep(100 * time.Millisecond)

	if s.SessionCount() != 1 {
		t.Errorf("expected 1 session after disconnect, got %d", s.SessionCount())
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port2), time.Second)
	if err != nil {
		t.Fatalf("client B tunnel should still work: %v", err)
	}
	defer conn.Close()

	payload := "client B still alive"
	conn.Write([]byte(payload))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != payload {
		t.Errorf("got %q, want %q", buf, payload)
	}

	ctrlC := dialControl(t, s)
	defer ctrlC.Close()
	proto.Write(ctrlC, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port1}},
	})
	msg, _ := proto.Read(ctrlC)
	if msg.Type != proto.TypeOK {
		t.Errorf("expected ok after re-register freed port, got %s reason=%s", msg.Type, msg.Reason)
	}
}

func TestServer_MultiClient_SameTunnelIDDifferentClients(t *testing.T) {
	s := newTestServer(t)
	port1 := freePort(t)
	port2 := freePort(t)

	ctrl1 := dialControl(t, s)
	defer ctrl1.Close()
	proto.Write(ctrl1, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port1}},
	})
	proto.Read(ctrl1)

	ctrl2 := dialControl(t, s)
	defer ctrl2.Close()
	proto.Write(ctrl2, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port2}},
	})

	msg, _ := proto.Read(ctrl2)
	if msg.Type != proto.TypeError {
		t.Errorf("expected error for duplicate tunnel_id, got %s", msg.Type)
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
	httpAddr := startHTTPService(t, "hello from http tunnel")

	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "app", Host: "app.example.com"}},
		map[string]string{"app": httpAddr},
	)
	time.Sleep(100 * time.Millisecond)

	req, _ := http.NewRequest("GET", "http://"+s.cfg.HTTPAddr+"/", nil)
	req.Host = "app.example.com"

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "hello from http tunnel") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestServer_EndToEnd_HTTPS(t *testing.T) {
	s := newTestServer(t)
	httpAddr := startHTTPService(t, "hello from tls tunnel")

	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "secure", Host: "secure.example.com"}},
		map[string]string{"secure": httpAddr},
	)
	time.Sleep(100 * time.Millisecond)

	tlsClient := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: insecureTLS,
			DialTLS: func(network, addr string) (net.Conn, error) {
				return tls.Dial(network, s.cfg.TLSAddr, &tls.Config{
					ServerName:         "secure.example.com",
					InsecureSkipVerify: true,
				})
			},
		},
	}

	req, _ := http.NewRequest("GET", "https://secure.example.com/", nil)
	resp, err := tlsClient.Do(req)
	if err != nil {
		t.Fatalf("https: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "hello from tls tunnel") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestServer_HTTP_NoTunnel_Returns502(t *testing.T) {
	s := newTestServer(t)

	req, _ := http.NewRequest("GET", "http://"+s.cfg.HTTPAddr+"/", nil)
	req.Host = "unknown.example.com"

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 502 {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}
}

func TestServer_MultiHost_Aliases(t *testing.T) {
	s := newTestServer(t)
	httpAddr := startHTTPService(t, "hello from aliased tunnel")

	connectTestClient(t, s,
		[]proto.TunnelDef{{
			TunnelID: "app",
			Host:     "app.example.com",
			Hosts:    []string{"alias.example.com", "other.example.com"},
		}},
		map[string]string{"app": httpAddr},
	)
	time.Sleep(100 * time.Millisecond)

	for _, host := range []string{"app.example.com", "alias.example.com", "other.example.com"} {
		host := host
		t.Run(host, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "http://"+s.cfg.HTTPAddr+"/", nil)
			req.Host = host

			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("http: %v", err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), "hello from aliased tunnel") {
				t.Errorf("host %s: unexpected body: %s", host, body)
			}
		})
	}
}

func TestServer_AliasConflict(t *testing.T) {
	s := newTestServer(t)

	ctrl1 := dialControl(t, s)
	defer ctrl1.Close()
	proto.Write(ctrl1, proto.Message{
		Type: proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{
			TunnelID: "app",
			Host:     "app.example.com",
			Hosts:    []string{"alias.example.com"},
		}},
	})
	proto.Read(ctrl1)

	ctrl2 := dialControl(t, s)
	defer ctrl2.Close()
	proto.Write(ctrl2, proto.Message{
		Type: proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{
			TunnelID: "app2",
			Host:     "alias.example.com",
		}},
	})

	msg, _ := proto.Read(ctrl2)
	if msg.Type != proto.TypeError {
		t.Errorf("expected error for alias conflict, got %s", msg.Type)
	}
	if !strings.Contains(msg.Reason, "already in use") {
		t.Errorf("unexpected reason: %s", msg.Reason)
	}
}

func TestServer_AliasCleanupOnDisconnect(t *testing.T) {
	s := newTestServer(t)

	ctrl := dialControl(t, s)
	proto.Write(ctrl, proto.Message{
		Type: proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{
			TunnelID: "app",
			Host:     "app.example.com",
			Hosts:    []string{"alias.example.com"},
		}},
	})
	proto.Read(ctrl)
	time.Sleep(50 * time.Millisecond)

	ctrl.Close()
	time.Sleep(100 * time.Millisecond)

	ctrl2 := dialControl(t, s)
	defer ctrl2.Close()
	proto.Write(ctrl2, proto.Message{
		Type: proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{
			TunnelID: "app",
			Host:     "alias.example.com",
		}},
	})

	msg, _ := proto.Read(ctrl2)
	if msg.Type != proto.TypeOK {
		t.Errorf("expected ok after alias freed, got %s reason=%s", msg.Type, msg.Reason)
	}
}

func TestServer_Heartbeat(t *testing.T) {
	s := newTestServer(t)

	ctrl := dialControl(t, s)
	defer ctrl.Close()

	proto.Write(ctrl, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", Host: "app.example.com"}},
	})
	proto.Read(ctrl)

	if err := proto.Write(ctrl, proto.Message{Type: proto.TypePing}); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	ctrl.SetReadDeadline(time.Now().Add(2 * time.Second))
	msg, err := proto.Read(ctrl)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if msg.Type != proto.TypePong {
		t.Errorf("expected pong, got %s", msg.Type)
	}
}

func TestServer_PendingTimeout(t *testing.T) {
	dir := t.TempDir()
	cfg := config.ServerConfig{
		ControlAddr:     freeAddr(t),
		DataAddr:        freeAddr(t),
		HTTPAddr:        freeAddr(t),
		TLSAddr:         freeAddr(t),
		TLSDomain:       "example.com",
		ControlCertFile: filepath.Join(dir, "control.crt"),
		ControlKeyFile:  filepath.Join(dir, "control.key"),
		MinPublicPort:   1024,
		MaxPublicPort:   65535,
		PendingTimeout:  500 * time.Millisecond,
		Dev:             true,
	}
	s := New(cfg)
	go s.listenControl()
	go s.listenData()
	go s.listenHTTP()
	go s.listenTLS()
	time.Sleep(50 * time.Millisecond)

	ctrl := dialControl(t, s)
	defer ctrl.Close()

	proto.Write(ctrl, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", Host: "app.example.com"}},
	})
	proto.Read(ctrl)
	time.Sleep(50 * time.Millisecond)

	httpConn, err := net.Dial("tcp", s.cfg.HTTPAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer httpConn.Close()

	httpConn.Write([]byte("GET / HTTP/1.1\r\nHost: app.example.com\r\n\r\n"))

	httpConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1)
	_, err = httpConn.Read(buf)
	if err == nil {
		t.Error("expected connection to be closed after pending timeout")
	}
}
