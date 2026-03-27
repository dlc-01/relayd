package server

import (
	"io"
	"log"
	"net"
	"sync"

	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/proto"
	"github.com/google/uuid"
)

type Server struct {
	cfg     config.ServerConfig
	mu      sync.Mutex
	pending map[string]net.Conn
}

func New(cfg config.ServerConfig) *Server {
	return &Server{
		cfg:     cfg,
		pending: make(map[string]net.Conn),
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
		log.Fatalf("control listen: %v", err)
	}
	log.Printf("control listening on %s", s.cfg.ControlAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("control accept: %v", err)
			continue
		}
		go s.handleControl(conn)
	}
}

func (s *Server) handleControl(ctrl net.Conn) {
	defer ctrl.Close()

	msg, err := proto.Read(ctrl)
	if err != nil || msg.Type != proto.TypeRegister {
		log.Printf("expected register, got: %v %v", msg, err)
		return
	}

	log.Printf("client registered tunnel: %s", msg.TunnelID)

	if err := proto.Write(ctrl, proto.Message{Type: proto.TypeOK}); err != nil {
		log.Printf("write ok: %v", err)
		return
	}

	s.listenPublic(ctrl)
}

func (s *Server) listenData() {
	ln, err := net.Listen("tcp", s.cfg.DataAddr)
	if err != nil {
		log.Fatalf("data listen: %v", err)
	}
	log.Printf("data listening on %s", s.cfg.DataAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("data accept: %v", err)
			continue
		}
		go s.handleData(conn)
	}
}

func (s *Server) handleData(dataConn net.Conn) {
	msg, err := proto.Read(dataConn)
	if err != nil || msg.Type != proto.TypeData {
		log.Printf("expected data msg: %v %v", msg, err)
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
		log.Printf("unknown connID: %s", msg.ConnID)
		dataConn.Close()
		return
	}

	log.Printf("bridging connID=%s", msg.ConnID)
	bridge(extConn, dataConn)
}

func (s *Server) listenPublic(ctrl net.Conn) {
	ln, err := net.Listen("tcp", s.cfg.PublicAddr)
	if err != nil {
		log.Fatalf("public listen: %v", err)
	}
	log.Printf("public listening on %s", s.cfg.PublicAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("public accept: %v", err)
			continue
		}
		connID := uuid.NewString()

		s.mu.Lock()
		s.pending[connID] = conn
		s.mu.Unlock()

		log.Printf("new external conn, connID=%s", connID)

		if err := proto.Write(ctrl, proto.Message{
			Type:   proto.TypeConnect,
			ConnID: connID,
		}); err != nil {
			log.Printf("write connect: %v", err)
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
