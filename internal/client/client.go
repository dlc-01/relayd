package client

import (
	"io"
	"log"
	"net"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/proto"
)

type Client struct {
	cfg config.ClientConfig
}

func New(cfg config.ClientConfig) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Run() {
	ctrl, err := net.Dial("tcp", c.cfg.ServerControlAddr)
	if err != nil {
		log.Fatalf("dial control: %v", err)
	}
	defer ctrl.Close()

	if err := proto.Write(ctrl, proto.Message{
		Type:     proto.TypeRegister,
		TunnelID: c.cfg.TunnelID,
	}); err != nil {
		log.Fatalf("register: %v", err)
	}

	msg, err := proto.Read(ctrl)
	if err != nil || msg.Type != proto.TypeOK {
		log.Fatalf("expected ok: %v %v", msg, err)
	}
	log.Printf("registered tunnel: %s", c.cfg.TunnelID)

	for {
		msg, err := proto.Read(ctrl)
		if err != nil {
			log.Printf("control read: %v", err)
			return
		}
		if msg.Type != proto.TypeConnect {
			log.Printf("unexpected msg: %v", msg)
			continue
		}
		go c.handleConnect(msg.ConnID)
	}
}

func (c *Client) handleConnect(connID string) {
	localConn, err := net.Dial("tcp", c.cfg.LocalAddr)
	if err != nil {
		log.Printf("dial local: %v", err)
		return
	}

	dataConn, err := net.Dial("tcp", c.cfg.ServerDataAddr)
	if err != nil {
		log.Printf("dial data: %v", err)
		localConn.Close()
		return
	}

	if err := proto.Write(dataConn, proto.Message{
		Type:   proto.TypeData,
		ConnID: connID,
	}); err != nil {
		log.Printf("send data msg: %v", err)
		localConn.Close()
		dataConn.Close()
		return
	}

	log.Printf("bridging connID=%s to %s", connID, c.cfg.LocalAddr)
	bridge(localConn, dataConn)
}

func bridge(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}
