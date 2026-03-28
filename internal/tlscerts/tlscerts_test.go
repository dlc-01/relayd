package tlscerts

import (
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
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

func TestGenerateAndSave(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	cert, err := GenerateAndSave(certFile, keyFile)
	if err != nil {
		t.Fatalf("GenerateAndSave: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Error("expected certificate data")
	}

	if _, err := os.Stat(certFile); err != nil {
		t.Errorf("cert file not created: %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Errorf("key file not created: %v", err)
	}
}

func TestLoadOrGenerate_GeneratesIfMissing(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	cert, err := LoadOrGenerate(certFile, keyFile)
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Error("expected certificate")
	}

	cert2, err := LoadOrGenerate(certFile, keyFile)
	if err != nil {
		t.Fatalf("LoadOrGenerate second: %v", err)
	}

	fp1, _ := Fingerprint(cert)
	fp2, _ := Fingerprint(cert2)
	if fp1 != fp2 {
		t.Error("fingerprint should be same on second load")
	}
}

func TestFingerprint(t *testing.T) {
	cert, err := SelfSigned("example.com")
	if err != nil {
		t.Fatal(err)
	}

	fp1, err := Fingerprint(cert)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if len(fp1) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(fp1))
	}

	fp2, _ := Fingerprint(cert)
	if fp1 != fp2 {
		t.Error("fingerprint should be deterministic")
	}
}

func TestFingerprint_DifferentCerts(t *testing.T) {
	cert1, _ := SelfSigned("example.com")
	cert2, _ := SelfSigned("example.com")

	fp1, _ := Fingerprint(cert1)
	fp2, _ := Fingerprint(cert2)

	if fp1 == fp2 {
		t.Error("different certs should have different fingerprints")
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

func TestBuildTLSConfig_DefaultCert(t *testing.T) {
	tlsCfg, err := BuildTLSConfig("", "", "example.com", nil)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if tlsCfg == nil {
		t.Fatal("expected non-nil tls config")
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
