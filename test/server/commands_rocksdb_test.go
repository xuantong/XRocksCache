package server_test

import (
	"bufio"
	"bytes"
	"context"
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
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
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

func TestExpiredCounterCommands(t *testing.T) {
	addr := newTestServer(t)
	for _, command := range []string{"INCR", "DECR", "INCRBY", "DECRBY"} {
		if got := runCommand(t, addr, "SET", command, "41", "PX", "1"); got != "+OK\r\n" {
			t.Fatal(got)
		}
		time.Sleep(10 * time.Millisecond)
		args := []string{command, command}
		if strings.HasSuffix(command, "BY") {
			args = append(args, "1")
		}
		want := "1"
		if strings.HasPrefix(command, "DECR") {
			want = "-1"
		}
		if got := runCommand(t, addr, args...); got != ":"+want+"\r\n" {
			t.Fatalf("%s: %q", command, got)
		}
		if got := runCommand(t, addr, "GET", command); got != fmt.Sprintf("$%d\r\n%s\r\n", len(want), want) {
			t.Fatalf("GET after %s: %q", command, got)
		}
	}
}

func TestTTLUpperBound(t *testing.T) {
	addr := newTestServer(t)
	got := runCommand(t, addr, "SET", "k", "v", "EX", "1296001")
	if !strings.Contains(got, "ttl exceeds 15 days") {
		t.Fatalf("expected ttl limit error, got %q", got)
	}
}

func TestValueAndKeyBoundaries(t *testing.T) {
	addr := newTestServer(t)
	key := strings.Repeat("k", 512*1024)
	value := strings.Repeat("v", 5*1024*1024)
	if got := runCommand(t, addr, "SET", key, value); got != "+OK\r\n" {
		t.Fatalf("SET at boundary: %q", got)
	}
	if got := runCommand(t, addr, "GET", key); got != fmt.Sprintf("$%d\r\n%s\r\n", len(value), value) {
		t.Fatal("GET at boundary did not return the original value")
	}
	if got := runCommand(t, addr, "SET", key+"k", "v"); !strings.Contains(got, "key exceeds 512KiB") {
		t.Fatalf("expected unchanged key limit: %q", got)
	}
}

func TestValueUpperBound(t *testing.T) {
	addr := newTestServer(t)
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	// 只发送长度声明，验证分配和读取负载之前就被拒绝。
	fmt.Fprintf(conn, "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$%d\r\n", store.MaxValueBytes+1)
	got, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "invalid bulk string length") {
		t.Fatalf("expected value limit error, got %q", got)
	}
}

func TestNumericAndTTLEdges(t *testing.T) {
	addr := newTestServer(t)
	for _, args := range [][]string{
		{"SET", "k", "v", "EX", "288230376151711744"},
		{"SET", "k", "v", "PX", "9223372036854775807"},
		{"SET", "k", "v", "NX", "XX"},
		{"SET", "k", "v", "KEEPTTL", "EX", "1"},
		{"DECRBY", "k", "-9223372036854775808"},
	} {
		if got := runCommand(t, addr, args...); !strings.HasPrefix(got, "-ERR") {
			t.Fatalf("%v: %q", args, got)
		}
	}
	if got := runCommand(t, addr, "SET", "k", "v"); got != "+OK\r\n" {
		t.Fatal(got)
	}
	if got := runCommand(t, addr, "EXPIRE", "k", "0"); got != ":1\r\n" {
		t.Fatal(got)
	}
	if got := runCommand(t, addr, "GET", "k"); got != "$-1\r\n" {
		t.Fatal(got)
	}
}

func TestCommandArgumentAndUnknownCommandErrors(t *testing.T) {
	addr := newTestServer(t)
	for _, args := range [][]string{
		{"GET"}, {"SET", "k"}, {"MGET"}, {"EXPIRE", "k"}, {"NOT_A_COMMAND"},
	} {
		if got := runCommand(t, addr, args...); !strings.HasPrefix(got, "-ERR") {
			t.Fatalf("%v accepted: %q", args, got)
		}
	}
}
