package proto

import (
	"net"
	"testing"
)

func TestWriteRead_AllTypes(t *testing.T) {
	cases := []Message{
		{
			Type: TypeRegister,
			Tunnels: []TunnelDef{
				{TunnelID: "web", PublicPort: 10001},
				{TunnelID: "app", Host: "app.example.com"},
				{TunnelID: "multi", Host: "app.example.com", Hosts: []string{"alias.example.com", "other.example.com"}},
			},
		},
		{Type: TypeOK},
		{Type: TypeConnect, ConnID: "abc-123", TunnelID: "web"},
		{Type: TypeData, ConnID: "abc-123"},
		{Type: TypeError, Reason: "port already in use"},
		{Type: TypePing},
		{Type: TypePong},
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
			if len(got.Tunnels) != len(msg.Tunnels) {
				t.Fatalf("tunnels len: got %d, want %d", len(got.Tunnels), len(msg.Tunnels))
			}
			for i := range msg.Tunnels {
				if got.Tunnels[i].TunnelID != msg.Tunnels[i].TunnelID {
					t.Errorf("tunnel[%d].TunnelID: got %s, want %s", i, got.Tunnels[i].TunnelID, msg.Tunnels[i].TunnelID)
				}
				if len(got.Tunnels[i].Hosts) != len(msg.Tunnels[i].Hosts) {
					t.Errorf("tunnel[%d].Hosts len: got %d, want %d", i, len(got.Tunnels[i].Hosts), len(msg.Tunnels[i].Hosts))
				}
				for j := range msg.Tunnels[i].Hosts {
					if got.Tunnels[i].Hosts[j] != msg.Tunnels[i].Hosts[j] {
						t.Errorf("tunnel[%d].Hosts[%d]: got %s, want %s", i, j, got.Tunnels[i].Hosts[j], msg.Tunnels[i].Hosts[j])
					}
				}
			}
		})
	}
}

func TestRead_InvalidJSON(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() { server.Write([]byte("not json\n")) }()

	_, err := Read(client)
	if err == nil {
		t.Error("expected error on invalid json, got nil")
	}
}

func TestRead_ConnectionClosed(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	go func() { server.Close() }()

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
