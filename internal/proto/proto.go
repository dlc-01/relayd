package proto

import (
	"encoding/json"
	"fmt"
	"net"
)

type MessageType string

const (
	TypeRegister MessageType = "register"
	TypeOK       MessageType = "ok"
	TypeConnect  MessageType = "connect"
	TypeData     MessageType = "data"
	TypeError    MessageType = "error"
)

type Message struct {
	Type     MessageType `json:"type"`
	TunnelID string      `json:"tunnel_id,omitempty"`
	ConnID   string      `json:"conn_id,omitempty"`
	Reason   string      `json:"reason,omitempty"`
}

func Write(conn net.Conn, msg Message) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = conn.Write(b)
	return err
}

func Read(conn net.Conn) (Message, error) {
	var msg Message
	buf := make([]byte, 4096)
	n, err := readLine(conn, buf)
	if err != nil {
		return msg, err
	}
	err = json.Unmarshal(buf[:n], &msg)
	return msg, err
}

func readLine(conn net.Conn, buf []byte) (int, error) {
	i := 0
	for i < len(buf) {
		_, err := conn.Read(buf[i : i+1])
		if err != nil {
			return 0, err
		}
		if buf[i] == '\n' {
			return i, nil
		}
		i++
	}
	return 0, fmt.Errorf("message too long")
}
