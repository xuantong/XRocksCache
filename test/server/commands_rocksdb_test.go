//go:build rocksdb && cgo

package server_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"xrockscache/internal/config"
	"xrockscache/internal/server"
	"xrockscache/internal/store"
)

func newTestServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	kv, err := store.OpenWithOptions(dir, store.Options{
		Expiration: store.ExpirationConfig{Enabled: false},
		Tuning: store.BuildRocksDBTuning(store.ResourceProfile{
			CPUCores:      2,
			MemoryBytes:   4 * 1024 * 1024 * 1024,
			DiskFreeBytes: 125 * 1024 * 1024 * 1024,
		}),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kv.Close() })

	cfg := config.Default()
	cfg.Bind = "127.0.0.1"
	cfg.Port = ln.Addr().(*net.TCPAddr).Port
	cfg.Dir = dir

	srv := server.New(cfg, kv, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server stopped with error: %v", err)
			}
		case <-time.After(time.Second):
			t.Errorf("server did not stop")
		}
	})

	return ln.Addr().String()
}

func runCommand(t *testing.T, addr string, args ...string) string {
	t.Helper()

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}

	var req bytes.Buffer
	fmt.Fprintf(&req, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&req, "$%d\r\n%s\r\n", len(arg), arg)
	}
	if _, err := conn.Write(req.Bytes()); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(line, "$") && line != "$-1\r\n" {
		var n int
		if _, err := fmt.Sscanf(line, "$%d\r\n", &n); err != nil {
			t.Fatal(err)
		}
		payload := make([]byte, n+2)
		if _, err := io.ReadFull(reader, payload); err != nil {
			t.Fatal(err)
		}
		return line + string(payload)
	}
	return line
}

func TestStringCommands(t *testing.T) {
	addr := newTestServer(t)
	if got := runCommand(t, addr, "SET", "hello", "world", "EX", "60"); got != "+OK\r\n" {
		t.Fatalf("SET = %q", got)
	}
	if got := runCommand(t, addr, "GET", "hello"); got != "$5\r\nworld\r\n" {
		t.Fatalf("GET = %q", got)
	}
}

func TestTTLUpperBound(t *testing.T) {
	addr := newTestServer(t)
	got := runCommand(t, addr, "SET", "k", "v", "EX", "1296001")
	if !strings.Contains(got, "ttl exceeds 15 days") {
		t.Fatalf("expected ttl limit error, got %q", got)
	}
}

func TestValueUpperBound(t *testing.T) {
	addr := newTestServer(t)
	tooLarge := strings.Repeat("v", store.MaxValueBytes+1)
	got := runCommand(t, addr, "SET", "k", tooLarge)
	if !strings.Contains(got, "value exceeds 1MiB") {
		t.Fatalf("expected value limit error, got %q", got)
	}
}
