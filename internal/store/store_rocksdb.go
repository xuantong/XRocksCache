//go:build rocksdb && cgo

package store

/*
#cgo LDFLAGS: -lrocksdb
#include <rocksdb/c.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

// XRocksCache 的每个 RocksDB value 存储为：
//
//   0..3   魔数字节："XRC1"
//   4      记录版本：1
//   5      flags，预留给后续 value 编码变化
//   6..7   reserved，当前为 0
//   8..15  expiresAtMs，小端序；0 表示无 TTL
//   16..19 用户 value 长度，小端序
//   20..   用户 value 字节
//
// compaction filter 只需要前 16 字节即可判断 key 是否过期。
// 这段逻辑放在 C 中，是为了避免 compaction 期间回调 Go；
// 在高写入压力下，跨语言回调成本高且更脆弱。
static uint64_t xrc_read_le64(const char* p) {
  return ((uint64_t)(unsigned char)p[0]) |
         ((uint64_t)(unsigned char)p[1] << 8) |
         ((uint64_t)(unsigned char)p[2] << 16) |
         ((uint64_t)(unsigned char)p[3] << 24) |
         ((uint64_t)(unsigned char)p[4] << 32) |
         ((uint64_t)(unsigned char)p[5] << 40) |
         ((uint64_t)(unsigned char)p[6] << 48) |
         ((uint64_t)(unsigned char)p[7] << 56);
}

static unsigned char xrc_ttl_filter(
    void* state,
    int level,
    const char* key,
    size_t key_length,
    const char* existing_value,
    size_t value_length,
    char** new_value,
    size_t* new_value_length,
    unsigned char* value_changed) {
  (void)state;
  (void)level;
  (void)key;
  (void)key_length;
  (void)new_value;
  (void)new_value_length;

  *value_changed = 0;
  if (value_length < 20) {
    return 0;
  }
  if (existing_value[0] != 'X' || existing_value[1] != 'R' ||
      existing_value[2] != 'C' || existing_value[3] != '1') {
    return 0;
  }

  uint64_t expires_at_ms = xrc_read_le64(existing_value + 8);
  if (expires_at_ms == 0) {
    return 0;
  }

  uint64_t now_ms = (uint64_t)time(NULL) * 1000ULL;
  return expires_at_ms <= now_ms ? 1 : 0;
}

static const char* xrc_ttl_filter_name(void* state) {
  (void)state;
  return "xrockscache.ttl.compaction_filter";
}

static void xrc_ttl_filter_destroy(void* state) {
  (void)state;
}

static rocksdb_compactionfilter_t* xrc_create_ttl_filter() {
  return rocksdb_compactionfilter_create(
      NULL,
      xrc_ttl_filter_destroy,
      xrc_ttl_filter,
      xrc_ttl_filter_name);
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

const (
	// encodedHeaderBytes 刻意保持小且固定，使 C compaction filter 无需解码完整用户 value 即可判断 TTL 过期。
	encodedHeaderBytes = 20
	recordVersion      = byte(1)
)

var valueMagic = [4]byte{'X', 'R', 'C', '1'}

// Store 是默认构建即启用的真实 RocksDB 实现。
// 它刻意不保留完整内存 key/value 索引：数据集、WAL、SST、blob files 和 compaction 都由 RocksDB 负责。
type Store struct {
	// mu 刻意不作为全局数据库锁使用。
	// 它只保护 NX/XX/GET/KEEPTTL、EXPIRE、DEL、INCRBY 等命令层读改写语义；
	// 简单 GET/SET/MSET 会直接进入 RocksDB。
	mu     sync.Mutex
	dir    string
	tuning RocksDBTuning
	logger *slog.Logger

	// 磁盘用量会被采样并缓存。
	// 这样写路径可以执行业务水位线控制，同时避免每次 SET 都遍历可能很大的 RocksDB 目录。
	diskMu                sync.Mutex
	cachedDiskUsageBytes  uint64
	diskUsageCheckedAt    time.Time
	lastWatermarkLogLevel string
	lastWatermarkLoggedAt time.Time

	db               *C.rocksdb_t
	opts             *C.rocksdb_options_t
	readOpts         *C.rocksdb_readoptions_t
	writeOpts        *C.rocksdb_writeoptions_t
	compactionFilter *C.rocksdb_compactionfilter_t
	rateLimiter      *C.rocksdb_ratelimiter_t
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

	tuning := opts.Tuning
	if tuning.Compression == "" {
		tuning = BuildRocksDBTuning(DetectResourceProfile(dir))
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	dbPath := filepath.Join(dir, "rocksdb")
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		return nil, err
	}

	cPath := C.CString(dbPath)
	defer C.free(unsafe.Pointer(cPath))

	s := &Store{
		dir:       dbPath,
		tuning:    tuning,
		logger:    logger,
		opts:      C.rocksdb_options_create(),
		readOpts:  C.rocksdb_readoptions_create(),
		writeOpts: C.rocksdb_writeoptions_create(),
	}
	if s.opts == nil || s.readOpts == nil || s.writeOpts == nil {
		s.Close()
		return nil, fmt.Errorf("create rocksdb options failed")
	}

	// 所有 RocksDB 底层默认值都保留在代码里。
	// 公开配置应保持业务导向，这里只应用根据 CPU、内存和数据盘剩余空间计算出的自动调参方案。
	C.rocksdb_options_set_create_if_missing(s.opts, 1)
	// 先运行 RocksDB 的宽泛预设，再应用 XRocksCache 下方明确的自动调参值。
	// 这个顺序可以避免预设 helper 意外覆盖基于资源推导出的默认值。
	C.rocksdb_options_optimize_for_point_lookup(s.opts, C.uint64_t(tuning.BlockCacheSizeBytes/mib))
	C.rocksdb_options_optimize_level_style_compaction(s.opts, C.uint64_t(tuning.WriteBufferSizeBytes*uint64(tuning.MaxWriteBufferNumber)))
	C.rocksdb_options_set_compression(s.opts, C.rocksdb_lz4_compression)
	C.rocksdb_options_set_bottommost_compression(s.opts, C.rocksdb_lz4_compression)
	C.rocksdb_options_set_compaction_style(s.opts, C.rocksdb_level_compaction)
	C.rocksdb_options_set_write_buffer_size(s.opts, C.size_t(tuning.WriteBufferSizeBytes))
	C.rocksdb_options_set_max_write_buffer_number(s.opts, C.int(tuning.MaxWriteBufferNumber))
	C.rocksdb_options_set_min_write_buffer_number_to_merge(s.opts, C.int(tuning.MinWriteBufferNumberToMerge))
	C.rocksdb_options_set_target_file_size_base(s.opts, C.uint64_t(tuning.TargetFileSizeBaseBytes))
	C.rocksdb_options_set_max_background_jobs(s.opts, C.int(tuning.MaxBackgroundJobs))
	C.rocksdb_options_set_max_subcompactions(s.opts, C.uint32_t(tuning.MaxSubcompactions))
	C.rocksdb_options_set_soft_pending_compaction_bytes_limit(s.opts, C.size_t(tuning.SoftPendingCompactionBytesLimit))
	C.rocksdb_options_set_hard_pending_compaction_bytes_limit(s.opts, C.size_t(tuning.HardPendingCompactionBytesLimit))
	C.rocksdb_options_set_periodic_compaction_seconds(s.opts, C.uint64_t(tuning.PeriodicCompactionSeconds))

	if tuning.EnableBlobFiles {
		C.rocksdb_options_set_enable_blob_files(s.opts, 1)
		C.rocksdb_options_set_min_blob_size(s.opts, C.uint64_t(tuning.MinBlobSizeBytes))
		C.rocksdb_options_set_blob_file_size(s.opts, C.uint64_t(tuning.BlobFileSizeBytes))
		C.rocksdb_options_set_blob_compression_type(s.opts, C.rocksdb_lz4_compression)
		if tuning.EnableBlobGarbageCollection {
			C.rocksdb_options_set_enable_blob_gc(s.opts, 1)
			C.rocksdb_options_set_blob_gc_age_cutoff(s.opts, 0.25)
			C.rocksdb_options_set_blob_gc_force_threshold(s.opts, 0.5)
		}
	}

	if tuning.RateLimiterBytesPerSec > 0 {
		s.rateLimiter = C.rocksdb_ratelimiter_create(
			C.int64_t(tuning.RateLimiterBytesPerSec),
			C.int64_t(100*1000),
			C.int32_t(10),
		)
		C.rocksdb_options_set_ratelimiter(s.opts, s.rateLimiter)
	}

	// TTL compaction filter 负责延迟物理清理。
	// 读命令仍会解码 ExpiresAt 并立即隐藏过期 key，因此过期正确性不依赖 compaction 时机。
	s.compactionFilter = C.xrc_create_ttl_filter()
	C.rocksdb_compactionfilter_set_ignore_snapshots(s.compactionFilter, 1)
	C.rocksdb_options_set_compaction_filter(s.opts, s.compactionFilter)

	var cErr *C.char
	s.db = C.rocksdb_open(s.opts, cPath, &cErr)
	if err := takeRocksError(cErr); err != nil {
		s.Close()
		return nil, err
	}
	if s.db == nil {
		s.Close()
		return nil, fmt.Errorf("open rocksdb failed")
	}

	logger.Info("rocksdb store opened", "dir", dbPath, "tuning", tuning.String())
	return s, nil
}

func (s *Store) Close() error {
	// 显式且幂等地销毁 C 资源。
	// compaction filter 有自己的 no-op C 析构函数，因为 RocksDB C wrapper 会在清理时调用它，
	// 即使 XRocksCache 没有分配 per-filter state。
	if s.db != nil {
		C.rocksdb_close(s.db)
		s.db = nil
	}
	if s.readOpts != nil {
		C.rocksdb_readoptions_destroy(s.readOpts)
		s.readOpts = nil
	}
	if s.writeOpts != nil {
		C.rocksdb_writeoptions_destroy(s.writeOpts)
		s.writeOpts = nil
	}
	if s.opts != nil {
		C.rocksdb_options_destroy(s.opts)
		s.opts = nil
	}
	if s.compactionFilter != nil {
		C.rocksdb_compactionfilter_destroy(s.compactionFilter)
		s.compactionFilter = nil
	}
	if s.rateLimiter != nil {
		C.rocksdb_ratelimiter_destroy(s.rateLimiter)
		s.rateLimiter = nil
	}
	return nil
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

func validateTTL(ttl time.Duration) error {
	if ttl < 0 {
		return fmt.Errorf("ERR invalid expire time")
	}
	if ttl > MaxTTL {
		return fmt.Errorf("ERR ttl exceeds 15 days")
	}
	return nil
}

func encodeValue(value []byte, expiresAt int64) []byte {
	encoded := make([]byte, encodedHeaderBytes+len(value))
	copy(encoded[0:4], valueMagic[:])
	encoded[4] = recordVersion
	binary.LittleEndian.PutUint64(encoded[8:16], uint64(expiresAt))
	binary.LittleEndian.PutUint32(encoded[16:20], uint32(len(value)))
	copy(encoded[encodedHeaderBytes:], value)
	return encoded
}

func decodeValue(encoded []byte, now time.Time) ([]byte, int64, bool, error) {
	if len(encoded) < encodedHeaderBytes {
		return nil, 0, false, fmt.Errorf("corrupt value header")
	}
	if encoded[0] != valueMagic[0] || encoded[1] != valueMagic[1] || encoded[2] != valueMagic[2] || encoded[3] != valueMagic[3] {
		return nil, 0, false, fmt.Errorf("corrupt value magic")
	}
	if encoded[4] != recordVersion {
		return nil, 0, false, fmt.Errorf("unsupported value version %d", encoded[4])
	}
	expiresAt := int64(binary.LittleEndian.Uint64(encoded[8:16]))
	valueLen := int(binary.LittleEndian.Uint32(encoded[16:20]))
	if valueLen < 0 || encodedHeaderBytes+valueLen != len(encoded) {
		return nil, 0, false, fmt.Errorf("corrupt value length")
	}
	if expiresAt > 0 && expiresAt <= now.UnixMilli() {
		return nil, expiresAt, true, nil
	}
	return append([]byte(nil), encoded[encodedHeaderBytes:]...), expiresAt, false, nil
}

func (s *Store) Set(key string, value []byte, opts SetOptions) ([]byte, bool, error) {
	if err := validateKeyValue(key, value); err != nil {
		return nil, false, err
	}
	if err := validateTTL(opts.TTL); err != nil {
		return nil, false, err
	}

	now := time.Now()

	// 常见 SET 路径不需要读取旧值。
	// 对 XRocksCache 的写密集缓存负载来说，避免这次读很重要；
	// 因为 RocksDB 点查会增加 bloom/filter/index 工作，并可能在 compaction 下放大尾延迟。
	if opts.Mode == "" && !opts.Get && !opts.KeepTTL {
		expiresAt := int64(0)
		if opts.TTL > 0 {
			expiresAt = now.Add(opts.TTL).UnixMilli()
		}
		if err := s.ensureDiskWriteAllowed(key, nil); err != nil {
			return nil, false, err
		}
		return nil, true, s.put(key, encodeValue(value, expiresAt))
	}

	// 条件 SET、SET GET 和 KEEPTTL 是命令层读改写操作。
	// RocksDB 本身是线程安全的，但如果没有命令临界区，两个客户端可能同时看到相同旧状态，
	// 从而破坏 Redis-like NX/XX 语义。
	s.mu.Lock()
	defer s.mu.Unlock()

	old, oldExpiresAt, exists, err := s.getWithExpireAtLocked(key, now)
	if err != nil {
		return nil, false, err
	}
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
		expiresAt = oldExpiresAt
	} else if opts.TTL > 0 {
		expiresAt = now.Add(opts.TTL).UnixMilli()
	}
	if err := s.ensureDiskWriteAllowed(key, &exists); err != nil {
		return old, false, err
	}
	if err := s.putLocked(key, encodeValue(value, expiresAt)); err != nil {
		return old, false, err
	}
	return old, true, nil
}

func (s *Store) Get(key string) ([]byte, bool) {
	value, _, exists, err := s.getWithExpireAt(key, time.Now())
	if err != nil {
		s.logger.Warn("rocksdb get failed", "key", key, "error", err)
		return nil, false
	}
	return value, exists
}

func (s *Store) MSet(pairs map[string][]byte) error {
	for key, value := range pairs {
		if err := validateKeyValue(key, value); err != nil {
			return err
		}
	}
	if err := s.ensureMSetDiskWriteAllowed(pairs); err != nil {
		return err
	}

	batch := C.rocksdb_writebatch_create()
	if batch == nil {
		return fmt.Errorf("create rocksdb write batch failed")
	}
	defer C.rocksdb_writebatch_destroy(batch)

	for key, value := range pairs {
		cKey := C.CBytes([]byte(key))
		encoded := encodeValue(value, 0)
		cVal := C.CBytes(encoded)
		C.rocksdb_writebatch_put(batch, (*C.char)(cKey), C.size_t(len(key)), (*C.char)(cVal), C.size_t(len(encoded)))
		C.free(cKey)
		C.free(cVal)
	}

	var cErr *C.char
	C.rocksdb_write(s.db, s.writeOpts, batch, &cErr)
	return takeRocksError(cErr)
}

func (s *Store) Del(keys ...string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var removed int64
	now := time.Now()
	for _, key := range keys {
		_, _, exists, err := s.getWithExpireAtLocked(key, now)
		if err != nil {
			return removed, err
		}
		if !exists {
			continue
		}
		if err := s.deleteLocked(key); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func (s *Store) Exists(keys ...string) int64 {
	var count int64
	now := time.Now()
	for _, key := range keys {
		_, _, exists, err := s.getWithExpireAt(key, now)
		if err == nil && exists {
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

	value, _, exists, err := s.getWithExpireAtLocked(key, time.Now())
	if err != nil || !exists {
		return false, err
	}
	return true, s.putLocked(key, encodeValue(value, time.Now().Add(ttl).UnixMilli()))
}

func (s *Store) TTL(key string) (time.Duration, bool, bool) {
	_, expiresAt, exists, err := s.getWithExpireAt(key, time.Now())
	if err != nil || !exists {
		return 0, false, false
	}
	if expiresAt == 0 {
		return 0, true, false
	}
	ttl := time.Until(time.UnixMilli(expiresAt))
	if ttl <= 0 {
		_ = s.delete(key)
		return 0, false, false
	}
	return ttl, true, true
}

func (s *Store) DBSize() int64 {
	// 精确 DBSIZE 需要扫描整个 RocksDB keyspace，并解码每个 value 的 TTL。
	// 对 100G 缓存服务来说这是不可接受的，因此 RocksDB 构建返回引擎估算值。
	// INFO 暴露同一个估算值，便于运维把它视为 approximate。
	return int64(s.rocksPropertyInt("rocksdb.estimate-num-keys"))
}

func (s *Store) ensureMSetDiskWriteAllowed(pairs map[string][]byte) error {
	usage, level := s.currentDiskWatermark()
	s.logDiskWatermark(level, usage)
	switch level {
	case "reject":
		// reject 模式仍允许覆盖已有 key。
		// 替换现有缓存 value 不会扩大逻辑 keyspace；拒绝新 key 是为了保护主机磁盘不被打满。
		now := time.Now()
		for key := range pairs {
			_, _, exists, err := s.getWithExpireAt(key, now)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("ERR disk usage exceeds reject watermark")
			}
		}
	case "slowdown":
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

func (s *Store) ensureDiskWriteAllowed(key string, knownExists *bool) error {
	usage, level := s.currentDiskWatermark()
	s.logDiskWatermark(level, usage)
	switch level {
	case "reject":
		// reject 水位线是容量护栏，不是只读开关。
		// 已有 key 仍可更新，使调用方可以在阻止增长时缩小或刷新热点缓存。
		exists := false
		if knownExists != nil {
			exists = *knownExists
		} else if key != "" {
			_, _, ok, err := s.getWithExpireAt(key, time.Now())
			if err != nil {
				return err
			}
			exists = ok
		}
		if !exists {
			return fmt.Errorf("ERR disk usage exceeds reject watermark")
		}
	case "slowdown":
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

func (s *Store) currentDiskWatermark() (uint64, string) {
	usage := s.currentDiskUsageBytes(5 * time.Second)
	switch {
	case s.tuning.DiskRejectWatermarkBytes > 0 && usage >= s.tuning.DiskRejectWatermarkBytes:
		return usage, "reject"
	case s.tuning.DiskSlowdownWatermarkBytes > 0 && usage >= s.tuning.DiskSlowdownWatermarkBytes:
		return usage, "slowdown"
	case s.tuning.DiskWarnWatermarkBytes > 0 && usage >= s.tuning.DiskWarnWatermarkBytes:
		return usage, "warn"
	default:
		return usage, ""
	}
}

func (s *Store) currentDiskUsageBytes(maxAge time.Duration) uint64 {
	s.diskMu.Lock()
	defer s.diskMu.Unlock()

	if maxAge > 0 && !s.diskUsageCheckedAt.IsZero() && time.Since(s.diskUsageCheckedAt) < maxAge {
		return s.cachedDiskUsageBytes
	}

	// RocksDB 可能在 XRocksCache 命令路径之外创建和删除 WAL/SST/blob/临时 compaction 文件。
	// 遍历数据库目录可以得到简单可信的数据源；调用方通过 maxAge 控制采样可接受的陈旧程度。
	var total uint64
	_ = filepath.WalkDir(s.dir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() <= 0 {
			return nil
		}
		total += uint64(info.Size())
		return nil
	})
	s.cachedDiskUsageBytes = total
	s.diskUsageCheckedAt = time.Now()
	return total
}

func (s *Store) logDiskWatermark(level string, usage uint64) {
	if level == "" {
		return
	}

	s.diskMu.Lock()
	defer s.diskMu.Unlock()
	if level == s.lastWatermarkLogLevel && time.Since(s.lastWatermarkLoggedAt) < time.Minute {
		return
	}
	s.lastWatermarkLogLevel = level
	s.lastWatermarkLoggedAt = time.Now()

	attrs := []any{
		"level", level,
		"usage_bytes", usage,
		"warn_bytes", s.tuning.DiskWarnWatermarkBytes,
		"slowdown_bytes", s.tuning.DiskSlowdownWatermarkBytes,
		"reject_bytes", s.tuning.DiskRejectWatermarkBytes,
	}
	if level == "reject" {
		s.logger.Error("rocksdb disk reject watermark reached", attrs...)
		return
	}
	s.logger.Warn("rocksdb disk watermark reached", attrs...)
}

func (s *Store) IncrBy(key string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, expiresAt, exists, err := s.getWithExpireAtLocked(key, time.Now())
	if err != nil {
		return 0, err
	}
	var current int64
	if exists {
		current, err = strconv.ParseInt(string(old), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("ERR value is not an integer or out of range")
		}
	}
	next := current + delta
	value := []byte(strconv.FormatInt(next, 10))
	if err := s.putLocked(key, encodeValue(value, expiresAt)); err != nil {
		return 0, err
	}
	return next, nil
}

func (s *Store) Stats() map[string]string {
	keys := s.rocksPropertyInt("rocksdb.estimate-num-keys")
	liveDataSize := s.rocksPropertyInt("rocksdb.estimate-live-data-size")
	pendingCompaction := s.rocksPropertyInt("rocksdb.estimate-pending-compaction-bytes")

	return map[string]string{
		"keys":                             strconv.FormatUint(keys, 10),
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
		"rocksdb_path":                     s.dir,
		"rocksdb_estimate_live_data_size":  strconv.FormatUint(liveDataSize, 10),
		"rocksdb_pending_compaction_bytes": strconv.FormatUint(pendingCompaction, 10),
		"rocksdb_auto_tuned":               strconv.FormatBool(s.tuning.AutoTuned),
		"rocksdb_compression":              s.tuning.Compression,
		"rocksdb_disk_budget_bytes":        strconv.FormatUint(s.tuning.DiskBudgetBytes, 10),
		"rocksdb_memory_budget_bytes":      strconv.FormatUint(s.tuning.MemoryBudgetBytes, 10),
		"rocksdb_block_cache_bytes":        strconv.FormatUint(s.tuning.BlockCacheSizeBytes, 10),
		"rocksdb_write_buffer_bytes":       strconv.FormatUint(s.tuning.WriteBufferSizeBytes, 10),
		"rocksdb_target_file_size_bytes":   strconv.FormatUint(s.tuning.TargetFileSizeBaseBytes, 10),
		"rocksdb_blob_files_enabled":       strconv.FormatBool(s.tuning.EnableBlobFiles),
		"rocksdb_min_blob_size_bytes":      strconv.FormatUint(s.tuning.MinBlobSizeBytes, 10),
		"rocksdb_blob_file_size_bytes":     strconv.FormatUint(s.tuning.BlobFileSizeBytes, 10),
		"rocksdb_blob_gc_enabled":          strconv.FormatBool(s.tuning.EnableBlobGarbageCollection),
		"rocksdb_max_background_jobs":      strconv.Itoa(s.tuning.MaxBackgroundJobs),
		"rocksdb_max_subcompactions":       strconv.Itoa(s.tuning.MaxSubcompactions),
		"rocksdb_soft_pending_bytes":       strconv.FormatUint(s.tuning.SoftPendingCompactionBytesLimit, 10),
		"rocksdb_hard_pending_bytes":       strconv.FormatUint(s.tuning.HardPendingCompactionBytesLimit, 10),
		"rocksdb_rate_limiter_bytes_sec":   strconv.FormatUint(s.tuning.RateLimiterBytesPerSec, 10),
		"rocksdb_periodic_compaction_sec":  strconv.FormatInt(s.tuning.PeriodicCompactionSeconds, 10),
		"disk_warn_watermark_bytes":        strconv.FormatUint(s.tuning.DiskWarnWatermarkBytes, 10),
		"disk_slowdown_watermark_bytes":    strconv.FormatUint(s.tuning.DiskSlowdownWatermarkBytes, 10),
		"disk_reject_watermark_bytes":      strconv.FormatUint(s.tuning.DiskRejectWatermarkBytes, 10),
	}
}

func (s *Store) getWithExpireAtLocked(key string, now time.Time) ([]byte, int64, bool, error) {
	return s.getWithExpireAt(key, now)
}

func (s *Store) getWithExpireAt(key string, now time.Time) ([]byte, int64, bool, error) {
	if key == "" {
		return nil, 0, false, nil
	}

	cKey := C.CBytes([]byte(key))
	defer C.free(cKey)

	var cErr *C.char
	var vLen C.size_t
	vPtr := C.rocksdb_get(s.db, s.readOpts, (*C.char)(cKey), C.size_t(len(key)), &vLen, &cErr)
	if err := takeRocksError(cErr); err != nil {
		return nil, 0, false, err
	}
	if vPtr == nil {
		return nil, 0, false, nil
	}
	defer C.rocksdb_free(unsafe.Pointer(vPtr))

	encoded := C.GoBytes(unsafe.Pointer(vPtr), C.int(vLen))
	value, expiresAt, expired, err := decodeValue(encoded, now)
	if err != nil {
		return nil, 0, false, err
	}
	if expired {
		// 过期 key 必须立即不可见。
		// 这里的 delete 是 best-effort 物理清理；正确性不依赖它，
		// 因为后续读取仍会把 encoded value 视为已过期，直到 compaction 将其丢弃。
		_ = s.delete(key)
		return nil, expiresAt, false, nil
	}
	return value, expiresAt, true, nil
}

func (s *Store) putLocked(key string, encoded []byte) error {
	return s.put(key, encoded)
}

func (s *Store) put(key string, encoded []byte) error {
	cKey := C.CBytes([]byte(key))
	defer C.free(cKey)
	cVal := C.CBytes(encoded)
	defer C.free(cVal)

	var cErr *C.char
	C.rocksdb_put(s.db, s.writeOpts, (*C.char)(cKey), C.size_t(len(key)), (*C.char)(cVal), C.size_t(len(encoded)), &cErr)
	return takeRocksError(cErr)
}

func (s *Store) deleteLocked(key string) error {
	return s.delete(key)
}

func (s *Store) delete(key string) error {
	if key == "" {
		return nil
	}
	cKey := C.CBytes([]byte(key))
	defer C.free(cKey)

	var cErr *C.char
	C.rocksdb_delete(s.db, s.writeOpts, (*C.char)(cKey), C.size_t(len(key)), &cErr)
	return takeRocksError(cErr)
}

func (s *Store) rocksPropertyInt(name string) uint64 {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var out C.uint64_t
	if C.rocksdb_property_int(s.db, cName, &out) != 0 {
		return 0
	}
	return uint64(out)
}

func takeRocksError(cErr *C.char) error {
	if cErr == nil {
		return nil
	}
	defer C.rocksdb_free(unsafe.Pointer(cErr))
	return fmt.Errorf("%s", C.GoString(cErr))
}
