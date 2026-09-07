# XRocksCache Agent Guide / XRocksCache 智能体协作指南

## 中文

本仓库当前主干已切换为 Go 实现。切换前的 C++ 精简版保存在本地 Git tag `release_tag_cpp_baseline_20260907`。

### 项目边界

- 服务端二进制目标固定为 `xrockscache`。
- 默认配置文件为 `xrockscache.conf`，4C8G 基线配置为 `xrockscache-4c8g.conf`。
- 项目聚焦单机 String K/V 缓存，不提供 Cluster、Replication、Lua、Search、Pub/Sub 或复杂 Redis 数据结构。
- key 最大 512KiB，value 最大 1MiB，写入 TTL 最长 15 天。
- 优先保证低成本 2C4G / 4C8G 云服务器上的可部署性、可压测性和 100ms 内延迟目标。

### 构建与测试

```bash
go build -trimpath -o build/xrockscache ./cmd/xrockscache
go test ./...
```

### 开发规则

- 用户文档保持中英文双语；代码标识符、命令、配置键保持英文。
- 不要重新引入 C++、CMake 或旧存储源码树。
- 存储层如果需要升级，应优先通过 `internal/store` 抽象扩展，不要影响 RESP 命令层。
- 修改命令协议或 TTL/大小限制后，必须补充或更新 Go 测试。

## English

The current main branch has been switched to a Go implementation. The minimal C++ baseline before the rewrite is preserved in the local Git tag `release_tag_cpp_baseline_20260907`.

### Project scope

- The server binary target is fixed as `xrockscache`.
- The default config file is `xrockscache.conf`; the 4C8G baseline config is `xrockscache-4c8g.conf`.
- The project focuses on single-node String K/V cache workloads. Cluster, Replication, Lua, Search, Pub/Sub, and complex Redis data structures are out of scope.
- Maximum key size is 512KiB, maximum value size is 1MiB, and maximum write TTL is 15 days.
- Prioritize deployability, benchmarkability, and sub-100ms latency on low-cost 2C4G / 4C8G cloud servers.

### Build and test

```bash
go build -trimpath -o build/xrockscache ./cmd/xrockscache
go test ./...
```

### Development rules

- Keep user-facing documents bilingual Chinese and English; keep code identifiers, commands, and config keys in English.
- Do not reintroduce C++, CMake, or the previous storage source tree.
- If the storage layer needs an upgrade, extend it through the `internal/store` abstraction without leaking storage details into the RESP command layer.
- When changing command protocol behavior or TTL/size limits, add or update Go tests.
