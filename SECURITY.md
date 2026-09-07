# Security Policy / 安全策略

## 中文

XRocksCache 当前定位为轻量级单机 K/V 缓存，默认应部署在可信内网或安全网关之后。

安全边界：

- 不应在公网暴露未鉴权端口。
- 生产环境必须设置 `requirepass`，或通过上游网关、私有网络、安全组限制访问。
- 当前 Go 版数据文件默认不做应用层加密；如需静态数据保护，请使用云盘加密、文件系统加密或主机侧加密。
- 当前版本不包含集群、复制、Lua、搜索、Pub/Sub 等高风险扩展面。
- 当前运行时代码仅使用 Go 标准库，没有额外第三方运行时依赖面。

漏洞反馈：

- 请通过仓库 Issue 或维护者指定的私有渠道报告安全问题。
- 报告中请包含复现步骤、影响版本、配置片段和最小化验证命令。
- 在修复方案可用前，请不要公开可直接利用的攻击细节。

## English

XRocksCache is currently scoped as a lightweight single-node K/V cache. It should be deployed inside a trusted private network or behind a secure gateway by default.

Security boundaries:

- Do not expose an unauthenticated port directly to the public internet.
- Production deployments must set `requirepass` or restrict access through an upstream gateway, private network, or security group.
- The current Go data files are not encrypted by the application by default. Use cloud disk encryption, filesystem encryption, or host-level encryption when data-at-rest protection is required.
- The current version does not include cluster, replication, Lua, search, Pub/Sub, or other high-risk extension surfaces.
- The current runtime code uses only the Go standard library and has no extra third-party runtime dependency surface.

Reporting vulnerabilities:

- Report security issues through repository Issues or a private maintainer-designated channel.
- Include reproduction steps, affected version, relevant configuration snippets, and minimal verification commands.
- Avoid publishing directly exploitable details in public channels until a fix is available.
