# XRocksCache Go + RocksDB 实施计划

英文文档见 [PLAN_EN.md](PLAN_EN.md)。

## 产品边界

XRocksCache 是轻量级、单机、Redis RESP 兼容的 String K/V 缓存服务。核心目标是低成本 2C4G / 4C8G 机器上的大容量本地磁盘缓存，不是完整 Redis，也不是主数据库。

- 一个进程，一个本地数据目录。
- 生产存储使用 RocksDB，不自研 segment、SST、blob 文件格式。
- 只保留 String key/value 与 TTL 行为。
- key 最大 512KiB，value 最大 5MiB。
- 写入 TTL 最长 15 天；超过 15 天的 TTL 会被拒绝。
- 过期不可见由读路径保证；物理清理由 RocksDB compaction filter 延迟完成。
- 不提供集群、复制、namespace 隔离、事务、Lua、Pub/Sub、搜索或复杂数据结构。
- 节点可丢弃；缓存 miss 后的数据恢复由调用方负责。

## 已完成

1. 创建本地 release tag `release_tag_cpp_baseline_20260907`，封存切换前 C++ 精简版。
2. 删除 C++/CMake 服务端源码树，新增 Go 模块。
3. 实现 RESP2 协议读写、TCP 服务、基础认证和命令分发。
4. 保留 `xrockscache` 二进制目标和 `xrockscache.conf` 配置入口。
5. 引入 `log/slog` 结构化日志，并补充按天切换和保留期清理。
6. 增加 RocksDB 自动调参逻辑：根据 CPU、内存、数据盘剩余空间推导内部参数，不暴露 RocksDB 细节到主配置。
7. 增加生产 RocksDB Store：使用 RocksDB WAL、memtable、SST、blob files、LZ4、leveled compaction、blob GC 和 TTL compaction filter。
8. 增加 WSL/Linux RocksDB 构建验证脚本 `dev/build-rocksdb-wsl.sh`。
9. 增加 RocksDB 专用测试，覆盖 TTL 不可见、重启恢复、条件 SET、KEEPTTL、大 value、Stats/INFO 形态和磁盘水位线。
10. 将 `DBSIZE` 在 RocksDB 构建中改为近似值，避免 100G 数据下全库扫描。
11. 将普通 `SET` 改为无条件直接写入，只有 `NX`、`XX`、`GET`、`KEEPTTL` 等语义需要时才读取旧值。
12. 磁盘水位线和实时可用空间保护覆盖全部写路径：warn 告警，slowdown 降速，reject 拒绝新增版本（包含覆盖写入），保留删除能力。
13. 删除旧 AOF + 内存 Store，以及无 RocksDB 库时的显式失败占位实现；`internal/store` 现在只保留直接依赖 cgo + RocksDB 的生产 Store，默认构建即直接使用 RocksDB，不再需要 `rocksdb` build tag。
14. 将主模块 Go 测试统一移动到 `test/` 目录，server 测试改为真实 RESP/TCP 黑盒测试。

## 当前重点

- 持续验证分片锁的并发语义、真实 Blob 回收和资源受限环境下的长时间运行。
- 扩展端到端 Redis 协议兼容测试覆盖面。
- 在真实 2C4G / 4C8G 云服务器上重新压测 10k QPS、100ms RT 基线。
- 根据压测结果决定是否增加更细粒度的磁盘水位策略和 compaction 观测指标。
