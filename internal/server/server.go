package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xrockscache/internal/config"
	"xrockscache/internal/resp"
	"xrockscache/internal/store"
)

type Server struct {
	cfg         config.Config
	store       *store.Store
	version     string
	logger      *slog.Logger
	active      atomic.Int64
	started     time.Time
	mu          sync.Mutex
	listener    net.Listener
	connections map[net.Conn]struct{}
	stopping    bool
	wg          sync.WaitGroup
	work        chan struct{}
}

func New(cfg config.Config, kv *store.Store, version string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, store: kv, version: version, logger: logger, started: time.Now(), connections: make(map[net.Conn]struct{}), work: make(chan struct{}, max(1, min(cfg.Workers, 32)))}
}

func (s *Server) ListenAndServe() error {
	addr := net.JoinHostPort(s.cfg.Bind, fmt.Sprint(s.cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

func (s *Server) Serve(ln net.Listener) error {
	defer ln.Close()
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return nil
	}
	s.listener = ln
	s.mu.Unlock()
	addr := ln.Addr().String()
	s.logger.Info("server listening", "version", s.version, "addr", addr, "dir", s.cfg.Dir)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if s.cfg.MaxClients > 0 && int(s.active.Load()) >= s.cfg.MaxClients {
			s.logger.Warn("reject connection because maxclients reached", "maxclients", s.cfg.MaxClients, "remote_addr", conn.RemoteAddr().String())
			_ = conn.Close()
			continue
		}
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			_ = conn.Close()
			return nil
		}
		s.connections[conn] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		s.active.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock() }()
			defer s.active.Add(-1)
			s.handleConn(conn)
		}()
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := resp.NewReader(conn)
	defer reader.Close()
	writer := resp.NewWriter(conn)
	authenticated := s.cfg.RequirePass == ""

	for {
		s.mu.Lock()
		stopping := s.stopping
		if !stopping {
			_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		}
		s.mu.Unlock()
		if stopping {
			return
		}
		args, err := reader.ReadCommand()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.logger.Warn("read command failed", "remote_addr", conn.RemoteAddr().String(), "error", err)
				_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
				_ = writer.Error("ERR " + err.Error())
				_ = writer.Flush()
			}
			return
		}
		if len(args) == 0 {
			continue
		}
		cmd := strings.ToUpper(args[0])
		_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if !authenticated && cmd != "AUTH" && cmd != "HELLO" {
			_ = writer.Error("NOAUTH Authentication required.")
			_ = writer.Flush()
			continue
		}
		// 限制同时执行的命令，批量响应和 cgo 临时内存不会随连接数无限增长。
		s.work <- struct{}{}
		quit := s.execute(writer, args, &authenticated)
		<-s.work
		if err := writer.Flush(); err != nil {
			return
		}
		if quit {
			return
		}
	}
}

// Shutdown 停止接收新连接并等待在途命令；超过期限后关闭剩余连接。
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.stopping = true
	if s.listener != nil {
		_ = s.listener.Close()
	}
	for conn := range s.connections {
		_ = conn.SetReadDeadline(time.Now())
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		for conn := range s.connections {
			_ = conn.Close()
		}
		s.mu.Unlock()
		return ctx.Err()
	}
}
