# XRocksCache Go Implementation Plan / XRocksCache Go 实施计划

## 中文

### 产品边界

XRocksCache 当前主干已经切换为 Go 实现。它是轻量级、单机、Redis RESP 兼容的 K/V 缓存，不是完整 Redis，也不是主数据库。

- 一个进程、一个本地数据目录、一个追加日志文件。
- 只保留字符串 key/value 与 TTL 行为。
- key 最大 512KiB，value 最大 1MiB。
- 写入 TTL 最长 15 天；超过 15 天的 TTL 会被拒绝。
- 不提供集群、复制、namespace 隔离、事务、Lua、Pub/Sub、搜索、复杂数据结构。
- 节点可丢弃；缓存 miss 后的数据恢复由调用方负责。

### 已完成

1. 创建本地 release tag `release_tag_cpp_baseline_20260907`，封存切换前 C++ 精简版。
2. 删除 C++/CMake 源码树，新增 Go 模块。
3. 实现 RESP2 协议读写、TCP 服务、基础认证和命令分发。
4. 实现标准库追加日志 `xrockscache.aof` 与内存索引。
5. 保留 `xrockscache` 二进制目标和 `xrockscache.conf` 配置入口。
6. 更新 Docker、devcontainer、pre-push、README、安全文档和代码阅读指南。
7. 删除旧 C++ 第三方依赖许可证目录，当前 Go 主干仅保留根目录 Apache License 2.0。
8. 引入 Go 标准库 `log/slog` 作为结构化日志组件，替代服务端 `fmt.Printf` 日志输出。

### V1 命令面

`PING`、`ECHO`、`GET`、`SET`、`MGET`、`MSET`、`DEL`、`EXISTS`、`EXPIRE`、`PEXPIRE`、`TTL`、`PTTL`、`INCR`、`INCRBY`、`DECR`、`DECRBY`、`DBSIZE`、`INFO`、`AUTH`、`COMMAND`、`HELLO`、`CLIENT`、`SELECT`。

### 后续重点

- 增加 AOF rewrite，避免长期运行后文件无限增长。
- 增加可插拔存储后端，为后续云盘和大容量优化预留空间。
- 用真实 2C4G / 4C8G 云服务器重新压测 10k QPS、100ms RT 基线。
- 增加端到端 Redis 协议兼容测试。

## English

### Product scope

The current main branch of XRocksCache has been switched to Go. It is a lightweight, single-node, Redis RESP-compatible K/V cache, not full Redis and not a primary database.

- One process, one local data directory, one append-only log file.
- String key/value operations with TTL semantics only.
- Maximum key size: 512KiB; maximum value size: 1MiB.
- Maximum write TTL: 15 days; longer TTLs are rejected.
- No cluster, replication, namespace isolation, transactions, Lua, Pub/Sub, search, or complex data structures.
- Nodes are disposable; cache-miss recovery belongs to callers.

### Completed

1. Created local release tag `release_tag_cpp_baseline_20260907` to preserve the minimal C++ baseline.
2. Removed the C++/CMake source tree and added the Go module.
3. Implemented RESP2 parsing/writing, TCP serving, basic authentication, and command dispatch.
4. Implemented standard-library append-only log `xrockscache.aof` plus in-memory index.
5. Kept the `xrockscache` binary target and `xrockscache.conf` configuration entry.
6. Updated Docker, devcontainer, pre-push, README, security docs, and code reading guide.
7. Removed the previous C++ third-party dependency license directory; the current Go main branch keeps only the root Apache License 2.0 file.
8. Added Go standard-library `log/slog` as the structured logging component and removed server-side `fmt.Printf` logging.

### V1 command surface

`PING`, `ECHO`, `GET`, `SET`, `MGET`, `MSET`, `DEL`, `EXISTS`, `EXPIRE`, `PEXPIRE`, `TTL`, `PTTL`, `INCR`, `INCRBY`, `DECR`, `DECRBY`, `DBSIZE`, `INFO`, `AUTH`, `COMMAND`, `HELLO`, `CLIENT`, `SELECT`.

### Next priorities

- Add AOF rewrite to prevent unlimited long-running file growth.
- Add pluggable storage backends for future cloud-disk and large-capacity optimization.
- Re-run 10k QPS and 100ms RT baselines on real 2C4G / 4C8G cloud servers.
- Add end-to-end Redis protocol compatibility tests.
