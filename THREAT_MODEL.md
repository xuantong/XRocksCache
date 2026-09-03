# Threat Model / 威胁模型

## 中文

XRocksCache 的威胁模型以“低成本单机 K/V 缓存”为核心，不覆盖分布式数据库、消息系统或脚本执行平台。

受保护资产：

- 缓存 key/value 数据
- 访问口令与运行配置
- RocksDB 数据目录、WAL、日志与备份目录
- 服务端进程可用性

可信边界：

- 运维人员、部署脚本和主机文件系统被视为可信。
- 客户端网络输入不可信。
- 未配置 TLS 时，传输链路不提供机密性保证。

主要威胁：

- 未鉴权访问导致缓存数据被读取、覆盖或删除。
- 超大请求、热点 key 或开放环高 QPS 导致排队和延迟放大。
- 磁盘空间耗尽导致写入失败或 RocksDB compaction 受阻。
- 错误配置导致日志、WAL、SST 文件占用超过机器承载能力。

缓解措施：

- 生产环境启用 `requirepass` 或部署在私有网络内。
- 使用 `xrockscache-profile yes` 固化 key/value 大小和最大 TTL。
- 使用 `max-db-size` 约束 RocksDB 主数据体积，为 WAL、日志和系统空间预留余量。
- 用 `benchmark/` 压测脚本验证 2c4g/4c8g 的安全 QPS 上沿。
- 对数据目录做磁盘告警和定期容量巡检。

明确不在当前模型内：

- 集群一致性、跨节点复制、slot migration
- Lua/脚本沙箱逃逸
- 搜索索引、Pub/Sub、复杂 Redis 数据结构
- 主机 root 权限被攻陷后的数据保护

## English

XRocksCache’s threat model is centered on a low-cost single-node K/V cache. It does not model a distributed database, message system, or script execution platform.

Protected assets:

- Cached key/value data
- Access secrets and runtime configuration
- RocksDB data directory, WAL, logs, and backup directories
- Server process availability

Trust boundaries:

- Operators, deployment scripts, and the host filesystem are trusted.
- Client network input is untrusted.
- Without TLS, the transport layer does not provide confidentiality.

Main threats:

- Unauthenticated access can read, overwrite, or delete cached data.
- Oversized requests, hot keys, or high open-loop QPS can amplify queuing and latency.
- Disk exhaustion can break writes or block RocksDB compaction.
- Misconfiguration can let logs, WAL, and SST files exceed host capacity.

Mitigations:

- Enable `requirepass` in production or deploy inside a private network.
- Use `xrockscache-profile yes` to enforce key/value size and maximum TTL.
- Use `max-db-size` to cap RocksDB primary data and reserve space for WAL, logs, and the OS.
- Use scripts under `benchmark/` to validate safe QPS limits for 2c4g/4c8g targets.
- Add disk alerts and periodic capacity checks for the data directory.

Explicitly out of scope:

- Cluster consistency, cross-node replication, slot migration
- Lua/script sandbox escape
- Search indexes, Pub/Sub, complex Redis data structures
- Data protection after host root compromise
