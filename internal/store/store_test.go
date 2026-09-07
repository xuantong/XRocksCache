package store

import (
	"bytes"
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
