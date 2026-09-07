package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuantong/XRocksCache/internal/config"
	"github.com/xuantong/XRocksCache/internal/resp"
	"github.com/xuantong/XRocksCache/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	kv, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kv.Close() })
	return New(config.Default(), kv, "test")
}

func runCommand(t *testing.T, srv *Server, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	w := resp.NewWriter(&buf)
	authed := true
	srv.execute(w, args, &authed)
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestStringCommands(t *testing.T) {
	srv := newTestServer(t)
	if got := runCommand(t, srv, "SET", "hello", "world", "EX", "60"); got != "+OK\r\n" {
		t.Fatalf("SET = %q", got)
	}
	if got := runCommand(t, srv, "GET", "hello"); got != "$5\r\nworld\r\n" {
		t.Fatalf("GET = %q", got)
	}
	if got := runCommand(t, srv, "DBSIZE"); got != ":1\r\n" {
		t.Fatalf("DBSIZE = %q", got)
	}
}

func TestTTLUpperBound(t *testing.T) {
	srv := newTestServer(t)
	got := runCommand(t, srv, "SET", "k", "v", "EX", "1296001")
	if !strings.Contains(got, "ttl exceeds 15 days") {
		t.Fatalf("expected ttl limit error, got %q", got)
	}
}

func TestValueUpperBound(t *testing.T) {
	srv := newTestServer(t)
	tooLarge := strings.Repeat("v", store.MaxValueBytes+1)
	got := runCommand(t, srv, "SET", "k", tooLarge)
	if !strings.Contains(got, "value exceeds 1MiB") {
		t.Fatalf("expected value limit error, got %q", got)
	}
}
