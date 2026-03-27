package client

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/logger"
	"github.com/dlc-01/relayd/internal/proto"
	"go.uber.org/zap"
)

type Client struct {
	cfg               config.ClientConfig
	log               *zap.SugaredLogger
	tunnels           map[string]string
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration
	reconnectInitial  time.Duration
	reconnectMax      time.Duration
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
		cfg:               cfg,
		log:               log,
		tunnels:           tunnels,
		heartbeatInterval: 30 * time.Second,
		heartbeatTimeout:  10 * time.Second,
		reconnectInitial:  1 * time.Second,
		reconnectMax:      30 * time.Second,
	}
}

func (c *Client) Run() {
	backoff := c.reconnectInitial
	for {
		err := c.connect()
		if err != nil {
			c.log.Warnw("connection failed, reconnecting", "err", err, "backoff", backoff)
		} else {
			c.log.Warnw("disconnected, reconnecting", "backoff", backoff)
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > c.reconnectMax {
			backoff = c.reconnectMax
		}
	}
}

func (c *Client) connect() error {
	ctrl, err := net.DialTimeout("tcp", c.cfg.ServerControlAddr, 10*time.Second)
	if err != nil {
		return err
	}
	defer ctrl.Close()

	var defs []proto.TunnelDef
	for _, t := range c.cfg.Tunnels {
		defs = append(defs, proto.TunnelDef{
			TunnelID:   t.TunnelID,
			PublicPort: t.PublicPort,
			Host:       t.Host,
			Hosts:      t.Hosts,
		})
	}

	if err := proto.Write(ctrl, proto.Message{
		Type:    proto.TypeRegister,
		Tunnels: defs,
	}); err != nil {
		return err
	}

	msg, err := proto.Read(ctrl)
	if err != nil {
		return err
	}
	if msg.Type == proto.TypeError {
		c.log.Fatalw("register rejected", "reason", msg.Reason)
	}
	if msg.Type != proto.TypeOK {
		return fmt.Errorf("unexpected register response: %s", msg.Type)
	}

	c.log.Infow("registered tunnels", "count", len(c.cfg.Tunnels))
	for _, t := range c.cfg.Tunnels {
		c.log.Infow("tunnel active",
			"tunnel_id", t.TunnelID,
			"local_addr", t.LocalAddr,
			"public_port", t.PublicPort,
			"host", t.Host,
			"aliases", t.Hosts,
		)
	}

	done := make(chan struct{})
	defer close(done)
	go c.heartbeat(ctrl, done)

	for {
		msg, err := proto.Read(ctrl)
		if err != nil {
			return err
		}
		switch msg.Type {
		case proto.TypeConnect:
			go c.handleConnect(msg.ConnID, msg.TunnelID)
		case proto.TypePong:
			c.log.Debugw("pong received")
		default:
			c.log.Warnw("unexpected message", "type", msg.Type)
		}
	}
}

func (c *Client) heartbeat(ctrl net.Conn, done chan struct{}) {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			ctrl.SetWriteDeadline(time.Now().Add(c.heartbeatTimeout))
			if err := proto.Write(ctrl, proto.Message{Type: proto.TypePing}); err != nil {
				c.log.Warnw("heartbeat send failed", "err", err)
				ctrl.Close()
				return
			}
			ctrl.SetWriteDeadline(time.Time{})
			c.log.Debugw("ping sent")
		}
	}
}

func (c *Client) handleConnect(connID, tunnelID string) {
	localAddr, ok := c.tunnels[tunnelID]
	if !ok {
		c.log.Warnw("unknown tunnel_id",
			"tunnel_id", tunnelID,
			"conn_id", connID,
		)
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
