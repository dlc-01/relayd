package config

import "testing"

func TestParseTunnel_TCP(t *testing.T) {
	tc, ok := parseTunnel("web:10001:127.0.0.1:8080")
	if !ok {
		t.Fatal("expected ok")
	}
	if tc.TunnelID != "web" {
		t.Errorf("tunnel_id: got %s", tc.TunnelID)
	}
	if tc.PublicPort != 10001 {
		t.Errorf("port: got %d", tc.PublicPort)
	}
	if tc.LocalAddr != "127.0.0.1:8080" {
		t.Errorf("local_addr: got %s", tc.LocalAddr)
	}
	if tc.Host != "" {
		t.Errorf("host should be empty, got %s", tc.Host)
	}
}

func TestParseTunnel_HTTP(t *testing.T) {
	tc, ok := parseTunnel("app:host:app.giveoffer.solutions:127.0.0.1:8080")
	if !ok {
		t.Fatal("expected ok")
	}
	if tc.TunnelID != "app" {
		t.Errorf("tunnel_id: got %s", tc.TunnelID)
	}
	if tc.Host != "app.giveoffer.solutions" {
		t.Errorf("host: got %s", tc.Host)
	}
	if tc.LocalAddr != "127.0.0.1:8080" {
		t.Errorf("local_addr: got %s", tc.LocalAddr)
	}
	if tc.PublicPort != 0 {
		t.Errorf("port should be 0, got %d", tc.PublicPort)
	}
}

func TestParseTunnel_Invalid(t *testing.T) {
	cases := []string{
		"",
		"web",
		"web:notaport:127.0.0.1:8080",
		"app:host:",
		"app:host::127.0.0.1:8080",
	}
	for _, c := range cases {
		_, ok := parseTunnel(c)
		if ok {
			t.Errorf("expected not ok for %q", c)
		}
	}
}
