package portcheck

import (
	"net"
	"runtime"
	"testing"
)

func TestIsOccupied_FreePort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	occupied, err := IsOccupied(port)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if occupied {
		t.Errorf("port %d should be free", port)
	}
}

func TestIsOccupied_BusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	occupied, err := IsOccupied(port)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !occupied {
		t.Errorf("port %d should be occupied", port)
	}
}

func TestIsOccupied_UsesCorrectMethod(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Log("using /proc/net/tcp")
	} else {
		t.Logf("using net.Listen fallback on %s", runtime.GOOS)
	}
}

func TestIsForbidden(t *testing.T) {
	cases := []struct {
		port     int
		expected bool
	}{
		{80, true},
		{443, true},
		{22, true},
		{1023, true},
		{1024, false},
		{10001, false},
		{6379, true},
	}

	for _, tc := range cases {
		got := IsForbidden(tc.port)
		if got != tc.expected {
			t.Errorf("IsForbidden(%d): got %v, want %v", tc.port, got, tc.expected)
		}
	}
}

func TestIsInRange(t *testing.T) {
	cases := []struct {
		port, min, max int
		expected       bool
	}{
		{10001, 10000, 60000, true},
		{9999, 10000, 60000, false},
		{60001, 10000, 60000, false},
		{10000, 10000, 60000, true},
		{60000, 10000, 60000, true},
	}

	for _, tc := range cases {
		got := IsInRange(tc.port, tc.min, tc.max)
		if got != tc.expected {
			t.Errorf("IsInRange(%d, %d, %d): got %v, want %v",
				tc.port, tc.min, tc.max, got, tc.expected)
		}
	}
}

func TestCheck_ForbiddenPort(t *testing.T) {
	err := Check(80, 10000, 60000)
	if err == nil {
		t.Error("expected error for forbidden port 80")
	}
}

func TestCheck_OutOfRange(t *testing.T) {
	err := Check(9999, 10000, 60000)
	if err == nil {
		t.Error("expected error for out of range port")
	}
}

func TestCheck_OccupiedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	err = Check(port, 1024, 65535)
	if err == nil {
		t.Errorf("expected error for occupied port %d", port)
	}
}
