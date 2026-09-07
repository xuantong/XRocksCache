package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xuantong/XRocksCache/internal/config"
	"github.com/xuantong/XRocksCache/internal/resp"
	"github.com/xuantong/XRocksCache/internal/store"
)

type Server struct {
	cfg     config.Config
	store   *store.Store
	version string
	active  atomic.Int64
	started time.Time
}

func New(cfg config.Config, kv *store.Store, version string) *Server {
	return &Server{cfg: cfg, store: kv, version: version, started: time.Now()}
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf("xrockscache %s listening on %s, dir=%s\n", s.version, addr, s.cfg.Dir)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		if s.cfg.MaxClients > 0 && int(s.active.Load()) >= s.cfg.MaxClients {
			_ = conn.Close()
			continue
		}
		s.active.Add(1)
		go func() {
			defer s.active.Add(-1)
			s.handleConn(conn)
		}()
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := resp.NewReader(conn)
	writer := resp.NewWriter(conn)
	authenticated := s.cfg.RequirePass == ""

	for {
		args, err := reader.ReadCommand()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				_ = writer.Error("ERR " + err.Error())
				_ = writer.Flush()
			}
			return
		}
		if len(args) == 0 {
			continue
		}
		cmd := strings.ToUpper(args[0])
		if !authenticated && cmd != "AUTH" && cmd != "HELLO" {
			_ = writer.Error("NOAUTH Authentication required.")
			_ = writer.Flush()
			continue
		}
		quit := s.execute(writer, args, &authenticated)
		if err := writer.Flush(); err != nil {
			return
		}
		if quit {
			return
		}
	}
}
