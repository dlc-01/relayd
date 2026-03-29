package server

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dlc-01/relayd/internal/proto"
)

func BenchmarkTunnel_Throughput(b *testing.B) {
	s := newTestServerB(b)
	echoAddr := startEchoServiceB(b)
	port := freePortB(b)

	connectBenchClient(b, s,
		[]proto.TunnelDef{{TunnelID: "echo", PublicPort: port}},
		map[string]string{"echo": echoAddr},
	)
	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	payload := make([]byte, 4096)
	buf := make([]byte, 4096)

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := conn.Write(payload); err != nil {
			b.Fatal(err)
		}
		if _, err := io.ReadFull(conn, buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTunnel_Latency(b *testing.B) {
	s := newTestServerB(b)
	echoAddr := startEchoServiceB(b)
	port := freePortB(b)

	connectBenchClient(b, s,
		[]proto.TunnelDef{{TunnelID: "echo", PublicPort: port}},
		map[string]string{"echo": echoAddr},
	)
	time.Sleep(100 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	payload := []byte("ping")
	buf := make([]byte, 4)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn.Write(payload)
		io.ReadFull(conn, buf)
	}
}

func BenchmarkTunnel_ConcurrentConns(b *testing.B) {
	s := newTestServerB(b)
	echoAddr := startEchoServiceB(b)
	port := freePortB(b)

	connectBenchClient(b, s,
		[]proto.TunnelDef{{TunnelID: "echo", PublicPort: port}},
		map[string]string{"echo": echoAddr},
	)
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err != nil {
			b.Error(err)
			return
		}
		defer conn.Close()

		payload := []byte("hello")
		buf := make([]byte, len(payload))

		for pb.Next() {
			conn.Write(payload)
			io.ReadFull(conn, buf)
		}
	})
}

func BenchmarkServer_HTTPRouting(b *testing.B) {
	s := newTestServerB(b)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	b.Cleanup(ts.Close)

	connectBenchClient(b, s,
		[]proto.TunnelDef{{TunnelID: "app", Host: "app.example.com"}},
		map[string]string{"app": ts.Listener.Addr().String()},
	)
	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", "http://"+s.cfg.HTTPAddr+"/", nil)
		req.Host = "app.example.com"
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkServer_Register(b *testing.B) {
	s := newTestServerB(b)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctrl, err := tls.Dial("tcp", s.cfg.ControlAddr, insecureTLS)
		if err != nil {
			b.Fatal(err)
		}
		proto.Write(ctrl, proto.Message{
			Type:    proto.TypeRegister,
			Tunnels: []proto.TunnelDef{{TunnelID: fmt.Sprintf("t%d", i), Host: fmt.Sprintf("t%d.example.com", i)}},
		})
		proto.Read(ctrl)
		ctrl.Close()
	}
}

func BenchmarkServer_HostLookup(b *testing.B) {
	s := newTestServerB(b)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	b.Cleanup(ts.Close)

	// регистрируем 100 туннелей чтобы map была нагружена
	for i := 0; i < 100; i++ {
		ctrl, err := tls.Dial("tcp", s.cfg.ControlAddr, insecureTLS)
		if err != nil {
			b.Fatal(err)
		}
		proto.Write(ctrl, proto.Message{
			Type:    proto.TypeRegister,
			Tunnels: []proto.TunnelDef{{TunnelID: fmt.Sprintf("t%d", i), Host: fmt.Sprintf("t%d.example.com", i)}},
		})
		proto.Read(ctrl)
		b.Cleanup(func() { ctrl.Close() })
	}

	connectBenchClient(b, s,
		[]proto.TunnelDef{{TunnelID: "target", Host: "target.example.com"}},
		map[string]string{"target": ts.Listener.Addr().String()},
	)
	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", "http://"+s.cfg.HTTPAddr+"/", nil)
		req.Host = "target.example.com"
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// helpers

func newTestServerB(b *testing.B) *Server {
	b.Helper()
	dir := b.TempDir()
	cfg := serverConfig(b, "", 0, dir)
	s := New(cfg)
	go s.listenControl()
	go s.listenData()
	go s.listenHTTP()
	go s.listenTLS()
	time.Sleep(50 * time.Millisecond)
	return s
}

func startEchoServiceB(b *testing.B) string {
	b.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { ln.Close() })
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

func freePortB(b *testing.B) int {
	b.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func connectBenchClient(b *testing.B, s *Server, tunnels []proto.TunnelDef, localAddrs map[string]string) {
	b.Helper()
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
