# Spring Boot Redis 客户端集成测试

测试工程位于 `benchmark/spring-redis`，固定 Spring Boot 3.2.4 / Spring Data Redis 3.2.4 / Lettuce 6.3.2 / Java 17。该版本组合是明确的测试目标，不代表所有客户端版本均已兼容。服务仅支持 RESP2、数据库 0；必须保留测试工程中的 `LettuceClientConfigurationBuilderCustomizer`，显式设置 `ProtocolVersion.RESP2`。

## 云端运行

更新源码后，在 Ubuntu 24.04 项目目录执行。先前已准备好 RocksDB 源码和编译依赖，详见 `disk-capacity-test.md`。

```bash
sudo apt-get install -y openjdk-17-jdk maven redis-tools python3
export ROCKSDB_DIR=/root/rocksdb
ROCKSDB_BUILD_JOBS=2 bash dev/build-ubuntu-24.04.sh && \
bash dev/test-rocksdb-wsl.sh -race && \
bash dev/test-spring-redis.sh
```

入口使用独立临时数据库、随机密码、仅本机监听的 6692 端口，不连接现有服务。端口冲突可设置 `XRC_JAVA_PORT=6693`。首次执行 Maven 需要下载依赖。测试最多执行 15 分钟；失败返回非零状态，只有全部通过才打印 `JAVA_COMPAT_PASS`。报告位于 `benchmark/results/java-it.*/`，包含 Maven 日志、JUnit XML 和服务日志。正常退出或失败后先停止服务再清理本轮数据库；若服务未退出则保留目录并报错。不要向测试工程传入生产连接参数。

## 覆盖范围

| 命令 | Java 验证 |
| --- | --- |
| PING、ECHO、AUTH、HELLO、QUIT | 消息回显、密码与 default 用户认证、错误密码拒绝与重新认证、RESP2 握手、关闭连接 |
| GET、SET | 中文、空值、二进制、1MiB/5MiB、512KiB key 边界、超限拒绝 |
| SET 选项 | EX、PX、NX、XX、GET、KEEPTTL；超长 TTL 拒绝 |
| MGET、MSET | 批量读写、缺失 key、64 key 限制、响应/请求预算拒绝、失败不产生部分写入 |
| DEL、EXISTS | 批量删除、存在数量、缺失 key |
| EXPIRE、PEXPIRE、TTL、PTTL | 秒/毫秒过期、立即删除、缺失 key、默认 15 天 TTL |
| INCR、DECR、INCRBY、DECRBY | 增减、并发原子性、非整数及溢出拒绝 |
| DBSIZE、INFO、SELECT、COMMAND | 数量、5MiB 能力信息、仅数据库 0、当前空命令列表 |
| CLIENT SETINFO/SETNAME/GETNAME/ID | 当前兼容占位行为：接受元数据/名称，名称返回空、ID 返回 0 |
| Spring 调用链 | 自动配置、StringRedisTemplate、RedisTemplate<String, byte[]>、管道与多线程连接复用 |

客户端超时配置为 10 秒以验证功能，不代表 100ms 性能目标通过。35MiB/s 下大 value 仅限速等待就可能超过 100ms。该套件验证命令入口与关键边界，不穷举所有参数排列，不替代重启恢复、网络故障注入及 100GiB 稳定性压测。

非法参数、超限、认证失败应抛出异常，这属于正确拒绝；不能以“没有任何异常”代替兼容性断言。未支持的事务、Lua、Cluster、Hash/List 等功能不能在业务中使用；Spring Cache 的清缓存操作也可能依赖未支持的命令，需要按实际调用另行验证。
