# Threat Model / 威胁模型

## 中文

XRocksCache 的威胁模型以“低成本单机 K/V 缓存”为核心，不覆盖分布式数据库、消息系统或脚本执行平台。

受保护资产：

- 缓存 key/value 数据
- 访问口令与运行配置
- Go 版追加日志数据文件 `xrockscache.aof`
- 服务端进程可用性

可信边界：

- 运维人员、部署脚本和主机文件系统被视为可信。
- 客户端网络输入不可信。
- 未配置上游 TLS 或安全网关时，传输链路不提供机密性保证。

主要威胁：

- 未鉴权访问导致缓存数据被读取、覆盖或删除。
- 超大请求、热点 key 或开放环高 QPS 导致排队和延迟放大。
- 磁盘空间耗尽导致追加日志写入失败。
- 数据文件长期追加导致磁盘占用增长，需要后续引入压缩/重写机制。

缓解措施：

- 生产环境启用 `requirepass`，或部署在私有网络/安全网关之后。
- 使用内置约束固定 key/value 大小和最大 TTL。
- 使用 `benchmark/` 压测脚本验证 2C4G / 4C8G 的安全 QPS 上沿。
- 对数据目录做磁盘告警和定期容量巡检。
- 后续容量压测通过后，优先补充 AOF rewrite 或可插拔存储后端。

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
- Go append-only data file `xrockscache.aof`
- Server process availability

Trust boundaries:

- Operators, deployment scripts, and the host filesystem are trusted.
- Client network input is untrusted.
- Without upstream TLS or a secure gateway, the transport layer does not provide confidentiality.

Main threats:

- Unauthenticated access can read, overwrite, or delete cached data.
- Oversized requests, hot keys, or high open-loop QPS can amplify queuing and latency.
- Disk exhaustion can break append-only log writes.
- Long-running append-only files can keep growing and require a future rewrite/compaction mechanism.

Mitigations:

- Enable `requirepass` in production or deploy behind a private network/security gateway.
- Use built-in limits to enforce key/value size and maximum TTL.
- Use scripts under `benchmark/` to validate safe QPS limits for 2C4G / 4C8G targets.
- Add disk alerts and periodic capacity checks for the data directory.
- After capacity validation, prioritize AOF rewrite or a pluggable storage backend.

Explicitly out of scope:

- Cluster consistency, cross-node replication, slot migration
- Lua/script sandbox escape
- Search indexes, Pub/Sub, complex Redis data structures
- Data protection after host root compromise
