package tlscerts

import (
	"crypto/tls"
	"net"
	"testing"
)

func TestSelfSigned(t *testing.T) {
	cert, err := SelfSigned("app.example.com", "*.example.com")
	if err != nil {
		t.Fatalf("SelfSigned: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Error("expected certificate data")
	}
}

func TestSelfSigned_TLSHandshake(t *testing.T) {
	cert, err := SelfSigned("*.example.com")
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		done <- conn.(*tls.Conn).Handshake()
	}()

	clientConn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		ServerName:         "app.example.com",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	defer clientConn.Close()

	if err := <-done; err != nil {
		t.Fatalf("handshake: %v", err)
	}
}

func TestLoadOrSelfSigned_FallsBackToSelfSigned(t *testing.T) {
	cert, err := LoadOrSelfSigned("", "", "*.example.com")
	if err != nil {
		t.Fatalf("LoadOrSelfSigned: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Error("expected certificate")
	}
}

func TestSelfSigned_SNI(t *testing.T) {
	cert, err := SelfSigned("*.example.com")
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	sniCh := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		tlsConn := conn.(*tls.Conn)
		tlsConn.Handshake()
		sniCh <- tlsConn.ConnectionState().ServerName
	}()

	clientConn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		ServerName:         "app.example.com",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()

	sni := <-sniCh
	if sni != "app.example.com" {
		t.Errorf("got SNI %q, want %q", sni, "app.example.com")
	}
}

func TestSelfSigned_MultipleConns(t *testing.T) {
	cert, err := SelfSigned("*.example.com")
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				c.(*tls.Conn).Handshake()
			}(conn)
		}
	}()

	for i := 0; i < 3; i++ {
		conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		conn.Close()
	}
}
