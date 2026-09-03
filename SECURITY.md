# Security Policy / 安全策略

## 中文

XRocksCache 当前定位为轻量级单机 K/V 缓存，默认应部署在可信内网或通过安全网关暴露服务。

安全边界：

- 不应在公网裸露未鉴权端口。
- 生产环境必须设置 `requirepass` 或使用上游网关/私有网络限制访问。
- RocksDB 数据目录默认不做应用层加密；如需数据静态加密，请使用云盘加密、文件系统加密或主机侧加密能力。
- 当前精简版不包含集群、复制、Lua、搜索、Pub/Sub 等高风险扩展面。

漏洞反馈：

- 请通过仓库 Issue 或维护者指定的私有渠道报告安全问题。
- 报告中请包含复现步骤、影响版本、配置文件片段和最小化验证命令。
- 请不要在公开渠道发布可直接利用的攻击细节，直到修复方案可用。

## English

XRocksCache is currently scoped as a lightweight single-node K/V cache. It should be deployed inside a trusted private network or behind a secure gateway by default.

Security boundaries:

- Do not expose an unauthenticated port directly to the public internet.
- Production deployments must set `requirepass` or restrict access through an upstream gateway/private network.
- RocksDB data directories are not encrypted by the application by default. Use cloud disk encryption, filesystem encryption, or host-level encryption when data-at-rest protection is required.
- The current minimal edition does not include cluster, replication, Lua, search, Pub/Sub, or other high-risk extension surfaces.

Reporting vulnerabilities:

- Report security issues through repository Issues or a private maintainer-designated channel.
- Include reproduction steps, affected version, relevant configuration snippets, and minimal verification commands.
- Avoid publishing directly exploitable details in public channels until a fix is available.
