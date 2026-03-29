package server

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/proto"
)

func newTestServerWithPendingTimeout(t *testing.T, timeout time.Duration) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.ServerConfig{
		ControlAddr:     freeAddrTB(t),
		DataAddr:        freeAddrTB(t),
		HTTPAddr:        freeAddrTB(t),
		TLSAddr:         freeAddrTB(t),
		TLSDomain:       "example.com",
		ControlCertFile: filepath.Join(dir, "control.crt"),
		ControlKeyFile:  filepath.Join(dir, "control.key"),
		MinPublicPort:   1024,
		MaxPublicPort:   65535,
		PendingTimeout:  timeout,
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

func TestFault_ControlConnectionDrop(t *testing.T) {
	s := newTestServer(t)
	echoAddr := startEchoService(t)
	port := freePort(t)

	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "echo", PublicPort: port}},
		map[string]string{"echo": echoAddr},
	)
	time.Sleep(100 * time.Millisecond)

	if s.SessionCount() != 1 {
		t.Fatalf("expected 1 session, got %d", s.SessionCount())
	}

	// обрываем control connection изнутри — закрываем все сессии
	s.mu.Lock()
	for _, sess := range s.sessions {
		sess.ctrl.Close()
	}
	s.mu.Unlock()

	time.Sleep(200 * time.Millisecond)

	if s.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after control drop, got %d", s.SessionCount())
	}

	// порт должен освободиться
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Error("expected port to be freed after control drop")
	}
}

func TestFault_ReconnectCleansOldSession(t *testing.T) {
	s := newTestServer(t)
	port := freePort(t)

	// первое подключение
	ctrl1 := dialControl(t, s)
	proto.Write(ctrl1, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port}},
	})
	proto.Read(ctrl1)
	time.Sleep(50 * time.Millisecond)

	if s.SessionCount() != 1 {
		t.Fatalf("expected 1 session, got %d", s.SessionCount())
	}

	// обрываем
	ctrl1.Close()
	time.Sleep(100 * time.Millisecond)

	if s.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after disconnect, got %d", s.SessionCount())
	}

	// переподключаемся с тем же туннелем
	ctrl2 := dialControl(t, s)
	defer ctrl2.Close()
	proto.Write(ctrl2, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "web", PublicPort: port}},
	})

	msg, _ := proto.Read(ctrl2)
	if msg.Type != proto.TypeOK {
		t.Errorf("expected ok on reconnect, got %s reason=%s", msg.Type, msg.Reason)
	}

	if s.SessionCount() != 1 {
		t.Errorf("expected 1 session after reconnect, got %d", s.SessionCount())
	}
}

func TestFault_BackendTimeout(t *testing.T) {
	s := newTestServerWithPendingTimeout(t, 300*time.Millisecond)

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

	httpConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, err = httpConn.Read(buf)
	if err == nil {
		t.Error("expected connection closed after backend timeout")
	}
}

func TestFault_MassDisconnect(t *testing.T) {
	s := newTestServer(t)
	n := 10

	ctrls := make([]net.Conn, n)
	for i := 0; i < n; i++ {
		ctrl := dialControl(t, s)
		ctrls[i] = ctrl
		proto.Write(ctrl, proto.Message{
			Type:    proto.TypeRegister,
			Tunnels: []proto.TunnelDef{{TunnelID: fmt.Sprintf("tunnel-%d", i), Host: fmt.Sprintf("t%d.example.com", i)}},
		})
		proto.Read(ctrl)
	}
	time.Sleep(100 * time.Millisecond)

	if s.SessionCount() != n {
		t.Fatalf("expected %d sessions, got %d", n, s.SessionCount())
	}

	var wg sync.WaitGroup
	for _, ctrl := range ctrls {
		ctrl := ctrl
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctrl.Close()
		}()
	}
	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	if s.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after mass disconnect, got %d", s.SessionCount())
	}

	ctrl := dialControl(t, s)
	defer ctrl.Close()
	proto.Write(ctrl, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "new", Host: "new.example.com"}},
	})
	msg, _ := proto.Read(ctrl)
	if msg.Type != proto.TypeOK {
		t.Errorf("server should accept new connections after mass disconnect, got %s", msg.Type)
	}
}

func TestFault_ConcurrentRegistrations(t *testing.T) {
	s := newTestServer(t)
	n := 20
	var succeeded int64
	var failed int64

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctrl, err := tls.Dial("tcp", s.cfg.ControlAddr, insecureTLS)
			if err != nil {
				atomic.AddInt64(&failed, 1)
				return
			}
			defer ctrl.Close()

			proto.Write(ctrl, proto.Message{
				Type:    proto.TypeRegister,
				Tunnels: []proto.TunnelDef{{TunnelID: fmt.Sprintf("t%d", i), Host: fmt.Sprintf("t%d.example.com", i)}},
			})
			msg, _ := proto.Read(ctrl)
			if msg.Type == proto.TypeOK {
				atomic.AddInt64(&succeeded, 1)
				time.Sleep(100 * time.Millisecond)
			} else {
				atomic.AddInt64(&failed, 1)
			}
		}()
	}
	wg.Wait()
	time.Sleep(300 * time.Millisecond)

	if succeeded != int64(n) {
		t.Errorf("expected all %d registrations to succeed, got %d succeeded %d failed", n, succeeded, failed)
	}
	if s.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after all disconnect, got %d", s.SessionCount())
	}
}

func TestFault_DuplicateTunnelAfterDisconnect(t *testing.T) {
	s := newTestServer(t)

	for i := 0; i < 3; i++ {
		ctrl := dialControl(t, s)
		proto.Write(ctrl, proto.Message{
			Type:    proto.TypeRegister,
			Tunnels: []proto.TunnelDef{{TunnelID: "web", Host: "app.example.com"}},
		})
		msg, _ := proto.Read(ctrl)
		if msg.Type != proto.TypeOK {
			t.Fatalf("iteration %d: expected ok, got %s reason=%s", i, msg.Type, msg.Reason)
		}
		ctrl.Close()
		time.Sleep(100 * time.Millisecond)
	}
}

func TestFault_DataConnectionWithoutControl(t *testing.T) {
	s := newTestServer(t)

	dataConn, err := tls.Dial("tcp", s.cfg.DataAddr, insecureTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer dataConn.Close()

	proto.Write(dataConn, proto.Message{
		Type:   proto.TypeData,
		ConnID: "nonexistent-conn-id",
	})

	dataConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	_, err = dataConn.Read(buf)
	if err == nil {
		t.Error("expected connection to be closed for unknown conn_id")
	}
}

func TestFault_SlowBackend(t *testing.T) {
	s := newTestServer(t)

	slow := make(chan struct{})
	slowLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slowLn.Close() })

	go func() {
		for {
			conn, err := slowLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				<-slow
				io.Copy(c, c)
			}(conn)
		}
	}()

	connectTestClient(t, s,
		[]proto.TunnelDef{{TunnelID: "slow", Host: "slow.example.com"}},
		map[string]string{"slow": slowLn.Addr().String()},
	)
	time.Sleep(100 * time.Millisecond)

	httpConn, err := net.Dial("tcp", s.cfg.HTTPAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer httpConn.Close()

	httpConn.Write([]byte("GET / HTTP/1.1\r\nHost: slow.example.com\r\n\r\n"))

	httpConn2, err := net.Dial("tcp", s.cfg.HTTPAddr)
	if err != nil {
		t.Fatalf("server should handle concurrent connections: %v", err)
	}
	defer httpConn2.Close()

	httpConn2.Write([]byte("GET / HTTP/1.1\r\nHost: unknown.example.com\r\n\r\n"))
	httpConn2.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 512)
	n, _ := httpConn2.Read(buf)
	if n == 0 {
		t.Error("expected 502 response for unknown host")
	}

	close(slow)
}

func TestFault_HostConflict_UnderLoad(t *testing.T) {
	s := newTestServer(t)

	ctrl1 := dialControl(t, s)
	defer ctrl1.Close()
	proto.Write(ctrl1, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: []proto.TunnelDef{{TunnelID: "app", Host: "app.example.com"}},
	})
	proto.Read(ctrl1)

	var conflicts int64
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctrl, err := tls.Dial("tcp", s.cfg.ControlAddr, insecureTLS)
			if err != nil {
				return
			}
			defer ctrl.Close()

			proto.Write(ctrl, proto.Message{
				Type:    proto.TypeRegister,
				Tunnels: []proto.TunnelDef{{TunnelID: "app2", Host: "app.example.com"}},
			})
			msg, _ := proto.Read(ctrl)
			if msg.Type == proto.TypeError {
				atomic.AddInt64(&conflicts, 1)
			}
		}()
	}
	wg.Wait()

	if conflicts != 5 {
		t.Errorf("expected 5 conflicts, got %d", conflicts)
	}
}
