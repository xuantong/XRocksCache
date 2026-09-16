//go:build !rocksdb || !cgo

package store

import (
	"fmt"
	"strconv"
	"time"
)

// Store 是未启用 RocksDB 生产构建时的占位实现。
// 旧的 AOF + 内存 Store 已删除，避免默认构建在行为上偏离“大容量 RocksDB 缓存”的核心目标。
type Store struct{}

func Open(dir string) (*Store, error) {
	return OpenWithOptions(dir, Options{Expiration: DefaultExpirationConfig()})
}

func OpenWithOptions(_ string, _ Options) (*Store, error) {
	return nil, fmt.Errorf("rocksdb store is required: build with CGO_ENABLED=1 and -tags rocksdb")
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) Set(_ string, _ []byte, _ SetOptions) ([]byte, bool, error) {
	return nil, false, fmt.Errorf("rocksdb store is required")
}

func (s *Store) Get(_ string) ([]byte, bool) {
	return nil, false
}

func (s *Store) MSet(_ map[string][]byte) error {
	return fmt.Errorf("rocksdb store is required")
}

func (s *Store) Del(_ ...string) (int64, error) {
	return 0, fmt.Errorf("rocksdb store is required")
}

func (s *Store) Exists(_ ...string) int64 {
	return 0
}

func (s *Store) Expire(_ string, _ time.Duration) (bool, error) {
	return false, fmt.Errorf("rocksdb store is required")
}

func (s *Store) TTL(_ string) (time.Duration, bool, bool) {
	return 0, false, false
}

func (s *Store) DBSize() int64 {
	return 0
}

func (s *Store) IncrBy(_ string, _ int64) (int64, error) {
	return 0, fmt.Errorf("rocksdb store is required")
}

func (s *Store) Stats() map[string]string {
	tuning := BuildRocksDBTuning(ResourceProfile{})
	return map[string]string{
		"keys":                             "0",
		"expires":                          "0",
		"expire_buckets":                   "0",
		"active_expire_enabled":            "false",
		"active_expire_bucket_seconds":     "0",
		"active_expire_interval_seconds":   "0",
		"active_expire_cycle_budget_ms":    "0",
		"active_expire_max_deletes_cycle":  "0",
		"max_key":                          strconv.Itoa(MaxKeyBytes),
		"max_value":                        strconv.Itoa(MaxValueBytes),
		"max_ttl_sec":                      strconv.FormatInt(int64(MaxTTL/time.Second), 10),
		"aof_path":                         "",
		"rocksdb_path":                     "",
		"rocksdb_estimate_live_data_size":  "0",
		"rocksdb_pending_compaction_bytes": "0",
		"rocksdb_auto_tuned":               strconv.FormatBool(tuning.AutoTuned),
		"rocksdb_compression":              tuning.Compression,
		"rocksdb_disk_budget_bytes":        strconv.FormatUint(tuning.DiskBudgetBytes, 10),
		"rocksdb_memory_budget_bytes":      strconv.FormatUint(tuning.MemoryBudgetBytes, 10),
		"rocksdb_block_cache_bytes":        strconv.FormatUint(tuning.BlockCacheSizeBytes, 10),
		"rocksdb_write_buffer_bytes":       strconv.FormatUint(tuning.WriteBufferSizeBytes, 10),
		"rocksdb_target_file_size_bytes":   strconv.FormatUint(tuning.TargetFileSizeBaseBytes, 10),
		"rocksdb_blob_files_enabled":       strconv.FormatBool(tuning.EnableBlobFiles),
		"rocksdb_min_blob_size_bytes":      strconv.FormatUint(tuning.MinBlobSizeBytes, 10),
		"rocksdb_blob_file_size_bytes":     strconv.FormatUint(tuning.BlobFileSizeBytes, 10),
		"rocksdb_blob_gc_enabled":          strconv.FormatBool(tuning.EnableBlobGarbageCollection),
		"rocksdb_max_background_jobs":      strconv.Itoa(tuning.MaxBackgroundJobs),
		"rocksdb_max_subcompactions":       strconv.Itoa(tuning.MaxSubcompactions),
		"rocksdb_soft_pending_bytes":       strconv.FormatUint(tuning.SoftPendingCompactionBytesLimit, 10),
		"rocksdb_hard_pending_bytes":       strconv.FormatUint(tuning.HardPendingCompactionBytesLimit, 10),
		"rocksdb_rate_limiter_bytes_sec":   strconv.FormatUint(tuning.RateLimiterBytesPerSec, 10),
		"rocksdb_periodic_compaction_sec":  strconv.FormatInt(tuning.PeriodicCompactionSeconds, 10),
		"disk_warn_watermark_bytes":        strconv.FormatUint(tuning.DiskWarnWatermarkBytes, 10),
		"disk_slowdown_watermark_bytes":    strconv.FormatUint(tuning.DiskSlowdownWatermarkBytes, 10),
		"disk_reject_watermark_bytes":      strconv.FormatUint(tuning.DiskRejectWatermarkBytes, 10),
	}
}
