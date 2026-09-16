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
- 如果读到已过期 key，会返回不存在，并顺手写入 RocksDB 删除；
- RocksDB compaction filter 会在后台 compaction 时物理丢弃过期记录。

因此，过期正确性不依赖 compaction 是否马上发生。compaction 只负责延迟物理回收。

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
