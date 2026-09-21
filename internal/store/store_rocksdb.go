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
	"math"
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
	writeLimiter *WriteLimiter
	// 生命周期锁防止关闭 C 句柄时仍有操作；分片锁让同一 key 的全部写入串行。
	lifecycle sync.RWMutex
	stripes   [256]sync.Mutex
	dir       string
	tuning    RocksDBTuning
	logger    *slog.Logger

	// 磁盘用量会被采样并缓存。
	// 这样写路径可以执行业务水位线控制，同时避免每次 SET 都遍历可能很大的 RocksDB 目录。
	diskMu                sync.Mutex
	cachedDiskUsageBytes  uint64
	diskUsageCheckedAt    time.Time
	lastWatermarkLogLevel string
	lastWatermarkLoggedAt time.Time
	cachedDiskFreeBytes   uint64
	diskSampleFailed      bool
	oldestSSTAgeSeconds   int64
	diskStop              chan struct{}
	diskDone              chan struct{}

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
	if opts.WriteRateMiB == 0 {
		opts.WriteRateMiB = 35
	}
	if opts.WriteRateMiB < 1 || opts.WriteRateMiB > 1024 {
		return nil, fmt.Errorf("invalid write rate")
	}
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
		writeLimiter: NewWriteLimiter(int64(opts.WriteRateMiB) * 1024 * 1024),
		dir:          dbPath,
		tuning:       tuning,
		logger:       logger,
		opts:         C.rocksdb_options_create(),
		readOpts:     C.rocksdb_readoptions_create(),
		writeOpts:    C.rocksdb_writeoptions_create(),
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
	// 索引和过滤器也纳入块缓存预算，避免大 key 数据集在缓存之外持续占用内存。
	tableOpts := C.rocksdb_block_based_options_create()
	cache := C.rocksdb_cache_create_lru(C.size_t(tuning.BlockCacheSizeBytes))
	C.rocksdb_block_based_options_set_block_cache(tableOpts, cache)
	C.rocksdb_block_based_options_set_cache_index_and_filter_blocks(tableOpts, 1)
	C.rocksdb_block_based_options_set_filter_policy(tableOpts, C.rocksdb_filterpolicy_create_bloom_full(10))
	C.rocksdb_options_set_block_based_table_factory(s.opts, tableOpts)
	C.rocksdb_block_based_options_destroy(tableOpts)
	C.rocksdb_cache_destroy(cache)
	C.rocksdb_options_set_max_open_files(s.opts, 256)
	C.rocksdb_options_set_max_log_file_size(s.opts, C.size_t(16*mib))
	C.rocksdb_options_set_keep_log_file_num(s.opts, 4)
	// 引擎进入写停顿时返回可观测错误，不让请求无限等待后台 compaction。
	C.rocksdb_writeoptions_set_no_slowdown(s.writeOpts, 1)
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

	s.refreshDiskUsage()
	s.diskStop, s.diskDone = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(s.diskDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.diskStop:
				return
			case <-ticker.C:
				s.refreshDiskUsage()
			}
		}
	}()
	logger.Info("rocksdb store opened", "dir", dbPath, "tuning", tuning.String())
	return s, nil
}

func (s *Store) Close() error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.diskStop != nil {
		close(s.diskStop)
		<-s.diskDone
		s.diskStop = nil
	}
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

// Flush 等待当前 memtable 落盘，供维护与落盘验收使用，不暴露为网络命令。
func (s *Store) Flush() error {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return fmt.Errorf("ERR store closed")
	}
	opts := C.rocksdb_flushoptions_create()
	defer C.rocksdb_flushoptions_destroy(opts)
	C.rocksdb_flushoptions_set_wait(opts, 1)
	var cErr *C.char
	C.rocksdb_flush(s.db, opts, &cErr)
	return takeRocksError(cErr)
}

// CompactRange 仅供显式维护与回收验收使用；空边界表示全库，不能放进普通请求路径。
// C 接口不返回本次任务状态，因此调用方还需核查文件与后台错误，不能把返回视为回收成功。
func (s *Store) CompactRange(start, end []byte) error {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return fmt.Errorf("ERR store closed")
	}
	var first, last unsafe.Pointer
	if len(start) > 0 {
		first = C.CBytes(start)
		defer C.free(first)
	}
	if len(end) > 0 {
		last = C.CBytes(end)
		defer C.free(last)
	}
	C.rocksdb_compact_range(s.db, (*C.char)(first), C.size_t(len(start)), (*C.char)(last), C.size_t(len(end)))
	if s.rocksPropertyInt("rocksdb.background-errors") != 0 {
		return fmt.Errorf("ERR rocksdb background error")
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
	value := make([]byte, valueLen)
	copy(value, encoded[encodedHeaderBytes:])
	return value, expiresAt, false, nil
}

func (s *Store) Set(key string, value []byte, opts SetOptions) ([]byte, bool, error) {
	if err := validateKeyValue(key, value); err != nil {
		return nil, false, err
	}
	if err := s.writeLimiter.Wait(int64(len(key) + len(value) + encodedHeaderBytes)); err != nil {
		return nil, false, err
	}
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return nil, false, fmt.Errorf("ERR store closed")
	}
	unlock := s.lockKeys([]string{key})
	defer unlock()
	if err := validateKeyValue(key, value); err != nil {
		return nil, false, err
	}
	if err := validateTTL(opts.TTL); err != nil {
		return nil, false, err
	}
	if opts.Mode != "" && opts.Mode != "NX" && opts.Mode != "XX" {
		return nil, false, fmt.Errorf("ERR invalid SET mode")
	}
	if opts.KeepTTL && opts.TTL != 0 {
		return nil, false, fmt.Errorf("ERR syntax error")
	}
	if opts.TTL == 0 {
		opts.TTL = MaxTTL
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
	if opts.KeepTTL && exists && oldExpiresAt > 0 {
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
	value, exists, _ := s.GetWithError(key)
	return value, exists
}

// GetWithError 保留存储错误，让协议层能够区分故障与未命中。
func (s *Store) GetWithError(key string) ([]byte, bool, error) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return nil, false, fmt.Errorf("ERR store closed")
	}
	value, _, exists, err := s.getWithExpireAt(key, time.Now())
	return value, exists, err
}

func (s *Store) MSet(pairs map[string][]byte) error {
	var bytes int64
	for key, value := range pairs {
		if err := validateKeyValue(key, value); err != nil {
			return err
		}
		bytes += int64(len(key) + len(value) + encodedHeaderBytes)
	}
	if err := s.writeLimiter.Wait(bytes); err != nil {
		return err
	}
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return fmt.Errorf("ERR store closed")
	}
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	unlock := s.lockKeys(keys)
	defer unlock()
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

	expiresAt := time.Now().Add(MaxTTL).UnixMilli()
	for key, value := range pairs {
		cKey := C.CBytes([]byte(key))
		encoded := encodeValue(value, expiresAt)
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
	// 删除保留为磁盘紧张时的恢复通道，不进入新增版本的字节限速。
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return 0, fmt.Errorf("ERR store closed")
	}
	unlock := s.lockKeys(keys)
	defer unlock()
	batch := C.rocksdb_writebatch_create()
	defer C.rocksdb_writebatch_destroy(batch)
	seen := make(map[string]bool, len(keys))

	var removed int64
	now := time.Now()
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		_, _, exists, err := s.getWithExpireAtLocked(key, now)
		if err != nil {
			return removed, err
		}
		if !exists {
			continue
		}
		cKey := C.CBytes([]byte(key))
		C.rocksdb_writebatch_delete(batch, (*C.char)(cKey), C.size_t(len(key)))
		C.free(cKey)
		removed++
	}
	var cErr *C.char
	C.rocksdb_write(s.db, s.writeOpts, batch, &cErr)
	if err := takeRocksError(cErr); err != nil {
		return 0, err
	}
	return removed, nil
}

func (s *Store) Exists(keys ...string) int64 {
	count, _ := s.ExistsWithError(keys...)
	return count
}

// ExistsWithError 对多 key 读取采用与批量写入一致的锁顺序。
func (s *Store) ExistsWithError(keys ...string) (int64, error) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return 0, fmt.Errorf("ERR store closed")
	}
	unlock := s.lockKeys(keys)
	defer unlock()
	var count int64
	now := time.Now()
	for _, key := range keys {
		_, _, exists, err := s.getWithExpireAt(key, now)
		if err != nil {
			return 0, err
		}
		if exists {
			count++
		}
	}
	return count, nil
}

func (s *Store) Expire(key string, ttl time.Duration) (bool, error) {
	// 原子读取前按最大 value 预留，避免等待预算时持有 key 锁。
	if ttl > 0 {
		if err := s.writeLimiter.Wait(int64(len(key) + MaxValueBytes + encodedHeaderBytes)); err != nil {
			return false, err
		}
	}
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return false, fmt.Errorf("ERR store closed")
	}
	if err := validateTTL(ttl); err != nil {
		return false, err
	}

	unlock := s.lockKeys([]string{key})
	defer unlock()

	value, _, exists, err := s.getWithExpireAtLocked(key, time.Now())
	if err != nil || !exists {
		return false, err
	}
	if ttl == 0 {
		return true, s.delete(key)
	}
	if err := s.ensureDiskWriteAllowed(key, nil); err != nil {
		return false, err
	}
	return true, s.putLocked(key, encodeValue(value, time.Now().Add(ttl).UnixMilli()))
}

func (s *Store) TTL(key string) (time.Duration, bool, bool) {
	ttl, exists, hasTTL, _ := s.TTLWithError(key)
	return ttl, exists, hasTTL
}

// TTLWithError 不在读路径删除记录，避免旧读取误删新版本。
func (s *Store) TTLWithError(key string) (time.Duration, bool, bool, error) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return 0, false, false, fmt.Errorf("ERR store closed")
	}
	_, expiresAt, exists, err := s.getWithExpireAt(key, time.Now())
	if err != nil || !exists {
		return 0, false, false, err
	}
	if expiresAt == 0 {
		return 0, true, false, nil
	}
	ttl := time.Until(time.UnixMilli(expiresAt))
	if ttl <= 0 {
		return 0, false, false, nil
	}
	return ttl, true, true, nil
}

func (s *Store) DBSize() int64 {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return 0
	}
	// 精确 DBSIZE 需要扫描整个 RocksDB keyspace，并解码每个 value 的 TTL。
	// 对 100G 缓存服务来说这是不可接受的，因此 RocksDB 构建返回引擎估算值。
	// INFO 暴露同一个估算值，便于运维把它视为 approximate。
	return int64(s.rocksPropertyInt("rocksdb.estimate-num-keys"))
}

func (s *Store) ensureMSetDiskWriteAllowed(pairs map[string][]byte) error {
	return s.ensureDiskWriteAllowed("", nil)
}

func (s *Store) ensureDiskWriteAllowed(key string, knownExists *bool) error {
	usage, level := s.currentDiskWatermark()
	s.logDiskWatermark(level, usage)
	switch level {
	case "reject":
		// 覆盖也会产生 WAL 和新文件，拒绝水位必须阻止所有新增版本。
		return fmt.Errorf("ERR disk usage exceeds reject watermark or free-space reserve")
	case "slowdown":
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

func (s *Store) currentDiskWatermark() (uint64, string) {
	s.diskMu.Lock()
	usage, free, failed, checked := s.cachedDiskUsageBytes, s.cachedDiskFreeBytes, s.diskSampleFailed, s.diskUsageCheckedAt
	s.diskMu.Unlock()
	if failed || time.Since(checked) > 10*time.Second || free <= s.tuning.DiskReserveBytes {
		return usage, "reject"
	}
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

// refreshDiskUsage 在后台采样，不把目录遍历延迟传递给每个写请求。
func (s *Store) refreshDiskUsage() {
	var total uint64
	failed := false
	var oldestDataMTime time.Time
	_ = filepath.WalkDir(s.dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if !os.IsNotExist(err) {
				failed = true
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			if !os.IsNotExist(err) {
				failed = true
			}
			return nil
		}
		if info.Size() <= 0 {
			return nil
		}
		total += uint64(info.Size())
		switch filepath.Ext(path) {
		case ".sst", ".blob":
			if oldestDataMTime.IsZero() || info.ModTime().Before(oldestDataMTime) {
				oldestDataMTime = info.ModTime()
			}
		}
		return nil
	})
	free := detectDiskFreeBytes(s.dir)
	s.diskMu.Lock()
	defer s.diskMu.Unlock()
	s.cachedDiskUsageBytes = total
	s.cachedDiskFreeBytes = free
	s.diskSampleFailed = failed || free == 0
	s.diskUsageCheckedAt = time.Now()
	if oldestDataMTime.IsZero() {
		s.oldestSSTAgeSeconds = 0
	} else {
		s.oldestSSTAgeSeconds = int64(time.Since(oldestDataMTime) / time.Second)
	}
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
	if err := s.writeLimiter.Wait(int64(len(key) + 20 + encodedHeaderBytes)); err != nil {
		return 0, err
	}
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return 0, fmt.Errorf("ERR store closed")
	}
	if err := validateKeyValue(key, nil); err != nil {
		return 0, err
	}
	unlock := s.lockKeys([]string{key})
	defer unlock()

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
	if (delta > 0 && current > math.MaxInt64-delta) || (delta < 0 && current < math.MinInt64-delta) {
		return 0, fmt.Errorf("ERR increment or decrement would overflow")
	}
	if err := s.ensureDiskWriteAllowed(key, nil); err != nil {
		return 0, err
	}
	// 过期记录在读路径上表现为不存在；重新创建时不能沿用旧的过期时间，
	// 否则 INCRBY 虽然返回成功，新值仍会立即被视为过期。
	if !exists || expiresAt == 0 {
		expiresAt = time.Now().Add(MaxTTL).UnixMilli()
	}
	next := current + delta
	value := []byte(strconv.FormatInt(next, 10))
	if err := s.putLocked(key, encodeValue(value, expiresAt)); err != nil {
		return 0, err
	}
	return next, nil
}

func (s *Store) Stats() map[string]string {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.db == nil {
		return map[string]string{}
	}
	keys := s.rocksPropertyInt("rocksdb.estimate-num-keys")
	liveDataSize := s.rocksPropertyInt("rocksdb.estimate-live-data-size")
	pendingCompaction := s.rocksPropertyInt("rocksdb.estimate-pending-compaction-bytes")
	s.diskMu.Lock()
	usage, free, failed, oldestSSTAge := s.cachedDiskUsageBytes, s.cachedDiskFreeBytes, s.diskSampleFailed, s.oldestSSTAgeSeconds
	s.diskMu.Unlock()

	return map[string]string{
		"write_rate_bytes_sec":             strconv.FormatInt(s.writeLimiter.bytesPerSecond, 10),
		"disk_usage_bytes":                 strconv.FormatUint(usage, 10),
		"disk_free_bytes":                  strconv.FormatUint(free, 10),
		"disk_reserve_bytes":               strconv.FormatUint(s.tuning.DiskReserveBytes, 10),
		"disk_sample_failed":               strconv.FormatBool(failed),
		"rocksdb_oldest_sst_age_sec":       strconv.FormatInt(oldestSSTAge, 10),
		"rocksdb_background_errors":        strconv.FormatUint(s.rocksPropertyInt("rocksdb.background-errors"), 10),
		"rocksdb_immutable_memtables":      strconv.FormatUint(s.rocksPropertyInt("rocksdb.num-immutable-mem-table"), 10),
		"rocksdb_num_running_compactions":  strconv.FormatUint(s.rocksPropertyInt("rocksdb.num-running-compactions"), 10),
		"rocksdb_num_running_flushes":      strconv.FormatUint(s.rocksPropertyInt("rocksdb.num-running-flushes"), 10),
		"rocksdb_memtable_bytes":           strconv.FormatUint(s.rocksPropertyInt("rocksdb.cur-size-all-mem-tables"), 10),
		"rocksdb_l0_files":                 s.rocksPropertyString("rocksdb.num-files-at-level0"),
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
	if err := validateKeyValue(key, nil); err != nil {
		return nil, 0, false, err
	}
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
		// 读路径只隐藏过期记录；物理回收由 RocksDB 完成，不删除可能已更新的 key。
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

func (s *Store) rocksPropertyString(name string) string {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	value := C.rocksdb_property_value(s.db, cName)
	if value == nil {
		return "unavailable"
	}
	defer C.rocksdb_free(unsafe.Pointer(value))
	return C.GoString(value)
}

func takeRocksError(cErr *C.char) error {
	if cErr == nil {
		return nil
	}
	defer C.rocksdb_free(unsafe.Pointer(cErr))
	return fmt.Errorf("%s", C.GoString(cErr))
}
