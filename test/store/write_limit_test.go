package store_test

import (
	"sync"
	"testing"
	"time"
	"xrockscache/internal/store"
)

func TestWriteLimiterSharesBudget(t *testing.T) {
	l := store.NewWriteLimiter(1024 * 1024)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Wait(128 * 1024); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if time.Since(start) < 490*time.Millisecond {
		t.Fatal("concurrent writers bypassed shared budget")
	}
}

func TestWriteLimiterRejectsOversizedReservation(t *testing.T) {
	l := store.NewWriteLimiter(1024)
	if err := l.Wait(1025); err == nil {
		t.Fatal("unbounded wait accepted")
	}
	if err := l.Wait(1); err != nil {
		t.Fatal("rejected request consumed budget", err)
	}
}
