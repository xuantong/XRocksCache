#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
xrcbench="${XRCBENCH:-${script_dir}/bin/xrcbench}"

addr="${XRC_ADDR:-127.0.0.1:6379}"
dataset_size="${DATASET_SIZE:-10GiB}"
value_size="${VALUE_SIZE:-1KiB}"
clients="${CLIENTS:-16}"
load_clients="${LOAD_CLIENTS:-8}"
pipeline="${PIPELINE:-64}"
duration="${DURATION:-5m}"
warmup="${WARMUP:-30s}"
report_interval="${REPORT_INTERVAL:-10s}"
read_ratios="${READ_RATIOS:-100 99 95 90 80}"
distributions="${DISTRIBUTIONS:-uniform zipfian}"
target_qps_values="${TARGET_QPS_VALUES:-0}"
load_dataset="${LOAD_DATASET:-yes}"
result_root="${RESULT_ROOT:-${script_dir}/results}"
run_id="${RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
result_dir="${result_root}/${run_id}_${dataset_size}_${value_size}"

if [[ ! -x "${xrcbench}" ]]; then
  echo "xrcbench is missing; run ${script_dir}/build.sh first" >&2
  exit 1
fi

mkdir -p "${result_dir}"
printf '%s\n' \
  "run_id=${run_id}" \
  "addr=${addr}" \
  "dataset_size=${dataset_size}" \
  "value_size=${value_size}" \
  "clients=${clients}" \
  "duration=${duration}" \
  "warmup=${warmup}" \
  "read_ratios=${read_ratios}" \
  "distributions=${distributions}" \
  "target_qps_values=${target_qps_values}" >"${result_dir}/case.env"

common_args=(
  --addr "${addr}"
  --dataset-size "${dataset_size}"
  --value-size "${value_size}"
  --value-pattern random
)

if [[ "${load_dataset}" == "yes" ]]; then
  echo "Loading ${dataset_size} with ${value_size} values. The server data directory must be fresh."
  "${xrcbench}" load \
    "${common_args[@]}" \
    --clients "${load_clients}" \
    --pipeline "${pipeline}" \
    --output "${result_dir}/load.json"
fi

for read_ratio in ${read_ratios}; do
  for distribution in ${distributions}; do
    for target_qps in ${target_qps_values}; do
      safe_qps="${target_qps//./_}"
      output="${result_dir}/run_r${read_ratio}_${distribution}_qps${safe_qps}.json"
      echo "Running read_ratio=${read_ratio} distribution=${distribution} target_qps=${target_qps}"
      "${xrcbench}" run \
        "${common_args[@]}" \
        --clients "${clients}" \
        --duration "${duration}" \
        --warmup "${warmup}" \
        --report-interval "${report_interval}" \
        --read-ratio "${read_ratio}" \
        --distribution "${distribution}" \
        --target-qps "${target_qps}" \
        --output "${output}"
    done
  done
done

echo "Results written to ${result_dir}"
