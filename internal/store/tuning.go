package store

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	kib uint64 = 1024
	mib uint64 = 1024 * kib
	gib uint64 = 1024 * mib
)

const (
	defaultUnknownMemoryBytes   = 4 * gib
	defaultUnknownDiskFreeBytes = 100 * gib
)

// ResourceProfile 描述用于推导 RocksDB 默认值的机器资源。
// 这里刻意保持小而面向业务：调用方不传 RocksDB 细节参数，只传机器和数据盘事实。
type ResourceProfile struct {
	// CPUCores 是当前进程可见的逻辑 CPU 数。
	// 调参逻辑用它限制后台 compaction 并发，避免抢占过多前台 GET/SET CPU。
	CPUCores int

	// MemoryBytes 是主机可见的物理内存总量。
	// XRocksCache 只把其中一部分分配给 RocksDB，给 2C4G/4C8G 机器保留
	// OS page cache、网络栈、Go runtime 和日志空间。
	MemoryBytes uint64

	// DiskFreeBytes 是数据目录所在文件系统的剩余空间。
	// 默认缓存预算从这个值推导，而不是写死 2C4G/4C8G 模板，因为云盘差异远大于 CPU/RAM。
	DiskFreeBytes uint64
	// DiskUsedBytes 计入已有数据库，避免每次重启都按剩余空间缩小总预算。
	DiskUsedBytes uint64

	// DiskBudgetBytes 可选地限制推导出的缓存预算。
	// 0 表示“使用 DiskFreeBytes 的 80%，再压到产品安全范围内”。
	DiskBudgetBytes uint64
}

// RocksDBTuning 是 XRocksCache 内部使用的 RocksDB 调参方案。
// 这些字段不应一一映射到 xrockscache.conf；普通用户只需要看到简单产品配置，
// 维护者则可以通过这个结构让引擎默认值保持明确、可测试。
type RocksDBTuning struct {
	// AutoTuned 标记这些值来自 BuildRocksDBTuning，而不是用户手写 RocksDB 参数。
	// 这样 INFO 和日志可以解释当前行为来源。
	AutoTuned bool

	// Compression 第一版刻意固定为 LZ4：
	// 在低成本机器上，它能提供有效压缩，同时避免 ZSTD 的 CPU 成本。
	Compression string

	// DiskBudgetBytes 是磁盘水位线和 compaction backlog 限制使用的有效缓存预算。
	// 它不是 RocksDB 一定精确使用的容量，而是业务侧容量护栏。
	DiskBudgetBytes uint64

	// MemoryBudgetBytes 是进程侧分配给 RocksDB 结构的总预算。
	// 它会拆分给 block cache、memtable 和元数据预留。
	MemoryBudgetBytes uint64

	// BlockCacheSizeBytes 缓存点查所需的解压数据块以及 bloom/filter 元数据。
	// XRocksCache 主要是 GET-heavy 缓存服务，所以这里获得最大内存份额。
	BlockCacheSizeBytes uint64

	// WriteBufferSizeBytes 是单个可写 memtable 的大小。
	// 默认允许三个 memtable，使后台 flush 运行时前台写入仍有缓冲空间。
	WriteBufferSizeBytes        uint64
	MaxWriteBufferNumber        int
	MinWriteBufferNumberToMerge int

	// TargetFileSizeBaseBytes 控制 leveled compaction 的基础 SST 文件大小。
	// 它随磁盘预算放大，避免大盘上出现过多小文件，同时让 2C4G 部署仍能较快 compact。
	TargetFileSizeBaseBytes uint64

	// Blob 选项用于让中大 value 避开 SST 重写路径。
	// blob 文件仍由 RocksDB 管理，XRocksCache 不实现自己的 segment 或 blob 存储引擎。
	EnableBlobFiles             bool
	MinBlobSizeBytes            uint64
	BlobFileSizeBytes           uint64
	EnableBlobGarbageCollection bool

	// 后台 compaction 限制刻意保守。
	// 目标是在便宜机器上得到可预测的 p99 延迟，而不是追求最大离线吞吐。
	MaxBackgroundJobs int
	MaxSubcompactions int

	// Pending compaction 限制是第一版替代复杂 kvrocks-style checker 的方案。
	// 当 compaction backlog 变大时，RocksDB 可以在文件无界膨胀前主动降速或暂停写入。
	SoftPendingCompactionBytesLimit uint64
	HardPendingCompactionBytesLimit uint64

	// RateLimiterBytesPerSec 限制后台 I/O 压力。
	// 它由磁盘预算推导并做上下限保护，避免小机器被 compaction 压垮。
	RateLimiterBytesPerSec uint64

	// PeriodicCompactionSeconds 防止很冷的 SST 文件永久保留过期 key 的物理存储。
	// GET 仍保证过期 key 不可见；这里只影响磁盘空间物理回收的时效性，不影响读路径正确性。
	// 这不是逐 key 删除的扫描周期：到期后 RocksDB 会强制重写涉及的整份 SST，
	// 因此不能定得像秒级主动过期那么短，否则会在 100GB 数据集上引发持续性全量重写，
	// 拖垮 2C4G/4C8G 目标机器的写放大和尾延迟。定为 2 小时是在“冷数据不会无限期占用磁盘”
	// 与“不给便宜机器制造额外 compaction 压力”之间的折中；配合 rocksdb_oldest_sst_age_sec
	// 指标观察实际回收时效，如果观测到回收滞后明显可以再收紧。
	PeriodicCompactionSeconds int64

	// 水位线是 XRocksCache 业务层容量控制。
	// compaction 不是容量管理器：如果用户持续写入新的有效 key，只有水位线能保护磁盘不被打满。
	DiskWarnWatermarkBytes     uint64
	DiskSlowdownWatermarkBytes uint64
	DiskRejectWatermarkBytes   uint64
	// DiskReserveBytes 是实时文件系统可用空间的保护阈值，给后台回收保留余量。
	DiskReserveBytes uint64
}

// DetectResourceProfile 读取指定数据目录对应的主机资源。
// 平台相关实现放在 sysinfo_*.go，让调参算法本身保持可测试且不混入 OS 细节。
func DetectResourceProfile(dir string) ResourceProfile {
	profile := ResourceProfile{
		CPUCores: runtime.NumCPU(),
	}
	if mem := detectTotalMemoryBytes(); mem > 0 {
		profile.MemoryBytes = mem
	}
	profile.MemoryBytes, profile.CPUCores = constrainResources(profile.MemoryBytes, profile.CPUCores)
	if free := detectDiskFreeBytes(dir); free > 0 {
		profile.DiskFreeBytes = free
	}
	if dir == "" {
		dir = "data"
	}
	_ = filepath.WalkDir(filepath.Join(dir, "rocksdb"), func(_ string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			if info, err := entry.Info(); err == nil && info.Size() > 0 {
				profile.DiskUsedBytes += uint64(info.Size())
			}
		}
		return nil
	})
	return profile
}

// BuildRocksDBTuning 根据机器资源推导 RocksDB 默认值。
func BuildRocksDBTuning(profile ResourceProfile) RocksDBTuning {
	if profile.CPUCores <= 0 {
		profile.CPUCores = runtime.NumCPU()
	}
	if profile.MemoryBytes == 0 {
		// 如果 OS 探测失败，按最小重要生产目标估算。
		// 这样未知机器上的默认值会偏安全，而不是意外分配过大的缓存。
		profile.MemoryBytes = defaultUnknownMemoryBytes
	}
	if profile.DiskFreeBytes == 0 {
		// 未知磁盘空间不应让引擎变激进。
		// 第一阶段讨论的产品目标是 100G，因此用它作为保守 fallback。
		profile.DiskFreeBytes = defaultUnknownDiskFreeBytes
	}

	diskBudget := profile.DiskBudgetBytes
	available := profile.DiskFreeBytes + profile.DiskUsedBytes
	if diskBudget == 0 || diskBudget > available*80/100 {
		// 最多使用“当前剩余空间加已有数据库占用”的 80%，给临时输出、
		// OS 元数据和运维恢复预留空间。
		diskBudget = available * 80 / 100
	}
	diskBudget = minUint64(diskBudget, 500*gib)

	// RocksDB 主要缓存结构预算为有效内存的 35%，不是进程 RSS 的硬上限。
	// 这会有意把大部分 RAM 留给 OS page cache 和非存储服务开销。
	memoryBudget := minUint64(profile.MemoryBytes*35/100, 3*gib)

	// GET 延迟最依赖 block cache，写入突增需要有界的 memtable 预算。
	// 剩余 10% 隐含留给索引、bloom filter、iterator 和 Go runtime 开销。
	blockCacheSize := minUint64(memoryBudget*60/100, 2*gib)
	writeBufferTotal := memoryBudget * 30 / 100
	writeBufferSize := roundDownPowerOfTwo(writeBufferTotal / 3)
	writeBufferSize = minUint64(writeBufferSize, 256*mib)

	// 按有效预算大约 1024 个基础层文件估算，再向上取 2 的幂，
	// 让 RocksDB 文件大小在运维上更可预期。
	targetFileSize := roundUpPowerOfTwo(diskBudget / 1024)
	targetFileSize = clampUint64(targetFileSize, 64*mib, 256*mib)

	// Blob 文件比 SST 文件更大，因为它们承载 value payload；
	// 在 5MiB value 工作负载下不应过于频繁轮转。
	blobFileSize := clampUint64(targetFileSize*4, 256*mib, 1*gib)

	// compaction 并发保持保守。
	// 2C4G 上通常是 2 个 job、1 个 subcompaction；常见 4C8G 上是 3 个 job、1 个 subcompaction。
	maxBackgroundJobs := clampInt(profile.CPUCores, 2, 3)
	maxSubcompactions := clampInt(profile.CPUCores/4, 1, 2)

	softPending := minUint64(diskBudget*5/100, 8*gib)
	hardPending := minUint64(diskBudget*10/100, 16*gib)

	// 限制后台任务速率，避免 compaction 独占便宜云盘。
	// 公式随容量扩展，但保持在保守的 16MB/s - 128MB/s 区间。
	rateLimiter := clampUint64(diskBudget/7200, 16*mib, 128*mib)

	return RocksDBTuning{
		AutoTuned:                       true,
		Compression:                     "lz4",
		DiskBudgetBytes:                 diskBudget,
		MemoryBudgetBytes:               memoryBudget,
		BlockCacheSizeBytes:             blockCacheSize,
		WriteBufferSizeBytes:            writeBufferSize,
		MaxWriteBufferNumber:            3,
		MinWriteBufferNumberToMerge:     1,
		TargetFileSizeBaseBytes:         targetFileSize,
		EnableBlobFiles:                 true,
		MinBlobSizeBytes:                16 * kib,
		BlobFileSizeBytes:               blobFileSize,
		EnableBlobGarbageCollection:     true,
		MaxBackgroundJobs:               maxBackgroundJobs,
		MaxSubcompactions:               maxSubcompactions,
		SoftPendingCompactionBytesLimit: softPending,
		HardPendingCompactionBytesLimit: hardPending,
		RateLimiterBytesPerSec:          rateLimiter,
		PeriodicCompactionSeconds:       int64(2 * 60 * 60),
		DiskWarnWatermarkBytes:          diskBudget * 85 / 100,
		DiskSlowdownWatermarkBytes:      diskBudget * 90 / 100,
		DiskRejectWatermarkBytes:        diskBudget * 95 / 100,
		DiskReserveBytes:                minUint64(available/10, 2*gib),
	}
}

// String 返回用于日志的紧凑运行摘要。
// 这里刻意使用字节而不是人类可读单位，便于监控系统直接解析。
func (t RocksDBTuning) String() string {
	return fmt.Sprintf(
		"compression=%s disk_budget=%d memory_budget=%d block_cache=%d write_buffer=%d target_file_size=%d blob_file_size=%d max_background_jobs=%d max_subcompactions=%d",
		t.Compression,
		t.DiskBudgetBytes,
		t.MemoryBudgetBytes,
		t.BlockCacheSizeBytes,
		t.WriteBufferSizeBytes,
		t.TargetFileSizeBaseBytes,
		t.BlobFileSizeBytes,
		t.MaxBackgroundJobs,
		t.MaxSubcompactions,
	)
}

func clampUint64(v, min, max uint64) uint64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func roundDownPowerOfTwo(v uint64) uint64 {
	if v == 0 {
		return 0
	}
	var p uint64 = 1
	for p<<1 <= v {
		p <<= 1
	}
	return p
}

func roundUpPowerOfTwo(v uint64) uint64 {
	if v <= 1 {
		return 1
	}
	p := roundDownPowerOfTwo(v)
	if p == v {
		return v
	}
	return p << 1
}
