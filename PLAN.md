# XRocksCache Implementation Plan / XRocksCache 实施计划

## 中文

### 产品边界

XRocksCache 是轻量级、单机、Redis 协议兼容的 RocksDB K/V 缓存。它是缓存，不是主数据库。

- 一个进程、一个本地 RocksDB、一个本地 SSD。
- 只保留字符串 key/value 与 TTL 行为。
- key 最大 512KiB，value 最大 1MiB。
- 所有成功写入的 value 最长 15 天过期；缺省 TTL 和超过 15 天的 TTL 都会被收敛到 15 天。
- 追求 Redis 客户端兼容，不追求完整 Redis 功能兼容。
- 不提供集群、复制、namespace 隔离、事务、Lua、Pub/Sub、搜索、复杂数据结构。
- 节点可丢弃；缓存 miss 后的数据恢复由调用方负责。

### 交付里程碑

1. 精简服务端：只保留 K/V、TTL、基础 server/info/auth/ping 能力，服务端二进制为 `xrockscache`。
2. 存储瘦身：只保留 `default` 与 `metadata` column family，移除复杂结构、脚本、搜索、迁移与旧测试路径。
3. 配置固化：启用 `xrockscache-profile yes` 时强制 key/value/TTL 边界，并拒绝与单机缓存定位冲突的配置。
4. 压测验证：使用 `benchmark/` 对 2c4g 与 4c8g 机器做 100GiB 级容量、开放环 1w QPS、100ms 延迟门槛验证。
5. 发布准备：补齐双语 README、安全策略、威胁模型、压测报告模板与容器运行说明。

### V1 命令面

`PING`、`ECHO`、`GET`、`SET`、`MGET`、`MSET`、`DEL`、`EXISTS`、`EXPIRE`、`PEXPIRE`、`TTL`、`PTTL`、`INCR`、`INCRBY`、`DECR`、`DECRBY`、`DBSIZE`、`INFO`、`AUTH`、`COMMAND`、`HELLO`、`CLIENT`。

其他命令返回不支持或未知命令错误。

### 验收门槛

- 机器：低成本 2c4g 与 4c8g。
- 数据集：目标 100GiB；容量不足时先以 10GiB/50GiB/70GiB 递进验证。
- 主场景：开放环 10,000 QPS，95% GET / 5% SET，Zipfian key 分布，1KiB 随机 value。
- GET p99 与 SET p99 均不超过 100ms，且无错误、无非预期 miss、无 OOM、无持续 write stall。
- 512KiB key 与 1MiB value 是正确性/稳定性边界，不要求在该边界承载 1w QPS。

## English

### Product scope

XRocksCache is a lightweight, single-node, Redis-protocol-compatible RocksDB K/V cache. It is a cache, not a primary database.

- One process, one local RocksDB, one local SSD.
- String key/value operations with TTL semantics only.
- Maximum key size: 512KiB; maximum value size: 1MiB.
- Every successful value write expires no later than 15 days; missing TTLs and longer TTLs are clamped to 15 days.
- Redis client compatibility is the goal; full Redis feature compatibility is not.
- No cluster, replication, namespace isolation, transactions, Lua, Pub/Sub, search, or complex data structures.
- Nodes are disposable; cache-miss recovery belongs to callers.

### Delivery milestones

1. Minimal server: keep only K/V, TTL, basic server/info/auth/ping capabilities; the server binary is `xrockscache`.
2. Storage reduction: keep only `default` and `metadata` column families; remove complex structures, scripting, search, migration, and legacy tests.
3. Configuration guardrails: with `xrockscache-profile yes`, enforce key/value/TTL bounds and reject options that conflict with single-node cache scope.
4. Benchmark validation: use `benchmark/` to validate 100GiB-class capacity, open-loop 10k QPS, and 100ms latency on 2c4g and 4c8g machines.
5. Release readiness: provide bilingual README, security policy, threat model, benchmark report template, and container instructions.

### V1 command surface

`PING`, `ECHO`, `GET`, `SET`, `MGET`, `MSET`, `DEL`, `EXISTS`, `EXPIRE`, `PEXPIRE`, `TTL`, `PTTL`, `INCR`, `INCRBY`, `DECR`, `DECRBY`, `DBSIZE`, `INFO`, `AUTH`, `COMMAND`, `HELLO`, `CLIENT`.

Other commands return unsupported-command or unknown-command errors.

### Acceptance gate

- Hosts: low-cost 2c4g and 4c8g instances.
- Dataset: target 100GiB; when capacity is constrained, validate progressively with 10GiB/50GiB/70GiB.
- Primary workload: open-loop 10,000 QPS, 95% GET / 5% SET, Zipfian key distribution, 1KiB random values.
- GET p99 and SET p99 must both stay within 100ms, with no errors, no unexpected misses, no OOM, and no sustained write stalls.
- The 512KiB key and 1MiB value limits are correctness/stability boundaries; they are not required to sustain 10k QPS.
