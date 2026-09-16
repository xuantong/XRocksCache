package store

import (
	"log/slog"
	"time"
)

const (
	// MaxKeyBytes 和 MaxValueBytes 是产品护栏，不是 RocksDB 极限。
	// 它们避免低成本缓存节点被少数超大请求拖垮，同时仍允许较大的缓存 payload 使用 RocksDB blob files。
	MaxKeyBytes   = 512 * 1024
	MaxValueBytes = 1024 * 1024

	// MaxTTL 用于落实约定的缓存合同：SET/EXPIRE 逻辑绝不能让用户 value 存活超过 15 天。
	MaxTTL = 15 * 24 * time.Hour
)

type Entry struct {
	Value     []byte
	ExpiresAt int64
}

// SetOptions 承载命令层支持的 Redis-compatible SET 变体。
// 只有需要旧状态的选项，才会在 RocksDB 实现中进入读改写临界区。
type SetOptions struct {
	TTL     time.Duration
	Mode    string
	Get     bool
	KeepTTL bool
}

type Options struct {
	// Expiration 只保留给历史配置兼容。
	// 生产 RocksDB 路径不使用内存过期 bucket 清理器，而是依赖读时不可见和 compaction-filter 物理清理。
	Expiration ExpirationConfig
	Tuning     RocksDBTuning
	Logger     *slog.Logger
}

// ExpirationConfig 只作为历史配置兼容面保留。
// RocksDB 构建会在 INFO 中明确报告 active expiration 为 disabled。
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
