#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "Usage: $0 <server-pid> <output-dir> [data-dir]" >&2
  exit 2
fi

server_pid="$1"
output_dir="$2"
data_dir="${3:-}"
interval="${METRICS_INTERVAL:-1}"
xrc_host="${XRC_HOST:-127.0.0.1}"
xrc_port="${XRC_PORT:-6379}"

if [[ ! -r "/proc/${server_pid}/status" ]]; then
  echo "Cannot read process ${server_pid}" >&2
  exit 1
fi

mkdir -p "${output_dir}/info"
metrics_csv="${output_dir}/process.csv"
echo "epoch_ms,rss_kb,vm_kb,threads,cpu_ticks,read_bytes,write_bytes,data_used_kb,data_available_kb" >"${metrics_csv}"

while kill -0 "${server_pid}" 2>/dev/null; do
  epoch_ms="$(date +%s%3N)"
  rss_kb="$(awk '/^VmRSS:/ {print $2}' "/proc/${server_pid}/status")"
  vm_kb="$(awk '/^VmSize:/ {print $2}' "/proc/${server_pid}/status")"
  threads="$(awk '/^Threads:/ {print $2}' "/proc/${server_pid}/status")"
  cpu_ticks="$(awk '{print $14 + $15}' "/proc/${server_pid}/stat")"
  read_bytes="$(awk '/^read_bytes:/ {print $2}' "/proc/${server_pid}/io")"
  write_bytes="$(awk '/^write_bytes:/ {print $2}' "/proc/${server_pid}/io")"
  data_used_kb=""
  data_available_kb=""
  if [[ -n "${data_dir}" ]]; then
    read -r data_used_kb data_available_kb < <(df -Pk "${data_dir}" | awk 'NR == 2 {print $3, $4}')
  fi

  echo "${epoch_ms},${rss_kb:-0},${vm_kb:-0},${threads:-0},${cpu_ticks:-0},${read_bytes:-0},${write_bytes:-0},${data_used_kb:-0},${data_available_kb:-0}" >>"${metrics_csv}"

  if command -v redis-cli >/dev/null 2>&1; then
    if [[ -n "${XRC_PASSWORD:-}" ]]; then
      REDISCLI_AUTH="${XRC_PASSWORD}" redis-cli -h "${xrc_host}" -p "${xrc_port}" --no-auth-warning INFO ALL \
        >"${output_dir}/info/${epoch_ms}.txt" 2>"${output_dir}/info/${epoch_ms}.err" || true
    else
      redis-cli -h "${xrc_host}" -p "${xrc_port}" INFO ALL \
        >"${output_dir}/info/${epoch_ms}.txt" 2>"${output_dir}/info/${epoch_ms}.err" || true
    fi
  fi
  sleep "${interval}"
done
