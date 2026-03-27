package proto

import (
	"net"
	"testing"
)

func TestWriteRead_AllTypes(t *testing.T) {
	cases := []Message{
		{Type: TypeRegister, TunnelID: "web"},
		{Type: TypeOK},
		{Type: TypeConnect, ConnID: "abc-123"},
		{Type: TypeData, ConnID: "abc-123"},
		{TypeError, "", "", "something went wrong"},
	}

	for _, msg := range cases {
		msg := msg
		t.Run(string(msg.Type), func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()

			go func() {
				if err := Write(server, msg); err != nil {
					t.Errorf("write: %v", err)
				}
			}()

			got, err := Read(client)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if got.Type != msg.Type {
				t.Errorf("type: got %s, want %s", got.Type, msg.Type)
			}
			if got.ConnID != msg.ConnID {
				t.Errorf("conn_id: got %s, want %s", got.ConnID, msg.ConnID)
			}
			if got.TunnelID != msg.TunnelID {
				t.Errorf("tunnel_id: got %s, want %s", got.TunnelID, msg.TunnelID)
			}
			if got.Reason != msg.Reason {
				t.Errorf("reason: got %s, want %s", got.Reason, msg.Reason)
			}
		})
	}
}

func TestRead_InvalidJSON(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		server.Write([]byte("not json\n"))
	}()

	_, err := Read(client)
	if err == nil {
		t.Error("expected error on invalid json, got nil")
	}
}

func TestRead_ConnectionClosed(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	go func() {
		server.Close()
	}()

	_, err := Read(client)
	if err == nil {
		t.Error("expected error on closed connection, got nil")
	}
}

func TestWrite_ClosedConnection(t *testing.T) {
	server, client := net.Pipe()
	server.Close()
	client.Close()

	err := Write(server, Message{Type: TypeOK})
	if err == nil {
		t.Error("expected error writing to closed conn, got nil")
	}
}
