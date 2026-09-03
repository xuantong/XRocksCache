#!/usr/bin/env python3
import argparse
import csv
import json
import sys
from pathlib import Path


FIELDS = [
    "status",
    "gate_status",
    "path",
    "dataset_bytes",
    "value_bytes",
    "keys",
    "read_ratio",
    "distribution",
    "target_qps",
    "clients",
    "attempts",
    "successful_operations",
    "attempted_qps",
    "qps",
    "errors",
    "get_misses",
    "get_hit_ratio",
    "get_count",
    "get_p50_ms",
    "get_p95_ms",
    "get_p99_ms",
    "get_p999_ms",
    "get_max_ms",
    "get_worst_interval_p99_ms",
    "set_count",
    "set_p50_ms",
    "set_p95_ms",
    "set_p99_ms",
    "set_p999_ms",
    "set_max_ms",
]


def nested(data, *keys, default=""):
    current = data
    for key in keys:
        if not isinstance(current, dict) or key not in current:
            return default
        current = current[key]
    return current


def classify(data, minimum_hit_ratio):
    if data.get("errors", 0) > 0:
        return "INVALID_ERRORS"
    get_count = nested(data, "get", "count", default=0)
    if get_count == 0:
        return "INVALID_NO_GET_SAMPLES"
    if data.get("get_hit_ratio", 0) < minimum_hit_ratio:
        return "INVALID_MISSES"
    return "OK"


def classify_gate(data, base_status):
    config = data.get("config", {})
    is_gate = (
        config.get("target_qps") == 10000
        and config.get("read_ratio") == 95
        and config.get("distribution") == "zipfian"
        and config.get("value_bytes") == 1024
    )
    if not is_gate:
        return "NOT_GATE"
    if base_status != "OK":
        return "FAIL_INVALID"
    if data.get("attempted_qps", 0) < 9900 or data.get("qps", 0) < 9900:
        return "FAIL_QPS"
    for interval in data.get("intervals", []):
        if nested(interval, "get", "p99_ms", default=0) > 100:
            return "FAIL_GET_P99"
        if nested(interval, "set", "count", default=0) > 0 and nested(interval, "set", "p99_ms", default=0) > 100:
            return "FAIL_SET_P99"
    return "PASS"


def make_row(path, data, minimum_hit_ratio):
    intervals = data.get("intervals", [])
    interval_p99 = [nested(item, "get", "p99_ms", default=0) for item in intervals]
    base_status = classify(data, minimum_hit_ratio)
    return {
        "status": base_status,
        "gate_status": classify_gate(data, base_status),
        "path": str(path),
        "dataset_bytes": nested(data, "config", "logical_bytes"),
        "value_bytes": nested(data, "config", "value_bytes"),
        "keys": nested(data, "config", "keys"),
        "read_ratio": nested(data, "config", "read_ratio"),
        "distribution": nested(data, "config", "distribution"),
        "target_qps": nested(data, "config", "target_qps", default=0),
        "clients": nested(data, "config", "clients"),
        "attempts": data.get("attempts", ""),
        "successful_operations": data.get("successful_operations", ""),
        "attempted_qps": data.get("attempted_qps", ""),
        "qps": data.get("qps", ""),
        "errors": data.get("errors", ""),
        "get_misses": data.get("get_misses", ""),
        "get_hit_ratio": data.get("get_hit_ratio", ""),
        "get_count": nested(data, "get", "count"),
        "get_p50_ms": nested(data, "get", "p50_ms"),
        "get_p95_ms": nested(data, "get", "p95_ms"),
        "get_p99_ms": nested(data, "get", "p99_ms"),
        "get_p999_ms": nested(data, "get", "p999_ms"),
        "get_max_ms": nested(data, "get", "max_ms"),
        "get_worst_interval_p99_ms": max(interval_p99, default=0),
        "set_count": nested(data, "set", "count"),
        "set_p50_ms": nested(data, "set", "p50_ms"),
        "set_p95_ms": nested(data, "set", "p95_ms"),
        "set_p99_ms": nested(data, "set", "p99_ms"),
        "set_p999_ms": nested(data, "set", "p999_ms"),
        "set_max_ms": nested(data, "set", "max_ms"),
    }


def main():
    parser = argparse.ArgumentParser(description="Summarize xrcbench run JSON files as CSV")
    parser.add_argument("result_dir", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--minimum-hit-ratio", type=float, default=1.0)
    args = parser.parse_args()

    if not 0 <= args.minimum_hit_ratio <= 1:
        parser.error("--minimum-hit-ratio must be between 0 and 1")

    if args.result_dir.is_file():
        result_paths = [args.result_dir]
    else:
        result_paths = sorted(args.result_dir.rglob("run_*.json"))

    rows = []
    for path in result_paths:
        with path.open("r", encoding="utf-8") as handle:
            data = json.load(handle)
        if data.get("tool") != "xrcbench" or data.get("mode") != "run":
            continue
        rows.append(make_row(path, data, args.minimum_hit_ratio))

    output = args.output.open("w", newline="", encoding="utf-8") if args.output else sys.stdout
    try:
        writer = csv.DictWriter(output, fieldnames=FIELDS)
        writer.writeheader()
        writer.writerows(rows)
    finally:
        if args.output:
            output.close()


if __name__ == "__main__":
    main()
