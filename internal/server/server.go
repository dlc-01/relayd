package server

import (
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/logger"
	"github.com/dlc-01/relayd/internal/proto"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Server struct {
	cfg     config.ServerConfig
	log     *zap.SugaredLogger
	mu      sync.Mutex
	pending map[string]net.Conn
	ports   map[int]string
	tunnels map[string]net.Conn
}

func New(cfg config.ServerConfig) *Server {
	var log *zap.SugaredLogger
	if cfg.Dev {
		log = logger.NewDev("server")
	} else {
		log = logger.New("server")
	}
	return &Server{
		cfg:     cfg,
		log:     log,
		pending: make(map[string]net.Conn),
		ports:   make(map[int]string),
		tunnels: make(map[string]net.Conn),
	}
}

func (s *Server) Run() {
	go s.listenControl()
	go s.listenData()
	select {}
}

func (s *Server) listenControl() {
	ln, err := net.Listen("tcp", s.cfg.ControlAddr)
	if err != nil {
		s.log.Fatalw("control listen failed",
			"addr", s.cfg.ControlAddr,
			"err", err,
		)
	}
	s.log.Infow("control listening", "addr", s.cfg.ControlAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.log.Errorw("control accept failed", "err", err)
			continue
		}
		s.log.Debugw("new control connection", "remote", conn.RemoteAddr())
		go s.handleControl(conn)
	}
}

func (s *Server) handleControl(ctrl net.Conn) {
	defer ctrl.Close()
	remote := ctrl.RemoteAddr().String()

	msg, err := proto.Read(ctrl)
	if err != nil || msg.Type != proto.TypeRegister {
		s.log.Warnw("expected register",
			"remote", remote,
			"got", msg.Type,
			"err", err,
		)
		return
	}

	if len(msg.Tunnels) == 0 {
		s.log.Warnw("register with no tunnels", "remote", remote)
		proto.Write(ctrl, proto.Message{
			Type:   proto.TypeError,
			Reason: "no tunnels in register",
		})
		return
	}

	s.mu.Lock()
	for _, t := range msg.Tunnels {
		if existing, ok := s.ports[t.PublicPort]; ok {
			s.mu.Unlock()
			s.log.Warnw("port conflict",
				"remote", remote,
				"port", t.PublicPort,
				"occupied_by", existing,
			)
			proto.Write(ctrl, proto.Message{
				Type:   proto.TypeError,
				Reason: fmt.Sprintf("port %d already in use by tunnel %s", t.PublicPort, existing),
			})
			return
		}
	}
	for _, t := range msg.Tunnels {
		s.ports[t.PublicPort] = t.TunnelID
		s.tunnels[t.TunnelID] = ctrl
		s.log.Infow("tunnel registered",
			"remote", remote,
			"tunnel_id", t.TunnelID,
			"public_port", t.PublicPort,
		)
	}
	s.mu.Unlock()

	if err := proto.Write(ctrl, proto.Message{Type: proto.TypeOK}); err != nil {
		s.log.Errorw("write ok failed", "remote", remote, "err", err)
		return
	}

	for _, t := range msg.Tunnels {
		go s.listenPublic(t.TunnelID, t.PublicPort, ctrl)
	}

	buf := make([]byte, 1)
	ctrl.Read(buf)

	s.mu.Lock()
	for _, t := range msg.Tunnels {
		delete(s.ports, t.PublicPort)
		delete(s.tunnels, t.TunnelID)
		s.log.Infow("tunnel unregistered",
			"remote", remote,
			"tunnel_id", t.TunnelID,
			"public_port", t.PublicPort,
		)
	}
	s.mu.Unlock()
}

func (s *Server) listenData() {
	ln, err := net.Listen("tcp", s.cfg.DataAddr)
	if err != nil {
		s.log.Fatalw("data listen failed",
			"addr", s.cfg.DataAddr,
			"err", err,
		)
	}
	s.log.Infow("data listening", "addr", s.cfg.DataAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.log.Errorw("data accept failed", "err", err)
			continue
		}
		go s.handleData(conn)
	}
}

func (s *Server) handleData(dataConn net.Conn) {
	msg, err := proto.Read(dataConn)
	if err != nil || msg.Type != proto.TypeData {
		s.log.Warnw("expected data msg",
			"got", msg.Type,
			"err", err,
		)
		dataConn.Close()
		return
	}

	s.mu.Lock()
	extConn, ok := s.pending[msg.ConnID]
	if ok {
		delete(s.pending, msg.ConnID)
	}
	s.mu.Unlock()

	if !ok {
		s.log.Warnw("unknown connID", "conn_id", msg.ConnID)
		dataConn.Close()
		return
	}

	s.log.Infow("bridging connection",
		"conn_id", msg.ConnID,
	)
	bridge(extConn, dataConn)
	s.log.Debugw("connection closed", "conn_id", msg.ConnID)
}

func (s *Server) listenPublic(tunnelID string, port int, ctrl net.Conn) {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.log.Errorw("public listen failed",
			"addr", addr,
			"tunnel_id", tunnelID,
			"err", err,
		)
		return
	}
	defer ln.Close()
	s.log.Infow("public listening",
		"addr", addr,
		"tunnel_id", tunnelID,
	)

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.log.Errorw("public accept failed",
				"tunnel_id", tunnelID,
				"err", err,
			)
			return
		}

		connID := uuid.NewString()
		s.mu.Lock()
		s.pending[connID] = conn
		s.mu.Unlock()

		s.log.Infow("new external connection",
			"tunnel_id", tunnelID,
			"conn_id", connID,
			"remote", conn.RemoteAddr(),
		)

		if err := proto.Write(ctrl, proto.Message{
			Type:     proto.TypeConnect,
			ConnID:   connID,
			TunnelID: tunnelID,
		}); err != nil {
			s.log.Errorw("write connect failed",
				"tunnel_id", tunnelID,
				"conn_id", connID,
				"err", err,
			)
			s.mu.Lock()
			delete(s.pending, connID)
			s.mu.Unlock()
			conn.Close()
		}
	}
}

func bridge(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}
