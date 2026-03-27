package server

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/httpparse"
	"github.com/dlc-01/relayd/internal/logger"
	"github.com/dlc-01/relayd/internal/portcheck"
	"github.com/dlc-01/relayd/internal/proto"
	"github.com/dlc-01/relayd/internal/tlscerts"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type pendingConn struct {
	conn   net.Conn
	peeked []byte
}

type Server struct {
	cfg     config.ServerConfig
	log     *zap.SugaredLogger
	mu      sync.Mutex
	pending map[string]pendingConn
	ports   map[int]string
	hosts   map[string]string
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
		pending: make(map[string]pendingConn),
		ports:   make(map[int]string),
		hosts:   make(map[string]string),
		tunnels: make(map[string]net.Conn),
	}
}

func (s *Server) Run() {
	go s.listenControl()
	go s.listenData()
	go s.listenHTTP()
	go s.listenTLS()
	select {}
}

func (s *Server) listenControl() {
	ln, err := net.Listen("tcp", s.cfg.ControlAddr)
	if err != nil {
		s.log.Fatalw("control listen failed", "addr", s.cfg.ControlAddr, "err", err)
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
		s.log.Warnw("expected register", "remote", remote, "got", msg.Type, "err", err)
		return
	}

	if len(msg.Tunnels) == 0 {
		proto.Write(ctrl, proto.Message{Type: proto.TypeError, Reason: "no tunnels in register"})
		return
	}

	for _, t := range msg.Tunnels {
		if t.TunnelID == "" {
			proto.Write(ctrl, proto.Message{Type: proto.TypeError, Reason: "tunnel_id cannot be empty"})
			return
		}
		if t.PublicPort == 0 && t.Host == "" {
			proto.Write(ctrl, proto.Message{
				Type:   proto.TypeError,
				Reason: fmt.Sprintf("tunnel %s must have public_port or host", t.TunnelID),
			})
			return
		}
	}

	s.mu.Lock()
	for _, t := range msg.Tunnels {
		if t.PublicPort != 0 {
			if existing, ok := s.ports[t.PublicPort]; ok {
				s.mu.Unlock()
				proto.Write(ctrl, proto.Message{
					Type:   proto.TypeError,
					Reason: fmt.Sprintf("port %d already in use by tunnel %s", t.PublicPort, existing),
				})
				return
			}
		}
		if t.Host != "" {
			if existing, ok := s.hosts[t.Host]; ok {
				s.mu.Unlock()
				proto.Write(ctrl, proto.Message{
					Type:   proto.TypeError,
					Reason: fmt.Sprintf("host %s already in use by tunnel %s", t.Host, existing),
				})
				return
			}
		}
	}
	s.mu.Unlock()

	for _, t := range msg.Tunnels {
		if t.PublicPort != 0 {
			if err := portcheck.Check(t.PublicPort, s.cfg.MinPublicPort, s.cfg.MaxPublicPort); err != nil {
				proto.Write(ctrl, proto.Message{
					Type:   proto.TypeError,
					Reason: fmt.Sprintf("tunnel %s: %s", t.TunnelID, err.Error()),
				})
				return
			}
		}
	}

	s.mu.Lock()
	for _, t := range msg.Tunnels {
		s.tunnels[t.TunnelID] = ctrl
		if t.PublicPort != 0 {
			s.ports[t.PublicPort] = t.TunnelID
		}
		if t.Host != "" {
			s.hosts[t.Host] = t.TunnelID
		}
		s.log.Infow("tunnel registered",
			"remote", remote,
			"tunnel_id", t.TunnelID,
			"public_port", t.PublicPort,
			"host", t.Host,
		)
	}
	s.mu.Unlock()

	if err := proto.Write(ctrl, proto.Message{Type: proto.TypeOK}); err != nil {
		s.log.Errorw("write ok failed", "remote", remote, "err", err)
		return
	}

	for _, t := range msg.Tunnels {
		if t.PublicPort != 0 {
			go s.listenPublic(t.TunnelID, t.PublicPort, ctrl)
		}
	}

	buf := make([]byte, 1)
	ctrl.Read(buf)

	s.mu.Lock()
	for _, t := range msg.Tunnels {
		delete(s.tunnels, t.TunnelID)
		if t.PublicPort != 0 {
			delete(s.ports, t.PublicPort)
		}
		if t.Host != "" {
			delete(s.hosts, t.Host)
		}
		s.log.Infow("tunnel unregistered", "remote", remote, "tunnel_id", t.TunnelID)
	}
	s.mu.Unlock()
}

func (s *Server) listenData() {
	ln, err := net.Listen("tcp", s.cfg.DataAddr)
	if err != nil {
		s.log.Fatalw("data listen failed", "addr", s.cfg.DataAddr, "err", err)
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
		s.log.Warnw("expected data msg", "got", msg.Type, "err", err)
		dataConn.Close()
		return
	}

	s.mu.Lock()
	pc, ok := s.pending[msg.ConnID]
	if ok {
		delete(s.pending, msg.ConnID)
	}
	s.mu.Unlock()

	if !ok {
		s.log.Warnw("unknown connID", "conn_id", msg.ConnID)
		dataConn.Close()
		return
	}

	s.log.Infow("bridging connection", "conn_id", msg.ConnID)

	if len(pc.peeked) > 0 {
		if _, err := dataConn.Write(pc.peeked); err != nil {
			s.log.Errorw("write peeked bytes failed", "conn_id", msg.ConnID, "err", err)
			pc.conn.Close()
			dataConn.Close()
			return
		}
	}

	bridge(pc.conn, dataConn)
	s.log.Debugw("connection closed", "conn_id", msg.ConnID)
}

func (s *Server) listenHTTP() {
	ln, err := net.Listen("tcp", s.cfg.HTTPAddr)
	if err != nil {
		s.log.Errorw("http listen failed", "addr", s.cfg.HTTPAddr, "err", err)
		return
	}
	s.log.Infow("http listening", "addr", s.cfg.HTTPAddr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			s.log.Errorw("http accept failed", "err", err)
			continue
		}
		go s.handleHTTP(conn)
	}
}

func (s *Server) handleHTTP(conn net.Conn) {
	result, err := httpparse.PeekHost(conn)
	if err != nil {
		s.log.Warnw("peek host failed", "remote", conn.RemoteAddr(), "err", err)
		conn.Close()
		return
	}

	s.mu.Lock()
	tunnelID, ok := s.hosts[result.Host]
	ctrl, ctrlOk := s.tunnels[tunnelID]
	s.mu.Unlock()

	if !ok || !ctrlOk {
		s.log.Warnw("no tunnel for host", "host", result.Host, "remote", conn.RemoteAddr())
		conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
		conn.Close()
		return
	}

	s.sendConnect(conn, result.Peeked, tunnelID, ctrl)
}

func (s *Server) listenTLS() {
	cert, err := tlscerts.LoadOrSelfSigned(
		s.cfg.TLSCertFile,
		s.cfg.TLSKeyFile,
		s.cfg.TLSDomain,
		"*."+s.cfg.TLSDomain,
	)
	if err != nil {
		s.log.Errorw("load tls cert failed", "err", err)
		return
	}

	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := tls.Listen("tcp", s.cfg.TLSAddr, tlsCfg)
	if err != nil {
		s.log.Errorw("tls listen failed", "addr", s.cfg.TLSAddr, "err", err)
		return
	}
	s.log.Infow("tls listening", "addr", s.cfg.TLSAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.log.Errorw("tls accept failed", "err", err)
			continue
		}
		go s.handleTLS(conn)
	}
}

func (s *Server) handleTLS(conn net.Conn) {
	tlsConn := conn.(*tls.Conn)
	if err := tlsConn.Handshake(); err != nil {
		s.log.Warnw("tls handshake failed", "remote", conn.RemoteAddr(), "err", err)
		conn.Close()
		return
	}

	host := tlsConn.ConnectionState().ServerName
	if host == "" {
		s.log.Warnw("no SNI in tls handshake", "remote", conn.RemoteAddr())
		conn.Close()
		return
	}

	s.mu.Lock()
	tunnelID, ok := s.hosts[host]
	ctrl, ctrlOk := s.tunnels[tunnelID]
	s.mu.Unlock()

	if !ok || !ctrlOk {
		s.log.Warnw("no tunnel for tls host", "host", host, "remote", conn.RemoteAddr())
		conn.Close()
		return
	}

	s.log.Infow("new tls connection",
		"host", host,
		"tunnel_id", tunnelID,
		"remote", conn.RemoteAddr(),
	)

	s.sendConnect(conn, nil, tunnelID, ctrl)
}

func (s *Server) sendConnect(conn net.Conn, peeked []byte, tunnelID string, ctrl net.Conn) {
	connID := uuid.NewString()

	s.mu.Lock()
	s.pending[connID] = pendingConn{conn: conn, peeked: peeked}
	s.mu.Unlock()

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

func (s *Server) listenPublic(tunnelID string, port int, ctrl net.Conn) {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.log.Errorw("public listen failed", "addr", addr, "tunnel_id", tunnelID, "err", err)
		return
	}
	defer ln.Close()
	s.log.Infow("public listening", "addr", addr, "tunnel_id", tunnelID)

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.log.Errorw("public accept failed", "tunnel_id", tunnelID, "err", err)
			return
		}
		connID := uuid.NewString()

		s.mu.Lock()
		s.pending[connID] = pendingConn{conn: conn}
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
			s.log.Errorw("write connect failed", "tunnel_id", tunnelID, "conn_id", connID, "err", err)
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
