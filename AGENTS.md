# XRocksCache Agent Guide / XRocksCache 智能体协作指南

## 中文

本仓库是 XRocksCache：一个面向低成本云服务器的大容量单机 K/V 缓存服务，基于 RocksDB 存储，兼容 Redis 协议中的核心字符串与基础管理能力。

### 项目边界

- 服务端二进制目标固定为 `xrockscache`。
- 默认配置文件为 `xrockscache.conf`，4C8G 基线配置为 `xrockscache-4c8g.conf`。
- 项目聚焦 String K/V 缓存，不再保留 Hash、List、Set、ZSet、Stream、JSON、Bloom、Search、Cluster、Replication、Lua/RDB 导入导出等非核心能力。
- `SET` 类写入必须遵守缓存业务约束：key 最大 512 KiB、value 最大 1 MiB、TTL 最长 15 天。
- 性能基线以低成本 2C4G / 4C8G 云服务器为目标，优先保证 1w QPS 下请求延迟在 100ms 以内。

### 构建

```bash
cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release -DDISABLE_JEMALLOC=ON
cmake --build build --target xrockscache -j4
```

如果本机没有 Ninja，可改用默认生成器：

```bash
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DDISABLE_JEMALLOC=ON
cmake --build build --target xrockscache -j4
```

### 本地运行

```bash
./build/xrockscache -c xrockscache.conf
```

### 开发规则

- 代码、命令名、配置项保持英文；面向用户的新文档必须同时包含中文和英文。
- 优先做小而清晰的补丁，不要重新引入已删除的旧复杂数据结构或分布式能力。
- 修改存储层、命令层或配置约束后，至少执行一次 `xrockscache` 目标构建。
- 保留必要的 Apache 2.0 上游版权与 NOTICE 归属；不要恢复旧项目品牌、官网、CI、发布、测试矩阵或社区文档。

## English

This repository is XRocksCache: a large-capacity, single-node K/V cache for low-cost cloud servers. It uses RocksDB for persistence and keeps Redis protocol compatibility for core string commands and basic administration.

### Project scope

- The server binary target is fixed as `xrockscache`.
- The default config file is `xrockscache.conf`; the 4C8G baseline config is `xrockscache-4c8g.conf`.
- The project focuses on String K/V cache workloads. Hash, List, Set, ZSet, Stream, JSON, Bloom, Search, Cluster, Replication, Lua, and RDB import/export are out of scope.
- `SET`-style writes must follow cache constraints: maximum key size 512 KiB, maximum value size 1 MiB, and maximum TTL 15 days.
- The performance baseline targets low-cost 2C4G / 4C8G cloud servers and prioritizes keeping request latency within 100 ms at 10k QPS.

### Build

```bash
cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release -DDISABLE_JEMALLOC=ON
cmake --build build --target xrockscache -j4
```

If Ninja is unavailable, use the default generator:

```bash
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DDISABLE_JEMALLOC=ON
cmake --build build --target xrockscache -j4
```

### Local run

```bash
./build/xrockscache -c xrockscache.conf
```

### Development rules

- Keep code, command names, and config keys in English; all new user-facing documents must be bilingual Chinese and English.
- Prefer small, reviewable patches. Do not reintroduce removed complex data structures or distributed features.
- After changing storage, commands, or config constraints, run at least one `xrockscache` target build.
- Keep the required Apache 2.0 upstream copyright and NOTICE attribution. Do not restore old project branding, website links, CI, release scripts, test matrix, or community documents.
