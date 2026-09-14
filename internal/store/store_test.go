package store

import (
	"bytes"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"
)

func TestStoreSetGetAndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, stored, err := s.Set("hello", []byte("world"), SetOptions{}); err != nil || !stored {
		t.Fatalf("Set() stored=%v err=%v", stored, err)
	}
	if got, ok := s.Get("hello"); !ok || string(got) != "world" {
		t.Fatalf("Get() = %q, %v", got, ok)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, ok := reopened.Get("hello"); !ok || string(got) != "world" {
		t.Fatalf("reopened Get() = %q, %v", got, ok)
	}
}

func TestTTLLimit(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, _, err := s.Set("k", []byte("v"), SetOptions{TTL: MaxTTL + time.Millisecond}); err == nil {
		t.Fatal("expected TTL limit error")
	}
}

func TestSizeLimits(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, _, err := s.Set(string(bytes.Repeat([]byte("k"), MaxKeyBytes+1)), []byte("v"), SetOptions{}); err == nil {
		t.Fatal("expected key size error")
	}
	if _, _, err := s.Set("k", bytes.Repeat([]byte("v"), MaxValueBytes+1), SetOptions{}); err == nil {
		t.Fatal("expected value size error")
	}
}

func TestActiveExpirationDeletesExpiredKeys(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithOptions(dir, Options{
		Expiration: ExpirationConfig{
			Enabled:            true,
			BucketSize:         10 * time.Millisecond,
			Interval:           10 * time.Millisecond,
			CycleBudget:        time.Second,
			MaxDeletesPerCycle: 100,
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, stored, err := s.Set("temp", []byte("value"), SetOptions{TTL: 20 * time.Millisecond}); err != nil || !stored {
		t.Fatalf("Set() stored=%v err=%v", stored, err)
	}
	waitForKeys(t, s, 0)

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenWithOptions(dir, Options{
		Expiration: ExpirationConfig{
			Enabled: false,
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, ok := reopened.Get("temp"); ok {
		t.Fatal("expired key survived active expiration and reload")
	}
}

func TestActiveExpirationDoesNotDeletePersistedRewrite(t *testing.T) {
	s, err := OpenWithOptions(t.TempDir(), Options{
		Expiration: ExpirationConfig{
			Enabled:            true,
			BucketSize:         10 * time.Millisecond,
			Interval:           10 * time.Millisecond,
			CycleBudget:        time.Second,
			MaxDeletesPerCycle: 100,
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, stored, err := s.Set("k", []byte("old"), SetOptions{TTL: 20 * time.Millisecond}); err != nil || !stored {
		t.Fatalf("Set() stored=%v err=%v", stored, err)
	}
	if _, stored, err := s.Set("k", []byte("new"), SetOptions{}); err != nil || !stored {
		t.Fatalf("rewrite Set() stored=%v err=%v", stored, err)
	}

	time.Sleep(100 * time.Millisecond)
	got, ok := s.Get("k")
	if !ok || string(got) != "new" {
		t.Fatalf("Get() after stale bucket cleanup = %q, %v", got, ok)
	}
}

func TestReloadSkipsExpiredRecords(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithOptions(dir, Options{
		Expiration: ExpirationConfig{
			Enabled: false,
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, stored, err := s.Set("expired", []byte("value"), SetOptions{TTL: 10 * time.Millisecond}); err != nil || !stored {
		t.Fatalf("Set() stored=%v err=%v", stored, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	reopened, err := OpenWithOptions(dir, Options{
		Expiration: ExpirationConfig{
			Enabled: false,
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stats := reopened.Stats()
	if stats["keys"] != "0" || stats["expires"] != "0" {
		t.Fatalf("Stats() after reload = keys %s expires %s", stats["keys"], stats["expires"])
	}
}

func waitForKeys(t *testing.T, s *Store, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		stats := s.Stats()
		got, err := strconv.Atoi(stats["keys"])
		if err != nil {
			t.Fatal(err)
		}
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for keys=%d, stats=%v", want, s.Stats())
}
