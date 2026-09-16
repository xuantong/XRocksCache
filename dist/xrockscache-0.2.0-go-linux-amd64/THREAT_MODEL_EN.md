# Threat Model

Chinese documentation is available in [THREAT_MODEL.md](THREAT_MODEL.md).

XRocksCache’s threat model is centered on a low-cost single-node K/V cache. It does not model a distributed database, message system, or script execution platform.

## Protected assets

- Cached key/value data
- Access secrets and runtime configuration
- RocksDB WAL, SST files, blob files, and MANIFEST metadata
- Server process availability

## Trust boundaries

- Operators, deployment scripts, and the host filesystem are trusted.
- Client network input is untrusted.
- Without upstream TLS or a secure gateway, the transport layer does not provide confidentiality.

## Main threats

- Unauthenticated access can read, overwrite, or delete cached data.
- Oversized requests, hot keys, or high open-loop QPS can amplify queuing and latency.
- Disk exhaustion can break RocksDB writes, flushes, or compaction.
- Heavy writes or short-TTL writes can increase pending compaction bytes and trigger RocksDB slowdown or stall.
- Inconsistent RocksDB runtime versions can cause build and runtime behavior differences.

## Mitigations

- Enable `requirepass` in production or deploy behind a private network/security gateway.
- Use built-in limits to enforce key/value size and maximum TTL.
- Use RocksDB auto-tuning to bound block cache, memtables, background compaction concurrency, and the rate limiter.
- Protect the host disk with warn / slowdown / reject watermarks.
- Use scripts under `benchmark/` and black-box tests under `test/benchmark` to validate the benchmark toolchain.
- Add disk alerts for the data directory and observe `rocksdb_pending_compaction_bytes`.

## Explicitly out of scope

- Cluster consistency, cross-node replication, slot migration
- Lua/script sandbox escape
- Search indexes, Pub/Sub, complex Redis data structures
- Data protection after host root compromise
