package config

import (
	"os"
	"testing"
)

func TestParseTunnel_TCP(t *testing.T) {
	tc, ok := parseTunnel("web:10001:127.0.0.1:8080")
	if !ok {
		t.Fatal("expected ok")
	}
	if tc.TunnelID != "web" || tc.PublicPort != 10001 || tc.LocalAddr != "127.0.0.1:8080" {
		t.Errorf("got %+v", tc)
	}
}

func TestParseTunnel_HTTP(t *testing.T) {
	tc, ok := parseTunnel("app:host:app.example.com:127.0.0.1:8080")
	if !ok {
		t.Fatal("expected ok")
	}
	if tc.TunnelID != "app" || tc.Host != "app.example.com" || tc.LocalAddr != "127.0.0.1:8080" {
		t.Errorf("got %+v", tc)
	}
	if len(tc.Hosts) != 0 {
		t.Errorf("expected no aliases, got %v", tc.Hosts)
	}
}

func TestParseTunnel_HTTP_MultiHost(t *testing.T) {
	tc, ok := parseTunnel("app:host:app.example.com|alias.com|other.com:127.0.0.1:8080")
	if !ok {
		t.Fatal("expected ok")
	}
	if tc.Host != "app.example.com" {
		t.Errorf("host: got %s", tc.Host)
	}
	if tc.LocalAddr != "127.0.0.1:8080" {
		t.Errorf("local_addr: got %s", tc.LocalAddr)
	}
	if len(tc.Hosts) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(tc.Hosts))
	}
	if tc.Hosts[0] != "alias.com" || tc.Hosts[1] != "other.com" {
		t.Errorf("aliases: got %v", tc.Hosts)
	}
}

func TestParseTunnel_HTTPS(t *testing.T) {
	tc, ok := parseTunnel("secure:https:app.example.com:127.0.0.1:8080")
	if !ok {
		t.Fatal("expected ok")
	}
	if tc.TunnelID != "secure" || tc.Host != "app.example.com" || tc.LocalAddr != "127.0.0.1:8080" {
		t.Errorf("got %+v", tc)
	}
}

func TestParseTunnel_HTTPS_MultiHost(t *testing.T) {
	tc, ok := parseTunnel("secure:https:app.example.com|alias.com:127.0.0.1:8443")
	if !ok {
		t.Fatal("expected ok")
	}
	if tc.Host != "app.example.com" {
		t.Errorf("host: got %s", tc.Host)
	}
	if len(tc.Hosts) != 1 || tc.Hosts[0] != "alias.com" {
		t.Errorf("aliases: got %v", tc.Hosts)
	}
	if tc.LocalAddr != "127.0.0.1:8443" {
		t.Errorf("local_addr: got %s", tc.LocalAddr)
	}
}

func TestParseTunnel_Invalid(t *testing.T) {
	cases := []string{
		"",
		"web",
		"web:notaport:127.0.0.1:8080",
		"app:host:",
		"app:https:",
	}
	for _, c := range cases {
		_, ok := parseTunnel(c)
		if ok {
			t.Errorf("expected not ok for %q", c)
		}
	}
}

func TestLoadServerConfig_TLSDomains(t *testing.T) {
	os.Setenv("RELAYD_TLS_DOMAINS", "example.com:/etc/certs/example.crt:/etc/certs/example.key,test.com:/etc/certs/test.crt:/etc/certs/test.key")
	defer os.Unsetenv("RELAYD_TLS_DOMAINS")

	cfg := LoadServerConfig()
	if len(cfg.TLSDomains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(cfg.TLSDomains))
	}
	if cfg.TLSDomains[0].Domain != "example.com" {
		t.Errorf("domain[0]: got %s", cfg.TLSDomains[0].Domain)
	}
	if cfg.TLSDomains[1].Domain != "test.com" {
		t.Errorf("domain[1]: got %s", cfg.TLSDomains[1].Domain)
	}
}

func TestLoadServerConfig_TLSDomains_Empty(t *testing.T) {
	os.Unsetenv("RELAYD_TLS_DOMAINS")
	cfg := LoadServerConfig()
	if len(cfg.TLSDomains) != 0 {
		t.Errorf("expected 0 domains, got %d", len(cfg.TLSDomains))
	}
}

func TestLoadServerConfig_TLSDomains_Invalid(t *testing.T) {
	os.Setenv("RELAYD_TLS_DOMAINS", "invalid,example.com:/cert:/key")
	defer os.Unsetenv("RELAYD_TLS_DOMAINS")

	cfg := LoadServerConfig()
	if len(cfg.TLSDomains) != 1 {
		t.Errorf("expected 1 valid domain, got %d", len(cfg.TLSDomains))
	}
}
