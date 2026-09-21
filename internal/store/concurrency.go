package store

import (
	"fmt"
	"time"
)

// lockKeys 按固定顺序获取去重后的分片锁，避免多 key 命令相互死锁。
// 锁表大小固定，不随磁盘 key 数量增长。
func (s *Store) lockKeys(keys []string) func() {
	var used [256]bool
	for _, key := range keys {
		var hash uint32 = 2166136261
		for i := 0; i < len(key); i++ {
			hash = (hash ^ uint32(key[i])) * 16777619
		}
		used[hash%uint32(len(used))] = true
	}
	for i, yes := range used {
		if yes {
			s.stripes[i].Lock()
		}
	}
	return func() {
		for i := len(used) - 1; i >= 0; i-- {
			if used[i] {
				s.stripes[i].Unlock()
			}
		}
	}
}

// MGet 在批量写入之间读取完整的一组结果；所有错误在发送 RESP 数组前返回。
func (s *Store) MGet(keys []string) ([][]byte, []bool, error) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return nil, nil, fmt.Errorf("ERR store closed")
	}
	unlock := s.lockKeys(keys)
	defer unlock()
	values := make([][]byte, len(keys))
	found := make([]bool, len(keys))
	now := time.Now()
	total := 0
	for i, key := range keys {
		value, _, exists, err := s.getWithExpireAt(key, now)
		if err != nil {
			return nil, nil, err
		}
		total += len(value)
		if total > 16*1024*1024 {
			return nil, nil, fmt.Errorf("ERR MGET response exceeds 16MiB")
		}
		values[i], found[i] = value, exists
	}
	return values, found, nil
}
