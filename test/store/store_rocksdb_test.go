package store_test

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
	"time"

	"xrockscache/internal/store"
)

func newRocksDBTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenWithOptions(t.TempDir(), store.Options{
		Expiration: store.ExpirationConfig{Enabled: false},
		Tuning: store.BuildRocksDBTuning(store.ResourceProfile{
			CPUCores:      2,
			MemoryBytes:   4 * testGiB,
			DiskFreeBytes: 125 * testGiB,
		}),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRocksDBSetGetAndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := store.OpenWithOptions(dir, store.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, stored, err := s.Set("hello", []byte("world"), store.SetOptions{}); err != nil || !stored {
		t.Fatalf("Set() stored=%v err=%v", stored, err)
	}
	if got, ok := s.Get("hello"); !ok || string(got) != "world" {
		t.Fatalf("Get() = %q, %v", got, ok)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.OpenWithOptions(dir, store.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, ok := reopened.Get("hello"); !ok || string(got) != "world" {
		t.Fatalf("reopened Get() = %q, %v", got, ok)
	}
}

func TestRocksDBTTLInvisibleAfterReload(t *testing.T) {
	dir := t.TempDir()
	s, err := store.OpenWithOptions(dir, store.Options{
		Expiration: store.ExpirationConfig{Enabled: false},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, stored, err := s.Set("ttl-key", []byte("ttl-value"), store.SetOptions{TTL: 20 * time.Millisecond}); err != nil || !stored {
		t.Fatalf("Set() stored=%v err=%v", stored, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(80 * time.Millisecond)
	reopened, err := store.OpenWithOptions(dir, store.Options{
		Expiration: store.ExpirationConfig{Enabled: false},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	if value, ok := reopened.Get("ttl-key"); ok {
		t.Fatalf("expired key should be invisible after reload, got %q", value)
	}
	if ttl, exists, hasTTL := reopened.TTL("ttl-key"); exists || hasTTL || ttl != 0 {
		t.Fatalf("TTL() for expired key = ttl=%s exists=%v hasTTL=%v", ttl, exists, hasTTL)
	}
}

func TestRocksDBTTLLimit(t *testing.T) {
	s := newRocksDBTestStore(t)
	if _, _, err := s.Set("k", []byte("v"), store.SetOptions{TTL: store.MaxTTL + time.Millisecond}); err == nil {
		t.Fatal("expected TTL limit error")
	}
}

func TestRocksDBSizeLimits(t *testing.T) {
	s := newRocksDBTestStore(t)
	key := string(bytes.Repeat([]byte("k"), store.MaxKeyBytes))
	value := bytes.Repeat([]byte("v"), 5*1024*1024)
	if _, _, err := s.Set(key, value, store.SetOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, found, err := s.GetWithError(key); err != nil || !found || !bytes.Equal(got, value) {
		t.Fatalf("boundary roundtrip: %v", err)
	}
	if _, _, err := s.Set(string(bytes.Repeat([]byte("k"), store.MaxKeyBytes+1)), []byte("v"), store.SetOptions{}); err == nil {
		t.Fatal("expected key size error")
	}
	if _, _, err := s.Set("k", bytes.Repeat([]byte("v"), store.MaxValueBytes+1), store.SetOptions{}); err == nil {
		t.Fatal("expected value size error")
	}
}

func TestRocksDBSetConditionModes(t *testing.T) {
	s := newRocksDBTestStore(t)

	if old, stored, err := s.Set("k", []byte("v1"), store.SetOptions{Mode: "NX"}); err != nil || !stored || old != nil {
		t.Fatalf("SET NX first = old=%q stored=%v err=%v", old, stored, err)
	}
	if old, stored, err := s.Set("k", []byte("v2"), store.SetOptions{Mode: "NX", Get: true}); err != nil || stored || string(old) != "v1" {
		t.Fatalf("SET NX GET existing = old=%q stored=%v err=%v", old, stored, err)
	}
	if old, stored, err := s.Set("missing", []byte("v"), store.SetOptions{Mode: "XX"}); err != nil || stored || old != nil {
		t.Fatalf("SET XX missing = old=%q stored=%v err=%v", old, stored, err)
	}
	if old, stored, err := s.Set("k", []byte("v3"), store.SetOptions{Mode: "XX", Get: true}); err != nil || !stored || string(old) != "v1" {
		t.Fatalf("SET XX GET existing = old=%q stored=%v err=%v", old, stored, err)
	}
	if got, ok := s.Get("k"); !ok || string(got) != "v3" {
		t.Fatalf("Get() after SET XX = %q, %v", got, ok)
	}
}

func TestRocksDBKeepTTL(t *testing.T) {
	s := newRocksDBTestStore(t)

	if _, stored, err := s.Set("k", []byte("v1"), store.SetOptions{TTL: time.Second}); err != nil || !stored {
		t.Fatalf("initial Set() stored=%v err=%v", stored, err)
	}
	before, exists, hasTTL := s.TTL("k")
	if !exists || !hasTTL || before <= 0 {
		t.Fatalf("TTL before KEEPTTL = %s exists=%v hasTTL=%v", before, exists, hasTTL)
	}
	if _, stored, err := s.Set("k", []byte("v2"), store.SetOptions{KeepTTL: true}); err != nil || !stored {
		t.Fatalf("KEEPTTL Set() stored=%v err=%v", stored, err)
	}
	after, exists, hasTTL := s.TTL("k")
	if !exists || !hasTTL || after <= 0 {
		t.Fatalf("TTL after KEEPTTL = %s exists=%v hasTTL=%v", after, exists, hasTTL)
	}
	if after > before {
		t.Fatalf("KEEPTTL should not extend ttl: before=%s after=%s", before, after)
	}
}

func TestRocksDBLargeValueAboveBlobThreshold(t *testing.T) {
	s := newRocksDBTestStore(t)
	value := bytes.Repeat([]byte("x"), int(32*testKiB))

	if _, stored, err := s.Set("blob-sized", value, store.SetOptions{}); err != nil || !stored {
		t.Fatalf("Set() stored=%v err=%v", stored, err)
	}
	got, ok := s.Get("blob-sized")
	if !ok || !bytes.Equal(got, value) {
		t.Fatalf("large value round trip failed: len=%d ok=%v", len(got), ok)
	}
	stats := s.Stats()
	if stats["rocksdb_blob_files_enabled"] != "true" {
		t.Fatalf("rocksdb_blob_files_enabled = %q, want true", stats["rocksdb_blob_files_enabled"])
	}
	if stats["rocksdb_min_blob_size_bytes"] != "16384" {
		t.Fatalf("rocksdb_min_blob_size_bytes = %q, want 16384", stats["rocksdb_min_blob_size_bytes"])
	}
}

func TestRocksDBInfoStatsShape(t *testing.T) {
	s := newRocksDBTestStore(t)
	stats := s.Stats()

	if stats["aof_path"] != "" {
		t.Fatalf("rocksdb build should not expose AOF path, got %q", stats["aof_path"])
	}
	if stats["rocksdb_path"] == "" {
		t.Fatalf("expected rocksdb_path in stats: %#v", stats)
	}
	if stats["rocksdb_compression"] != "lz4" {
		t.Fatalf("rocksdb_compression = %q, want lz4", stats["rocksdb_compression"])
	}
	if stats["rocksdb_pending_compaction_bytes"] == "" {
		t.Fatalf("expected rocksdb_pending_compaction_bytes in stats: %#v", stats)
	}
	for _, key := range []string{"rocksdb_immutable_memtables", "rocksdb_num_running_compactions", "rocksdb_num_running_flushes", "rocksdb_memtable_bytes", "rocksdb_l0_files"} {
		if stats[key] == "" || stats[key] == "unavailable" {
			t.Fatalf("missing diagnostic property %s: %q", key, stats[key])
		}
	}
}

func TestRocksDBRejectWatermarkRejectsAllWrites(t *testing.T) {
	dir := t.TempDir()
	normal := store.BuildRocksDBTuning(store.ResourceProfile{
		CPUCores:      2,
		MemoryBytes:   4 * testGiB,
		DiskFreeBytes: 125 * testGiB,
	})
	s, err := store.OpenWithOptions(dir, store.Options{
		Expiration: store.ExpirationConfig{Enabled: false},
		Tuning:     normal,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, stored, err := s.Set("existing", []byte("old"), store.SetOptions{}); err != nil || !stored {
		t.Fatalf("initial Set() stored=%v err=%v", stored, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	rejecting := normal
	rejecting.DiskWarnWatermarkBytes = 1
	rejecting.DiskSlowdownWatermarkBytes = 1
	rejecting.DiskRejectWatermarkBytes = 1
	reopened, err := store.OpenWithOptions(dir, store.Options{
		Expiration: store.ExpirationConfig{Enabled: false},
		Tuning:     rejecting,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	if _, stored, err := reopened.Set("new", []byte("value"), store.SetOptions{}); err == nil || stored {
		t.Fatalf("new key should be rejected above disk watermark: stored=%v err=%v", stored, err)
	}
	if _, stored, err := reopened.Set("existing", []byte("new"), store.SetOptions{}); err == nil || stored {
		t.Fatalf("overwrite should be rejected: stored=%v err=%v", stored, err)
	}
	got, ok := reopened.Get("existing")
	if _, err := reopened.IncrBy("counter", 1); err == nil {
		t.Fatal("INCR bypassed disk gate")
	}
	if err := reopened.MSet(map[string][]byte{"existing": []byte("v")}); err == nil {
		t.Fatal("MSET bypassed disk gate")
	}
	if _, err := reopened.Expire("existing", time.Hour); err == nil {
		t.Fatal("EXPIRE bypassed disk gate")
	}
	if !ok || string(got) != "old" {
		t.Fatalf("overwrite after reject watermark = %q, %v", got, ok)
	}
}
