# Security Policy

Chinese documentation is available in [SECURITY.md](SECURITY.md).

XRocksCache is currently scoped as a lightweight single-node K/V cache. It should be deployed inside a trusted private network or behind a secure gateway by default.

## Security boundaries

- Do not expose an unauthenticated port directly to the public internet.
- Production deployments must set `requirepass` or restrict access through an upstream gateway, private network, or security group.
- The current Go data files are not encrypted by the application by default. Use cloud disk encryption, filesystem encryption, or host-level encryption when data-at-rest protection is required.
- The current version does not include cluster, replication, Lua, search, Pub/Sub, or other high-risk extension surfaces.
- Production storage uses RocksDB; runtime dependencies should be pinned through the release package or container image.

## Reporting vulnerabilities

- Report security issues through repository Issues or a private maintainer-designated channel.
- Include reproduction steps, affected version, relevant configuration snippets, and minimal verification commands.
- Avoid publishing directly exploitable details in public channels until a fix is available.
