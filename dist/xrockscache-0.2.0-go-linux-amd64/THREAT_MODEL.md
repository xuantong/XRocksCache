# 威胁模型

英文文档见 [THREAT_MODEL_EN.md](THREAT_MODEL_EN.md)。

XRocksCache 的威胁模型以“低成本单机 K/V 缓存”为核心，不覆盖分布式数据库、消息系统或脚本执行平台。

## 受保护资产

- 缓存 key/value 数据
- 访问口令与运行配置
- RocksDB WAL、SST、blob files 和 MANIFEST 元数据
- 服务端进程可用性

## 可信边界

- 运维人员、部署脚本和主机文件系统被视为可信。
- 客户端网络输入不可信。
- 未配置上游 TLS 或安全网关时，传输链路不提供机密性保证。

## 主要威胁

- 未鉴权访问导致缓存数据被读取、覆盖或删除。
- 超大请求、热点 key 或开放环高 QPS 导致排队和延迟放大。
- 磁盘空间耗尽导致 RocksDB 写入、flush 或 compaction 失败。
- 大量写入或短 TTL 写入导致 pending compaction bytes 增长，进而触发 RocksDB slowdown 或 stall。
- 运行环境 RocksDB 版本不一致导致构建和运行行为差异。

## 缓解措施

- 生产环境启用 `requirepass`，或部署在私有网络/安全网关之后。
- 使用内置约束固定 key/value 大小和最大 TTL。
- 通过 RocksDB 自动调参限制 block cache、memtable、后台 compaction 并发和 rate limiter。
- 通过磁盘 warn / slowdown / reject 水位线保护主机磁盘。
- 使用 `benchmark/` 压测脚本和 `test/benchmark` 黑盒测试验证压测工具链。
- 对数据目录做磁盘告警，并观察 `rocksdb_pending_compaction_bytes`。

## 明确不在当前模型内

- 集群一致性、跨节点复制、slot migration
- Lua/脚本沙箱逃逸
- 搜索索引、Pub/Sub、复杂 Redis 数据结构
- 主机 root 权限被攻陷后的数据保护
