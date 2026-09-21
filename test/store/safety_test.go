package store_test

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
	"xrockscache/internal/store"
)

func TestUncleanProcessWALRecovery(t *testing.T) {
	if dir := os.Getenv("XRC_TEST_CRASH_DIR"); dir != "" {
		s, err := store.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Set("acknowledged", []byte("value"), store.SetOptions{}); err != nil {
			t.Fatal(err)
		}
		// 有意绕过 Close，验证正常确认后的 WAL 能在进程中断后恢复。
		os.Exit(0)
	}
	dir := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestUncleanProcessWALRecovery$")
	command.Env = append(os.Environ(), "XRC_TEST_CRASH_DIR="+dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("child failed: %v %s", err, output)
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, found, err := s.GetWithError("acknowledged")
	if err != nil || !found || string(got) != "value" {
		t.Fatalf("WAL recovery: %q %v %v", got, found, err)
	}
}

func TestDefaultTTLAndOverflow(t *testing.T) {
	s := newRocksDBTestStore(t)
	if _, _, err := s.Set("set", []byte("v"), store.SetOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.MSet(map[string][]byte{"mset": []byte("v")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IncrBy("incr", 1); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"set", "mset", "incr"} {
		ttl, exists, hasTTL, err := s.TTLWithError(key)
		if err != nil || !exists || !hasTTL || ttl > store.MaxTTL || ttl < store.MaxTTL-time.Minute {
			t.Fatalf("%s: %s %v", key, ttl, err)
		}
	}
	for _, pair := range [][2]int64{{math.MaxInt64, 1}, {math.MinInt64, -1}} {
		old := fmt.Sprint(pair[0])
		if _, _, err := s.Set("n", []byte(old), store.SetOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.IncrBy("n", pair[1]); err == nil {
			t.Fatal("overflow accepted")
		}
		got, _, err := s.GetWithError("n")
		if err != nil || string(got) != old {
			t.Fatalf("overflow modified value: %q %v", got, err)
		}
	}
}

func TestIncrementRecreatesExpiredKey(t *testing.T) {
	s := newRocksDBTestStore(t)
	for _, delta := range []int64{1, -1, 0} {
		key := fmt.Sprintf("expired-counter-%d", delta)
		if _, _, err := s.Set(key, []byte("41"), store.SetOptions{TTL: time.Millisecond}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		if _, found, err := s.GetWithError(key); err != nil || found {
			t.Fatalf("expired key visible: %v %v", found, err)
		}
		if got, err := s.IncrBy(key, delta); err != nil || got != delta {
			t.Fatalf("increment: %d %v", got, err)
		}
		if got, found, err := s.GetWithError(key); err != nil || !found || string(got) != fmt.Sprint(delta) {
			t.Fatalf("recreated value: %q %v %v", got, found, err)
		}
		ttl, found, hasTTL, err := s.TTLWithError(key)
		if err != nil || !found || !hasTTL || ttl < store.MaxTTL-time.Minute || ttl > store.MaxTTL {
			t.Fatalf("recreated TTL: %v %v %v %v", ttl, found, hasTTL, err)
		}
	}
}

func TestIncrementPreservesLiveTTL(t *testing.T) {
	s := newRocksDBTestStore(t)
	if _, _, err := s.Set("live-counter", []byte("41"), store.SetOptions{TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	before, _, _, _ := s.TTLWithError("live-counter")
	if got, err := s.IncrBy("live-counter", 1); err != nil || got != 42 {
		t.Fatalf("increment: %d %v", got, err)
	}
	after, found, hasTTL, err := s.TTLWithError("live-counter")
	if err != nil || !found || !hasTTL || after > before || after < before-time.Minute {
		t.Fatalf("live TTL changed: before=%v after=%v error=%v", before, after, err)
	}
}

func TestConcurrentIncrementAndBatchSnapshot(t *testing.T) {
	s := newRocksDBTestStore(t)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if _, err := s.IncrBy("counter", 1); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	got, _, err := s.GetWithError("counter")
	if err != nil || string(got) != "800" {
		t.Fatalf("counter=%q %v", got, err)
	}
	if err := s.MSet(map[string][]byte{"a": []byte("0"), "b": []byte("0")}); err != nil {
		t.Fatal(err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			value := []byte(fmt.Sprint(i))
			if err := s.MSet(map[string][]byte{"a": value, "b": value}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i := 0; i < 300; i++ {
		values, _, err := s.MGet([]string{"a", "b"})
		if err != nil {
			t.Fatal(err)
		}
		if string(values[0]) != string(values[1]) {
			t.Errorf("torn batch: %q", values)
		}
	}
	wg.Wait()
}

func TestCloseWithConcurrentOperations(t *testing.T) {
	s := newRocksDBTestStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _, _ = s.Set("k", []byte("v"), store.SetOptions{})
				_, _, _ = s.GetWithError("k")
			}
		}()
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if _, _, err := s.GetWithError("k"); err == nil {
		t.Fatal("closed store accepted read")
	}
}

func TestBudgetStableAcrossRestartAndSmallMemory(t *testing.T) {
	before := store.BuildRocksDBTuning(store.ResourceProfile{CPUCores: 2, MemoryBytes: 4 * testGiB, DiskFreeBytes: 125 * testGiB})
	after := store.BuildRocksDBTuning(store.ResourceProfile{CPUCores: 2, MemoryBytes: 4 * testGiB, DiskFreeBytes: 75 * testGiB, DiskUsedBytes: 50 * testGiB})
	if before.DiskBudgetBytes != after.DiskBudgetBytes {
		t.Fatal("restart changed budget")
	}
	small := store.BuildRocksDBTuning(store.ResourceProfile{CPUCores: 1, MemoryBytes: 256 * testMiB, DiskFreeBytes: testGiB})
	if small.MemoryBudgetBytes > 256*testMiB || small.DiskBudgetBytes > testGiB {
		t.Fatal("budget exceeds resources")
	}
}

func TestTuningHandlesUnknownAndLargeResources(t *testing.T) {
	unknown := store.BuildRocksDBTuning(store.ResourceProfile{})
	if unknown.MemoryBudgetBytes == 0 || unknown.DiskBudgetBytes == 0 {
		t.Fatal("unknown resources produced zero budgets")
	}
	large := store.BuildRocksDBTuning(store.ResourceProfile{
		CPUCores: 64, MemoryBytes: math.MaxUint64, DiskFreeBytes: math.MaxUint64,
	})
	if large.MemoryBudgetBytes == 0 || large.DiskBudgetBytes == 0 || large.MaxBackgroundJobs > 3 {
		t.Fatalf("large resources produced invalid tuning: %+v", large)
	}
}
