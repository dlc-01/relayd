package client

import (
	"io"
	"net"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/logger"
	"github.com/dlc-01/relayd/internal/proto"
	"go.uber.org/zap"
)

type Client struct {
	cfg     config.ClientConfig
	log     *zap.SugaredLogger
	tunnels map[string]string
}

func New(cfg config.ClientConfig) *Client {
	var log *zap.SugaredLogger
	if cfg.Dev {
		log = logger.NewDev("client")
	} else {
		log = logger.New("client")
	}
	tunnels := make(map[string]string)
	for _, t := range cfg.Tunnels {
		tunnels[t.TunnelID] = t.LocalAddr
	}
	return &Client{
		cfg:     cfg,
		log:     log,
		tunnels: tunnels,
	}
}

func (c *Client) Run() {
	ctrl, err := net.Dial("tcp", c.cfg.ServerControlAddr)
	if err != nil {
		c.log.Fatalw("dial control failed",
			"addr", c.cfg.ServerControlAddr,
			"err", err,
		)
	}
	defer ctrl.Close()

	var defs []proto.TunnelDef
	for _, t := range c.cfg.Tunnels {
		defs = append(defs, proto.TunnelDef{
			TunnelID:   t.TunnelID,
			PublicPort: t.PublicPort,
		})
	}

	if err := proto.Write(ctrl, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: defs,
	}); err != nil {
		c.log.Fatalw("register failed", "err", err)
	}

	msg, err := proto.Read(ctrl)
	if err != nil {
		c.log.Fatalw("read register response failed", "err", err)
	}
	if msg.Type == proto.TypeError {
		c.log.Fatalw("register rejected", "reason", msg.Reason)
	}
	if msg.Type != proto.TypeOK {
		c.log.Fatalw("unexpected register response", "type", msg.Type)
	}

	c.log.Infow("registered tunnels", "count", len(c.cfg.Tunnels))
	for _, t := range c.cfg.Tunnels {
		c.log.Infow("tunnel active",
			"tunnel_id", t.TunnelID,
			"local_addr", t.LocalAddr,
			"public_port", t.PublicPort,
		)
	}

	for {
		msg, err := proto.Read(ctrl)
		if err != nil {
			c.log.Errorw("control read failed", "err", err)
			return
		}
		if msg.Type != proto.TypeConnect {
			c.log.Warnw("unexpected message", "type", msg.Type)
			continue
		}
		go c.handleConnect(msg.ConnID, msg.TunnelID)
	}
}

func (c *Client) handleConnect(connID, tunnelID string) {
	localAddr, ok := c.tunnels[tunnelID]
	if !ok {
		c.log.Warnw("unknown tunnel_id", "tunnel_id", tunnelID, "conn_id", connID)
		return
	}

	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		c.log.Errorw("dial local failed",
			"tunnel_id", tunnelID,
			"local_addr", localAddr,
			"conn_id", connID,
			"err", err,
		)
		return
	}

	dataConn, err := net.Dial("tcp", c.cfg.ServerDataAddr)
	if err != nil {
		c.log.Errorw("dial data failed",
			"tunnel_id", tunnelID,
			"conn_id", connID,
			"err", err,
		)
		localConn.Close()
		return
	}

	if err := proto.Write(dataConn, proto.Message{
		Type:   proto.TypeData,
		ConnID: connID,
	}); err != nil {
		c.log.Errorw("send data msg failed",
			"tunnel_id", tunnelID,
			"conn_id", connID,
			"err", err,
		)
		localConn.Close()
		dataConn.Close()
		return
	}

	c.log.Infow("bridging",
		"conn_id", connID,
		"tunnel_id", tunnelID,
		"local_addr", localAddr,
	)
	bridge(localConn, dataConn)
	c.log.Debugw("bridge closed",
		"conn_id", connID,
		"tunnel_id", tunnelID,
	)
}

func bridge(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}
