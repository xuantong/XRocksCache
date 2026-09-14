package store

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	MaxKeyBytes   = 512 * 1024
	MaxValueBytes = 1024 * 1024
	MaxTTL        = 15 * 24 * time.Hour

	recordSet byte = 'S'
	recordDel byte = 'D'
)

type Entry struct {
	Value     []byte
	ExpiresAt int64
}

type SetOptions struct {
	TTL     time.Duration
	Mode    string
	Get     bool
	KeepTTL bool
}

type Options struct {
	Expiration ExpirationConfig
	Logger     *slog.Logger
}

type ExpirationConfig struct {
	Enabled            bool
	BucketSize         time.Duration
	Interval           time.Duration
	CycleBudget        time.Duration
	MaxDeletesPerCycle int
}

func DefaultExpirationConfig() ExpirationConfig {
	return ExpirationConfig{
		Enabled:            true,
		BucketSize:         30 * time.Second,
		Interval:           10 * time.Second,
		CycleBudget:        10 * time.Millisecond,
		MaxDeletesPerCycle: 1000,
	}
}

type Store struct {
	mu            sync.RWMutex
	items         map[string]Entry
	expireIndex   map[string]int64
	expireBuckets map[int64]map[string]struct{}
	file          *os.File
	path          string

	expiration      ExpirationConfig
	logger          *slog.Logger
	cleanerStop     chan struct{}
	cleanerDone     chan struct{}
	cleanerStopOnce sync.Once
}

func Open(dir string) (*Store, error) {
	return OpenWithOptions(dir, Options{Expiration: DefaultExpirationConfig()})
}

func OpenWithOptions(dir string, opts Options) (*Store, error) {
	if dir == "" {
		dir = "data"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "xrockscache.aof")
	expiration := opts.Expiration
	if expiration.BucketSize <= 0 {
		expiration.BucketSize = 30 * time.Second
	}
	if expiration.Interval <= 0 {
		expiration.Interval = 10 * time.Second
	}
	if expiration.CycleBudget <= 0 {
		expiration.CycleBudget = 10 * time.Millisecond
	}
	if expiration.MaxDeletesPerCycle <= 0 {
		expiration.MaxDeletesPerCycle = 1000
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	s := &Store{
		items:         map[string]Entry{},
		expireIndex:   map[string]int64{},
		expireBuckets: map[int64]map[string]struct{}{},
		path:          path,
		expiration:    expiration,
		logger:        logger,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	s.rebuildExpireIndex(time.Now())
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	s.file = file
	s.startExpirationCleaner()
	return s, nil
}

func (s *Store) Close() error {
	if s.cleanerStop != nil {
		s.cleanerStopOnce.Do(func() {
			close(s.cleanerStop)
			<-s.cleanerDone
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	return s.file.Close()
}

func (s *Store) load() error {
	file, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	r := bufio.NewReaderSize(file, 1024*1024)
	for {
		var size uint32
		if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if size == 0 || size > MaxKeyBytes+MaxValueBytes+32 {
			return fmt.Errorf("invalid record size %d", size)
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		if err := s.applyRecord(payload, false); err != nil {
			return err
		}
	}
}

func (s *Store) applyRecord(payload []byte, validate bool) error {
	if len(payload) < 17 {
		return fmt.Errorf("short record")
	}
	op := payload[0]
	expiresAt := int64(binary.LittleEndian.Uint64(payload[1:9]))
	keyLen := int(binary.LittleEndian.Uint32(payload[9:13]))
	valueLen := int(binary.LittleEndian.Uint32(payload[13:17]))
	if keyLen < 0 || valueLen < 0 || 17+keyLen+valueLen != len(payload) {
		return fmt.Errorf("corrupt record")
	}
	key := string(payload[17 : 17+keyLen])
	value := payload[17+keyLen:]
	if validate {
		if err := validateKeyValue(key, value); err != nil {
			return err
		}
	}
	switch op {
	case recordSet:
		copied := append([]byte(nil), value...)
		s.items[key] = Entry{Value: copied, ExpiresAt: expiresAt}
	case recordDel:
		delete(s.items, key)
	default:
		return fmt.Errorf("unknown record op %q", op)
	}
	return nil
}

func (s *Store) rebuildExpireIndex(now time.Time) {
	nowMs := now.UnixMilli()
	for key, entry := range s.items {
		if entry.ExpiresAt > 0 && entry.ExpiresAt <= nowMs {
			delete(s.items, key)
			continue
		}
		s.indexExpirationLocked(key, entry.ExpiresAt)
	}
}

func (s *Store) appendRecordLocked(op byte, key string, value []byte, expiresAt int64) error {
	payloadSize := 17 + len(key) + len(value)
	payload := make([]byte, payloadSize)
	payload[0] = op
	binary.LittleEndian.PutUint64(payload[1:9], uint64(expiresAt))
	binary.LittleEndian.PutUint32(payload[9:13], uint32(len(key)))
	binary.LittleEndian.PutUint32(payload[13:17], uint32(len(value)))
	copy(payload[17:], key)
	copy(payload[17+len(key):], value)

	if err := binary.Write(s.file, binary.LittleEndian, uint32(payloadSize)); err != nil {
		return err
	}
	_, err := s.file.Write(payload)
	return err
}

func validateKeyValue(key string, value []byte) error {
	if key == "" {
		return fmt.Errorf("ERR key must not be empty")
	}
	if len(key) > MaxKeyBytes {
		return fmt.Errorf("ERR key exceeds 512KiB")
	}
	if len(value) > MaxValueBytes {
		return fmt.Errorf("ERR value exceeds 1MiB")
	}
	return nil
}

func (s *Store) expirationBucketLocked(expiresAt int64) int64 {
	bucketMs := s.expiration.BucketSize.Milliseconds()
	if bucketMs <= 0 {
		bucketMs = int64((30 * time.Second).Milliseconds())
	}
	if expiresAt <= 0 {
		return 0
	}
	return ((expiresAt + bucketMs - 1) / bucketMs) * bucketMs
}

func (s *Store) indexExpirationLocked(key string, expiresAt int64) {
	s.removeExpirationIndexLocked(key)
	if expiresAt <= 0 {
		return
	}
	bucket := s.expirationBucketLocked(expiresAt)
	keys := s.expireBuckets[bucket]
	if keys == nil {
		keys = map[string]struct{}{}
		s.expireBuckets[bucket] = keys
	}
	keys[key] = struct{}{}
	s.expireIndex[key] = bucket
}

func (s *Store) removeExpirationIndexLocked(key string) {
	bucket, ok := s.expireIndex[key]
	if !ok {
		return
	}
	delete(s.expireIndex, key)
	if keys := s.expireBuckets[bucket]; keys != nil {
		delete(keys, key)
		if len(keys) == 0 {
			delete(s.expireBuckets, bucket)
		}
	}
}

func (s *Store) setEntryLocked(key string, value []byte, expiresAt int64) {
	s.items[key] = Entry{Value: append([]byte(nil), value...), ExpiresAt: expiresAt}
	s.indexExpirationLocked(key, expiresAt)
}

func (s *Store) deleteEntryLocked(key string) {
	delete(s.items, key)
	s.removeExpirationIndexLocked(key)
}

func (s *Store) startExpirationCleaner() {
	if !s.expiration.Enabled {
		return
	}
	s.cleanerStop = make(chan struct{})
	s.cleanerDone = make(chan struct{})
	go s.expirationCleaner()
}

func (s *Store) expirationCleaner() {
	defer close(s.cleanerDone)
	ticker := time.NewTicker(s.expiration.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			stats := s.cleanExpiredCycle(time.Now())
			if stats.deleted > 0 {
				s.logger.Debug("active expiration cycle finished",
					"deleted", stats.deleted,
					"scanned", stats.scanned,
					"remaining_due_buckets", stats.remainingDueBuckets,
				)
			}
		case <-s.cleanerStop:
			return
		}
	}
}

type expireCycleStats struct {
	scanned             int
	deleted             int
	remainingDueBuckets int
}

func (s *Store) cleanExpiredCycle(now time.Time) expireCycleStats {
	start := time.Now()
	nowMs := now.UnixMilli()
	stats := expireCycleStats{}

	s.mu.Lock()
	defer s.mu.Unlock()

	for bucket, keys := range s.expireBuckets {
		if bucket > nowMs {
			continue
		}
		for key := range keys {
			stats.scanned++
			entry, ok := s.items[key]
			indexedBucket, indexed := s.expireIndex[key]
			if !ok || !indexed || indexedBucket != bucket || entry.ExpiresAt == 0 {
				delete(keys, key)
			} else if entry.ExpiresAt <= nowMs {
				if err := s.appendRecordLocked(recordDel, key, nil, 0); err != nil {
					s.logger.Error("persist expired key deletion failed", "key", key, "error", err)
					return stats
				}
				s.deleteEntryLocked(key)
				stats.deleted++
			}
			if s.expiration.MaxDeletesPerCycle > 0 && stats.deleted >= s.expiration.MaxDeletesPerCycle {
				return s.finishExpireCycleStatsLocked(stats)
			}
			if s.expiration.CycleBudget > 0 && time.Since(start) >= s.expiration.CycleBudget {
				return s.finishExpireCycleStatsLocked(stats)
			}
		}
		if len(keys) == 0 {
			delete(s.expireBuckets, bucket)
		} else {
			stats.remainingDueBuckets++
		}
	}
	return stats
}

func (s *Store) finishExpireCycleStatsLocked(stats expireCycleStats) expireCycleStats {
	nowMs := time.Now().UnixMilli()
	for bucket, keys := range s.expireBuckets {
		if bucket <= nowMs && len(keys) > 0 {
			stats.remainingDueBuckets++
		}
	}
	return stats
}

func validateTTL(ttl time.Duration) error {
	if ttl < 0 {
		return fmt.Errorf("ERR invalid expire time")
	}
	if ttl > MaxTTL {
		return fmt.Errorf("ERR ttl exceeds 15 days")
	}
	return nil
}

func (s *Store) Set(key string, value []byte, opts SetOptions) ([]byte, bool, error) {
	if err := validateKeyValue(key, value); err != nil {
		return nil, false, err
	}
	if err := validateTTL(opts.TTL); err != nil {
		return nil, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	old, exists := s.getLocked(key, time.Now())
	switch opts.Mode {
	case "NX":
		if exists {
			return old, false, nil
		}
	case "XX":
		if !exists {
			return nil, false, nil
		}
	}

	expiresAt := int64(0)
	if opts.KeepTTL && exists {
		expiresAt = s.items[key].ExpiresAt
	} else if opts.TTL > 0 {
		expiresAt = time.Now().Add(opts.TTL).UnixMilli()
	}
	if err := s.appendRecordLocked(recordSet, key, value, expiresAt); err != nil {
		return old, false, err
	}
	s.setEntryLocked(key, value, expiresAt)
	return old, true, nil
}

func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, exists := s.getLocked(key, time.Now())
	return value, exists
}

func (s *Store) getLocked(key string, now time.Time) ([]byte, bool) {
	entry, ok := s.items[key]
	if !ok {
		return nil, false
	}
	if entry.ExpiresAt > 0 && entry.ExpiresAt <= now.UnixMilli() {
		s.deleteEntryLocked(key)
		_ = s.appendRecordLocked(recordDel, key, nil, 0)
		return nil, false
	}
	return append([]byte(nil), entry.Value...), true
}

func (s *Store) MSet(pairs map[string][]byte) error {
	for key, value := range pairs {
		if err := validateKeyValue(key, value); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range pairs {
		if err := s.appendRecordLocked(recordSet, key, value, 0); err != nil {
			return err
		}
		s.setEntryLocked(key, value, 0)
	}
	return nil
}

func (s *Store) Del(keys ...string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed int64
	now := time.Now()
	for _, key := range keys {
		if _, exists := s.getLocked(key, now); exists {
			if err := s.appendRecordLocked(recordDel, key, nil, 0); err != nil {
				return removed, err
			}
			s.deleteEntryLocked(key)
			removed++
		}
	}
	return removed, nil
}

func (s *Store) Exists(keys ...string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	now := time.Now()
	for _, key := range keys {
		if _, exists := s.getLocked(key, now); exists {
			count++
		}
	}
	return count
}

func (s *Store) Expire(key string, ttl time.Duration) (bool, error) {
	if err := validateTTL(ttl); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, exists := s.getLocked(key, time.Now())
	if !exists {
		return false, nil
	}
	expiresAt := time.Now().Add(ttl).UnixMilli()
	if err := s.appendRecordLocked(recordSet, key, value, expiresAt); err != nil {
		return false, err
	}
	s.setEntryLocked(key, value, expiresAt)
	return true, nil
}

func (s *Store) TTL(key string) (time.Duration, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.getLocked(key, time.Now())
	if !exists {
		return 0, false, false
	}
	expiresAt := s.items[key].ExpiresAt
	if expiresAt == 0 {
		return 0, true, false
	}
	ttl := time.Until(time.UnixMilli(expiresAt))
	if ttl < 0 {
		s.deleteEntryLocked(key)
		_ = s.appendRecordLocked(recordDel, key, nil, 0)
		return 0, false, false
	}
	return ttl, true, true
}

func (s *Store) DBSize() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var count int64
	for key := range s.items {
		if _, exists := s.getLocked(key, now); exists {
			count++
		}
	}
	return count
}

func (s *Store) IncrBy(key string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.getLocked(key, time.Now())
	var current int64
	if exists {
		v, err := strconv.ParseInt(string(old), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("ERR value is not an integer or out of range")
		}
		current = v
	}
	next := current + delta
	expiresAt := int64(0)
	if exists {
		expiresAt = s.items[key].ExpiresAt
	}
	value := []byte(strconv.FormatInt(next, 10))
	if err := s.appendRecordLocked(recordSet, key, value, expiresAt); err != nil {
		return 0, err
	}
	s.setEntryLocked(key, value, expiresAt)
	return next, nil
}

func (s *Store) Stats() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]string{
		"keys":                            strconv.FormatInt(int64(len(s.items)), 10),
		"expires":                         strconv.FormatInt(int64(len(s.expireIndex)), 10),
		"expire_buckets":                  strconv.FormatInt(int64(len(s.expireBuckets)), 10),
		"active_expire_enabled":           strconv.FormatBool(s.expiration.Enabled),
		"active_expire_bucket_seconds":    strconv.FormatInt(int64(s.expiration.BucketSize/time.Second), 10),
		"active_expire_interval_seconds":  strconv.FormatInt(int64(s.expiration.Interval/time.Second), 10),
		"active_expire_cycle_budget_ms":   strconv.FormatInt(int64(s.expiration.CycleBudget/time.Millisecond), 10),
		"active_expire_max_deletes_cycle": strconv.Itoa(s.expiration.MaxDeletesPerCycle),
		"max_key":                         strconv.Itoa(MaxKeyBytes),
		"max_value":                       strconv.Itoa(MaxValueBytes),
		"max_ttl_sec":                     strconv.FormatInt(int64(MaxTTL/time.Second), 10),
		"aof_path":                        s.path,
	}
}
