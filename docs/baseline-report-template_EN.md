# XRocksCache Baseline Report Template

Chinese documentation is available in [baseline-report-template.md](baseline-report-template.md).

## Decision

- Date:
- XRocksCache commit:
- RocksDB commit:
- Result: `GO` / `NO-GO` / `INCONCLUSIVE`
- Decision owner:
- Main reason:

## Environment

| Item | Value |
| --- | --- |
| CPU model / allocated cores | |
| RAM / swap | |
| SSD model / capacity | |
| Filesystem / mount options | |
| Kernel | |
| Container or VM limits | |
| Server config | |
| RocksDB build method | |
| Benchmark client machine | |
| Redis-compatible client version | |

## Fixed product limits

| Limit | Required value | Validation |
| --- | ---: | --- |
| Maximum key | 512KiB / 524,288 bytes | boundary and boundary + 1 |
| Maximum value | 5MiB / 5,242,880 bytes | boundary and boundary + 1 |
| Maximum write TTL | 15 days | short TTL, long TTL, `SET EX/PX`, `EXPIRE/PEXPIRE` |

## Dataset

| Dataset | Keys | Value size | On-disk size | TTL policy | Load time |
| ---: | ---: | ---: | ---: | --- | ---: |
| 10GiB | | | | | |
| 50GiB | | | | | |
| 70GiB | | | | | |
| 100GiB | | | | | |

## Workload results

| Dataset | Value | Read/write ratio | Distribution | Status | QPS | GET p50 | GET p95 | GET p99 | SET p50 | SET p99 | Errors |
| --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| | | | | | | | | | | | |

Primary gate: open-loop 10,000 QPS, 95% GET / 5% SET, Zipfian keys, 1KiB random values, with both GET and SET p99 no greater than 100ms.

## RocksDB and resource observations

| Run ID | CPU | RSS | Goroutines | GC pause p99 | Read IOPS | Write IOPS | Throughput | await | SST bytes | Blob bytes | Pending compaction bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| | | | | | | | | | | | |

## Conclusion and follow-up

- Bottleneck:
- Configuration-sensitive items:
- RocksDB compaction observations:
- Blob GC observations:
- Recovery / consistency observations:
- Must-fix items:
- Risks requiring long-duration stability testing:
