package server

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/dlc-01/relayd/internal/auth"
	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/httpparse"
	"github.com/dlc-01/relayd/internal/logger"
	"github.com/dlc-01/relayd/internal/notify"
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

type clientSession struct {
	id        string
	ctrl      net.Conn
	tunnels   []proto.TunnelDef
	listeners []net.Listener
}

type Server struct {
	cfg         config.ServerConfig
	log         *zap.SugaredLogger
	mu          sync.Mutex
	pending     map[string]pendingConn
	ports       map[int]string
	hosts       map[string]string
	tunnels     map[string]net.Conn
	sessions    map[string]*clientSession
	auth        *auth.Manager
	controlCert tls.Certificate
	notify      notify.Notifier
}

func New(cfg config.ServerConfig) *Server {
	var log *zap.SugaredLogger
	if cfg.Dev {
		log = logger.NewDev("server")
	} else {
		log = logger.New("server")
	}

	var mgr *auth.Manager
	if cfg.MasterToken != "" {
		var err error
		mgr, err = auth.NewManager(cfg.MasterToken, cfg.SessionTTL)
		if err != nil {
			log.Fatalw("auth manager failed", "err", err)
		}
		log.Infow("auth enabled", "session_ttl", cfg.SessionTTL)
	} else {
		log.Warnw("auth disabled — set RELAYD_TOKEN to enable")
	}

	cert, err := tlscerts.LoadOrGenerate(cfg.ControlCertFile, cfg.ControlKeyFile)
	if err != nil {
		log.Fatalw("load control cert failed", "err", err)
	}
	fp, _ := tlscerts.Fingerprint(cert)
	log.Infow("control tls ready", "fingerprint", fp)

	return &Server{
		cfg:         cfg,
		log:         log,
		pending:     make(map[string]pendingConn),
		ports:       make(map[int]string),
		hosts:       make(map[string]string),
		tunnels:     make(map[string]net.Conn),
		sessions:    make(map[string]*clientSession),
		auth:        mgr,
		controlCert: cert,
		notify:      buildNotifier(cfg),
	}
}

func buildNotifier(cfg config.ServerConfig) notify.Notifier {
	var notifiers []notify.Notifier

	if cfg.TGToken != "" && cfg.TGChatID != "" {
		notifiers = append(notifiers, notify.NewTelegram(cfg.TGToken, cfg.TGChatID))
	}

	if len(notifiers) == 0 {
		return &notify.NoopNotifier{}
	}

	return notify.NewMulti(notifiers...)
}

func (s *Server) Run() {
	s.notify.ServerStarted(s.cfg.ControlAddr)
	go s.listenControl()
	go s.listenData()
	go s.listenHTTP()
	go s.listenTLS()
	select {}
}

func (s *Server) listenControl() {
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{s.controlCert}}
	ln, err := tls.Listen("tcp", s.cfg.ControlAddr, tlsCfg)
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

func (s *Server) authAndIssue(token, remote string) (tempToken string, expiresAt string, err error) {
	if s.auth == nil {
		return "", "", nil
	}

	entry, err := s.auth.Validate(token)
	if err != nil {
		s.notify.InvalidToken(remote)
		return "", "", err
	}

	if entry.IsMaster() {
		temp, err := s.auth.IssueForMaster(remote)
		if err != nil {
			return "", "", fmt.Errorf("issue temp token: %w", err)
		}
		s.log.Infow("issued temp token",
			"remote", remote,
			"expires_at", temp.ExpiresAt,
		)
		return temp.Token, temp.ExpiresAt.Format(time.RFC3339), nil
	}

	s.log.Infow("auth ok", "remote", remote, "label", entry.Label)
	return "", "", nil
}

func (s *Server) handleControl(ctrl net.Conn) {
	defer ctrl.Close()
	remote := ctrl.RemoteAddr().String()

	msg, err := proto.Read(ctrl)
	if err != nil || msg.Type != proto.TypeRegister {
		s.log.Warnw("expected register", "remote", remote, "got", msg.Type, "err", err)
		return
	}

	tempToken, expiresAt, err := s.authAndIssue(msg.Token, remote)
	if err != nil {
		s.log.Warnw("auth failed", "remote", remote, "err", err)
		proto.Write(ctrl, proto.Message{
			Type:   proto.TypeError,
			Reason: err.Error(),
		})
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
		if t.PublicPort == 0 && t.Host == "" && len(t.Hosts) == 0 {
			proto.Write(ctrl, proto.Message{
				Type:   proto.TypeError,
				Reason: fmt.Sprintf("tunnel %s must have public_port or host", t.TunnelID),
			})
			return
		}
	}

	s.mu.Lock()
	for _, t := range msg.Tunnels {
		if _, ok := s.tunnels[t.TunnelID]; ok {
			s.mu.Unlock()
			proto.Write(ctrl, proto.Message{
				Type:   proto.TypeError,
				Reason: fmt.Sprintf("tunnel_id %s already in use", t.TunnelID),
			})
			return
		}
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
		for _, h := range t.Hosts {
			if existing, ok := s.hosts[h]; ok {
				s.mu.Unlock()
				proto.Write(ctrl, proto.Message{
					Type:   proto.TypeError,
					Reason: fmt.Sprintf("host %s already in use by tunnel %s", h, existing),
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

	session := &clientSession{
		id:      uuid.NewString(),
		ctrl:    ctrl,
		tunnels: msg.Tunnels,
	}

	s.mu.Lock()
	s.sessions[session.id] = session
	for _, t := range msg.Tunnels {
		s.tunnels[t.TunnelID] = ctrl
		if t.PublicPort != 0 {
			s.ports[t.PublicPort] = t.TunnelID
		}
		if t.Host != "" {
			s.hosts[t.Host] = t.TunnelID
		}
		for _, h := range t.Hosts {
			s.hosts[h] = t.TunnelID
		}
		s.log.Infow("tunnel registered",
			"remote", remote,
			"session_id", session.id,
			"tunnel_id", t.TunnelID,
			"public_port", t.PublicPort,
			"host", t.Host,
			"aliases", t.Hosts,
		)
	}
	s.mu.Unlock()

	okMsg := proto.Message{Type: proto.TypeOK}
	if tempToken != "" {
		okMsg.TempToken = tempToken
		okMsg.ExpiresAt = expiresAt
	}
	if err := proto.Write(ctrl, okMsg); err != nil {
		s.log.Errorw("write ok failed", "remote", remote, "err", err)
		s.removeSession(session)
		return
	}

	for _, t := range msg.Tunnels {
		if t.PublicPort != 0 {
			if ln := s.listenPublic(t.TunnelID, t.PublicPort, ctrl); ln != nil {
				s.mu.Lock()
				session.listeners = append(session.listeners, ln)
				s.mu.Unlock()
			}
		}
	}

	s.log.Infow("client connected",
		"remote", remote,
		"session_id", session.id,
		"tunnels", len(msg.Tunnels),
	)

	tunnelNames := tunnelNamesFromDefs(msg.Tunnels)
	s.notify.ClientConnected(remote, tunnelNames, remote)
	s.readControlLoop(ctrl, session)

	s.log.Infow("client disconnected",
		"remote", remote,
		"session_id", session.id,
	)

	s.notify.ClientDisconnected(remote, remote)
	s.removeSession(session)
}

func tunnelNamesFromDefs(tunnels []proto.TunnelDef) string {
	names := make([]string, 0, len(tunnels))
	for _, t := range tunnels {
		if t.Host != "" {
			names = append(names, t.Host)
		} else {
			names = append(names, fmt.Sprintf(":%d", t.PublicPort))
		}
	}
	return strings.Join(names, ", ")
}

func (s *Server) readControlLoop(ctrl net.Conn, session *clientSession) {
	for {
		msg, err := proto.Read(ctrl)
		if err != nil {
			return
		}
		switch msg.Type {
		case proto.TypePing:
			s.log.Debugw("ping received", "session_id", session.id)
			if err := proto.Write(ctrl, proto.Message{Type: proto.TypePong}); err != nil {
				s.log.Warnw("pong failed", "session_id", session.id, "err", err)
				return
			}
		default:
			s.log.Warnw("unexpected control msg",
				"session_id", session.id,
				"type", msg.Type,
			)
		}
	}
}

func (s *Server) removeSession(session *clientSession) {
	for _, ln := range session.listeners {
		ln.Close()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, session.id)
	for _, t := range session.tunnels {
		delete(s.tunnels, t.TunnelID)
		if t.PublicPort != 0 {
			delete(s.ports, t.PublicPort)
		}
		if t.Host != "" {
			delete(s.hosts, t.Host)
		}
		for _, h := range t.Hosts {
			delete(s.hosts, h)
		}
		s.log.Infow("tunnel unregistered",
			"session_id", session.id,
			"tunnel_id", t.TunnelID,
		)
	}
}

func (s *Server) listenData() {
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{s.controlCert}}
	ln, err := tls.Listen("tcp", s.cfg.DataAddr, tlsCfg)
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
	var extraDomains []struct{ Domain, CertFile, KeyFile string }
	for _, d := range s.cfg.TLSDomains {
		extraDomains = append(extraDomains, struct{ Domain, CertFile, KeyFile string }{
			Domain:   d.Domain,
			CertFile: d.CertFile,
			KeyFile:  d.KeyFile,
		})
	}

	tlsCfg, err := tlscerts.BuildTLSConfig(
		s.cfg.TLSCertFile,
		s.cfg.TLSKeyFile,
		s.cfg.TLSDomain,
		extraDomains,
	)
	if err != nil {
		s.log.Errorw("build tls config failed", "err", err)
		return
	}

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

	go func() {
		time.Sleep(s.cfg.PendingTimeout)
		s.mu.Lock()
		pc, ok := s.pending[connID]
		if ok {
			delete(s.pending, connID)
			s.log.Warnw("pending connection timeout",
				"conn_id", connID,
				"tunnel_id", tunnelID,
			)
			pc.conn.Close()
		}
		s.mu.Unlock()
	}()

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

func (s *Server) listenPublic(tunnelID string, port int, ctrl net.Conn) net.Listener {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.log.Errorw("public listen failed", "addr", addr, "tunnel_id", tunnelID, "err", err)
		return nil
	}
	s.log.Infow("public listening", "addr", addr, "tunnel_id", tunnelID)

	go func() {
		defer ln.Close()
		for {
			conn, err := ln.Accept()
			if err != nil {
				s.log.Debugw("public listener closed", "tunnel_id", tunnelID)
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
	}()

	return ln
}

func (s *Server) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func bridge(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}
