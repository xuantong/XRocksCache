# Spring Boot Redis client integration tests

`benchmark/spring-redis` pins Spring Boot 3.2.4, Spring Data Redis 3.2.4, Lettuce 6.3.2 and Java 17. This is a specific compatibility target, not a guarantee for every client version. Keep the `LettuceClientConfigurationBuilderCustomizer` setting `ProtocolVersion.RESP2`; only RESP2 and database 0 are supported.

## Run on Ubuntu 24.04

Prepare RocksDB source and build dependencies as described in `disk-capacity-test_EN.md`, then run from the updated project:

```bash
sudo apt-get install -y openjdk-17-jdk maven redis-tools python3
export ROCKSDB_DIR=/root/rocksdb
ROCKSDB_BUILD_JOBS=2 bash dev/build-ubuntu-24.04.sh && \
bash dev/test-rocksdb-wsl.sh -race && \
bash dev/test-spring-redis.sh
```

The entry point starts its own temporary database with a random password on loopback port 6692. Set `XRC_JAVA_PORT=6693` if occupied. Maven requires dependency downloads on first use. The suite has a 15-minute timeout and propagates failures; only success prints `JAVA_COMPAT_PASS`. Reports under `benchmark/results/java-it.*/` include Maven output, JUnit XML and server logs. Cleanup stops the server before removing its database; if shutdown fails, data is retained and the script fails. Never supply production connection settings.

## Coverage

| Commands | Assertions |
| --- | --- |
| PING, ECHO, AUTH, HELLO, QUIT | Echo, password/default-user authentication, wrong-password rejection and reauthentication, RESP2 handshake, close |
| GET, SET | Unicode, empty/binary values, 1MiB/5MiB, 512KiB key boundary, oversize rejection |
| SET options | EX, PX, NX, XX, GET, KEEPTTL, TTL ceiling |
| MGET, MSET | Batch values, missing keys, 64-key ceiling, request/response budgets, atomic rejection |
| DEL, EXISTS | Batch deletion, counts, missing keys |
| EXPIRE, PEXPIRE, TTL, PTTL | Seconds/milliseconds, immediate expiration, missing keys, default 15-day TTL |
| INCR, DECR, INCRBY, DECRBY | Arithmetic, concurrent atomicity, invalid integers and overflow |
| DBSIZE, INFO, SELECT, COMMAND | Count, advertised 5MiB limit, database 0, current empty command list |
| CLIENT SETINFO/SETNAME/GETNAME/ID | Current compatibility stubs: accept metadata/name, return null name and ID 0 |
| Spring path | Auto-configuration, StringRedisTemplate, binary RedisTemplate, pipeline and concurrent connection reuse |

The 10-second client timeout checks functionality, not a 100ms latency target. Large values can spend over 100ms in the 35MiB/s limiter alone. Coverage spans command entry points and important boundaries, not every argument permutation, restart recovery, network fault injection or 100GiB stability.

Invalid input, size limits and authentication failures must throw exceptions; these are expected rejections. Transactions, Lua, Cluster, Hash/List and other unsupported features must not be used. Spring Cache clearing may issue unsupported commands and needs separate workload-specific validation.
