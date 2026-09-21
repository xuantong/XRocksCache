package store_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
	"xrockscache/internal/store"
)

func TestBlobFlushAndExpiredCompaction(t *testing.T) {
	s := newRocksDBTestStore(t)
	value := bytes.Repeat([]byte("payload"), 8192)
	if _, _, err := s.Set("expired-blob", value, store.SetOptions{TTL: 2 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Set("live", []byte("alive"), store.SetOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	dir := s.Stats()["rocksdb_path"]
	blobs, err := filepath.Glob(filepath.Join(dir, "*.blob"))
	if err != nil || len(blobs) == 0 {
		t.Fatalf("no real Blob file: %v", err)
	}
	ssts, _ := filepath.Glob(filepath.Join(dir, "*.sst"))
	if len(ssts) == 0 {
		t.Fatal("no SST file")
	}
	if got, ok, err := s.GetWithError("expired-blob"); err != nil || !ok || !bytes.Equal(got, value) {
		t.Fatalf("disk read failed: %v %v", ok, err)
	}
	time.Sleep(3 * time.Second)
	if err := s.CompactRange(nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetWithError("expired-blob"); err != nil || ok {
		t.Fatalf("expired Blob visible: %v", err)
	}
	if got, ok, err := s.GetWithError("live"); err != nil || !ok || string(got) != "alive" {
		t.Fatalf("live data lost: %v", err)
	}
	if s.DBSize() != 1 {
		t.Fatalf("expired entry retained after compaction: %d", s.DBSize())
	}
	// 关闭后不存在迭代器或后台任务引用，旧 Blob 文件应已被回收。
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range blobs {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("obsolete Blob remains: %s (%v)", path, err)
		}
	}
}
