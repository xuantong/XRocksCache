package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"xrockscache/internal/config"
	"xrockscache/internal/resp"
	"xrockscache/internal/store"
)

type Server struct {
	cfg     config.Config
	store   *store.Store
	version string
	logger  *slog.Logger
	active  atomic.Int64
	started time.Time
}

func New(cfg config.Config, kv *store.Store, version string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, store: kv, version: version, logger: logger, started: time.Now()}
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.logger.Info("server listening", "version", s.version, "addr", addr, "dir", s.cfg.Dir)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		if s.cfg.MaxClients > 0 && int(s.active.Load()) >= s.cfg.MaxClients {
			s.logger.Warn("reject connection because maxclients reached", "maxclients", s.cfg.MaxClients, "remote_addr", conn.RemoteAddr().String())
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
				s.logger.Warn("read command failed", "remote_addr", conn.RemoteAddr().String(), "error", err)
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
