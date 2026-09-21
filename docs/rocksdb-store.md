# XRocksCache RocksDB 存储

英文文档见 [rocksdb-store_EN.md](rocksdb-store_EN.md)。

## 存储定位

XRocksCache 的生产存储路径使用 RocksDB，而不是自研 segment、SST、blob 文件系统。`internal/store` 的生产 Store 实现直接依赖 cgo + RocksDB，默认构建即直接使用真正的 RocksDB Store，不需要额外 build tag：

```bash
CGO_ENABLED=1 go build -trimpath -o build/xrockscache ./cmd/xrockscache
```

该命令要求能找到 RocksDB 头文件和链接库（系统安装的 `librocksdb-dev`，或通过 `dev/build-rocksdb-wsl.sh` 从源码构建并设置 `CGO_CFLAGS`/`CGO_LDFLAGS`），否则会编译失败。

## 数据布局

RocksDB key 直接使用用户 key。RocksDB value 使用 XRocksCache 自己的轻量头部加用户 value：

```text
0..3    magic: XRC1
4       version: 1
5       flags，预留
6..7    reserved，预留
8..15   expiresAtMs，小端序；0 表示永不过期
16..19  用户 value 长度，小端序
20..    用户 value
```

这样做的原因是 TTL 信息必须和 value 一起进入 RocksDB，读路径和 compaction filter 都可以直接判断是否过期，不依赖全量内存索引。

## TTL 语义

XRocksCache 保证：

- 过期 key 在读路径立即不可见；
- `GET`、`EXISTS`、`TTL`、`PTTL` 都会解码 `expiresAtMs`；
- 如果读到已过期 key，只返回不存在，不执行可能误删新版本的无条件删除；
- 新的 `SET` 未提供 TTL、`MSET` 以及 `INCRBY` 创建 key 时，默认 TTL 为 15 天；
- RocksDB compaction filter 会在后台 compaction 时物理丢弃过期记录。

因此，过期正确性不依赖 compaction 是否马上发生。compaction 只负责延迟物理回收。

周期 compaction 当前为 12 小时，它是文件调度条件，不是清理完成期限。物理空间回收不保证 30–60 秒；已有数据库中历史的零 TTL 记录仍可读取，升级时应重建缓存或显式设置过期时间。不要直接删除正在使用的 SST 或 Blob 文件。

所有写入都获取固定大小分片锁；多 key 命令按分片编号排序获取锁。读路径不删除数据，`MGET` 与 `MSET` 保持批量一致性。关闭数据库会等待存储操作结束，防止 C 句柄释放后继续访问。

WAL 默认启用，但不对每个请求同步刷盘。进程重启恢复与机器掉电不丢数据是不同保证；缓存调用方必须允许掉电时丢失最近写入并回源重建。

## AOF 删除

XRocksCache 不再使用 `xrockscache.aof` 作为主存储。旧 AOF + 内存 Store 已删除，持久化由 RocksDB WAL、memtable、SST 和 blob 文件负责。

这点是核心变化：XRocksCache 不再把完整缓存数据放进内存 map，也不再通过 AOF 重放恢复 100G 级数据。

## 文件膨胀控制

第一版不引入 kvrocks 的复杂 checker，而使用这些机制控制文件膨胀：

- LZ4 压缩；
- leveled compaction；
- TTL compaction filter；
- blob file 和 blob GC；
- `soft_pending_compaction_bytes_limit`；
- `hard_pending_compaction_bytes_limit`；
- 后台 rate limiter；
- `periodic_compaction_seconds`；
- XRocksCache 磁盘水位线。

这些参数由启动时自动探测 CPU、内存和数据盘剩余空间后计算，不暴露到主配置文件。

容量预算包含已有数据库占用，避免重启后缩小预算；每秒后台采样数据库占用和文件系统剩余空间。拒绝水位阻止全部新增版本，包括覆盖、续期、自增和批量写入；删除仍然允许。采样失败或超过 10 秒未更新时拒绝写入。采样存在时间窗口，磁盘预留空间仍然必要，也不能替代操作系统磁盘告警。

内存探测考虑常见 cgroup v1/v2 当前及祖先限制。块缓存包含索引与过滤器；Go 堆另外设置软限制。这些预算不是进程 RSS 的硬上限，应使用实际部署环境监控 RSS、可用内存和 OOM 事件。部署验收见 [production-readiness.md](production-readiness.md)。
