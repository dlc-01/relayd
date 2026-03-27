package config

import "testing"

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
