# XRocksCache Baseline Report Template / XRocksCache 基线报告模板

## 中文

### 决策

- 日期：
- XRocksCache commit：
- 结果：`GO` / `NO-GO` / `INCONCLUSIVE`
- 决策人：
- 主要原因：

### 环境

| 项目 | 值 |
| --- | --- |
| CPU 型号 / 核数 | |
| 内存 / swap | |
| SSD 型号 / 容量 | |
| 文件系统 / 挂载参数 | |
| 内核 | |
| 容器或 VM 限制 | |
| 服务端配置 | |
| 压测客户端机器 | |
| Redis 兼容客户端版本 | |

### 固定产品限制

| 限制 | 要求值 | 验证方式 |
| --- | ---: | --- |
| 最大 key | 512KiB / 524,288 bytes | 边界值与边界 + 1 |
| 最大 value | 1MiB / 1,048,576 bytes | 边界值与边界 + 1 |
| 最大写入 TTL | 15 天 | 无 TTL、短 TTL、长 TTL、`MSET`、`INCR/DECR`、`EXPIRE/PEXPIRE` |

### 数据集

| 数据集 | key 数 | value 大小 | 实际落盘 | TTL 策略 | 装载耗时 |
| ---: | ---: | ---: | ---: | --- | ---: |
| 10GiB | | | | | |
| 50GiB | | | | | |
| 70GiB | | | | | |
| 100GiB | | | | | |

### 工作负载结果

| 数据集 | value | 读写比 | 分布 | 状态 | QPS | GET p50 | GET p95 | GET p99 | SET p50 | SET p99 | 错误 |
| --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| | | | | | | | | | | | |

主验收场景：开放环 10,000 QPS、95% GET / 5% SET、Zipfian key、1KiB 随机 value，GET/SET p99 均不超过 100ms。

### 资源与 RocksDB 观测

| Run ID | CPU | RSS | Block-cache hit rate | Read IOPS | Write IOPS | Throughput | await | Compaction bytes | Write stall |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| | | | | | | | | | |

### 结论与跟进

- 瓶颈：
- 配置敏感项：
- 数据恢复/一致性观察：
- 必须修复项：
- 需要长稳压测的风险：

## English

### Decision

- Date:
- XRocksCache commit:
- Result: `GO` / `NO-GO` / `INCONCLUSIVE`
- Decision owner:
- Main reason:

### Environment

| Item | Value |
| --- | --- |
| CPU model / allocated cores | |
| RAM / swap | |
| SSD model / capacity | |
| Filesystem / mount options | |
| Kernel | |
| Container or VM limits | |
| Server configuration | |
| Benchmark client host | |
| Redis-compatible client version | |

### Fixed product limits

| Limit | Required value | Verification |
| --- | ---: | --- |
| Maximum key | 512KiB / 524,288 bytes | boundary and boundary + 1 |
| Maximum value | 1MiB / 1,048,576 bytes | boundary and boundary + 1 |
| Maximum write TTL | 15 days | missing TTL, shorter TTL, longer TTL, `MSET`, `INCR/DECR`, `EXPIRE/PEXPIRE` |

### Dataset

| Dataset | Key count | Value size | On-disk bytes | TTL policy | Load duration |
| ---: | ---: | ---: | ---: | --- | ---: |
| 10GiB | | | | | |
| 50GiB | | | | | |
| 70GiB | | | | | |
| 100GiB | | | | | |

### Workload results

| Dataset | Value | R/W | Distribution | State | QPS | GET p50 | GET p95 | GET p99 | SET p50 | SET p99 | Errors |
| --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| | | | | | | | | | | | |

Primary gate: open-loop 10,000 QPS, 95% GET / 5% SET, Zipfian keys, 1KiB random values, with both GET and SET p99 no greater than 100ms.

### Resource and RocksDB observations

| Run ID | CPU | RSS | Block-cache hit rate | Read IOPS | Write IOPS | Throughput | await | Compaction bytes | Write stall |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| | | | | | | | | | |

### Findings and follow-ups

- Bottleneck:
- Configuration sensitivity:
- Data recovery/consistency observations:
- Required fixes:
- Risks that need a longer soak test:
