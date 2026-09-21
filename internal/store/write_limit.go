package store

import (
	"fmt"
	"sync"
	"time"
)

// WriteLimiter 将所有写入共享的字节预算均匀排期，最多等待一秒。
// 不积攒空闲令牌，避免恢复写入时产生突发流量。
type WriteLimiter struct {
	mu             sync.Mutex
	next           time.Time
	bytesPerSecond int64
}

func NewWriteLimiter(bytesPerSecond int64) *WriteLimiter {
	return &WriteLimiter{bytesPerSecond: bytesPerSecond}
}

func (l *WriteLimiter) Wait(bytes int64) error {
	if bytes < 0 || l.bytesPerSecond <= 0 {
		return fmt.Errorf("ERR invalid write budget")
	}
	if bytes > l.bytesPerSecond {
		return fmt.Errorf("ERR write rate limit exceeded")
	}
	duration := time.Duration(bytes * int64(time.Second) / l.bytesPerSecond)
	l.mu.Lock()
	now := time.Now()
	start := l.next
	if start.Before(now) {
		start = now
	}
	end := start.Add(duration)
	if end.Sub(now) > time.Second {
		l.mu.Unlock()
		return fmt.Errorf("ERR write rate limit exceeded")
	}
	l.next = end
	l.mu.Unlock()
	time.Sleep(time.Until(end))
	return nil
}
