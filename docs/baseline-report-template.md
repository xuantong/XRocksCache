# XRocksCache 基线报告模板

英文文档见 [baseline-report-template_EN.md](baseline-report-template_EN.md)。

## 决策

- 日期：
- XRocksCache commit：
- RocksDB commit：
- 结果：`GO` / `NO-GO` / `INCONCLUSIVE`
- 决策人：
- 主要原因：

## 环境

| 项目 | 值 |
| --- | --- |
| CPU 型号 / 核数 | |
| 内存 / swap | |
| SSD 型号 / 容量 | |
| 文件系统 / 挂载参数 | |
| 内核 | |
| 容器或 VM 限制 | |
| 服务端配置 | |
| RocksDB 构建方式 | |
| 压测客户端机器 | |
| Redis 兼容客户端版本 | |

## 固定产品限制

| 限制 | 要求值 | 验证方式 |
| --- | ---: | --- |
| 最大 key | 512KiB / 524,288 bytes | 边界值与边界 + 1 |
| 最大 value | 5MiB / 5,242,880 bytes | 边界值与边界 + 1 |
| 最大写入 TTL | 15 天 | 短 TTL、长 TTL、`SET EX/PX`、`EXPIRE/PEXPIRE` |

## 数据集

| 数据集 | key 数 | value 大小 | 实际落盘 | TTL 策略 | 装载耗时 |
| ---: | ---: | ---: | ---: | --- | ---: |
| 10GiB | | | | | |
| 50GiB | | | | | |
| 70GiB | | | | | |
| 100GiB | | | | | |

## 工作负载结果

| 数据集 | value | 读写比 | 分布 | 状态 | QPS | GET p50 | GET p95 | GET p99 | SET p50 | SET p99 | 错误 |
| --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| | | | | | | | | | | | |

主验收场景：开放环 10,000 QPS、95% GET / 5% SET、Zipfian key、1KiB 随机 value，GET/SET p99 均不超过 100ms。

## RocksDB 与资源观测

| Run ID | CPU | RSS | Goroutines | GC pause p99 | Read IOPS | Write IOPS | Throughput | await | SST bytes | Blob bytes | Pending compaction bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| | | | | | | | | | | | |

## 结论与跟进

- 瓶颈：
- 配置敏感项：
- RocksDB compaction 观察：
- Blob GC 观察：
- 数据恢复/一致性观察：
- 必须修复项：
- 需要长稳压测的风险：
