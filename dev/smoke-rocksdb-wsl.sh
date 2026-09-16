#!/usr/bin/env bash
set -euo pipefail

# 该脚本用于在同一个 WSL/Linux 进程内完成最小真实运行验证。
# 这样可以避开 Windows 侧多次创建 WSL 实例带来的偶发连接问题。

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
DATA_DIR="$(mktemp -d /tmp/xrc_smoke.XXXXXX)"
PORT="${XRC_SMOKE_PORT:-6679}"
SERVER_BIN="${XRC_SERVER_BIN:-${PROJECT_DIR}/build/xrockscache}"
SERVER_LOG="${DATA_DIR}/server.log"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" >/dev/null 2>&1 || true
  fi
  if [[ "${XRC_SMOKE_KEEP_DATA:-0}" != "1" && "${DATA_DIR}" == /tmp/xrc_smoke.* ]]; then
    rm -rf "${DATA_DIR}"
  fi
}
trap cleanup EXIT

if ! command -v redis-cli >/dev/null 2>&1; then
  echo "redis-cli not found" >&2
  exit 1
fi

cd "${PROJECT_DIR}"

"${SERVER_BIN}" \
  -c xrockscache.conf \
  -dir "${DATA_DIR}" \
  -bind 127.0.0.1 \
  -port "${PORT}" \
  >"${SERVER_LOG}" 2>&1 &
SERVER_PID="$!"

READY=0
for _ in $(seq 1 50); do
  if redis-cli -h 127.0.0.1 -p "${PORT}" --raw PING >/dev/null 2>&1; then
    READY=1
    break
  fi
  if ! kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

if [[ "${READY}" != "1" ]]; then
  echo "服务未能在预期时间内启动，启动日志如下：" >&2
  cat "${SERVER_LOG}" >&2 || true
  exit 1
fi

run() {
  echo "> $*"
  redis-cli -h 127.0.0.1 -p "${PORT}" --raw "$@"
}

echo "data_dir=${DATA_DIR}"
echo "server_pid=${SERVER_PID}"

run PING
run SET smoke:hello world EX 60
run GET smoke:hello

run SET smoke:ttl alive EX 3
run GET smoke:ttl
run TTL smoke:ttl
sleep 4
run GET smoke:ttl
run TTL smoke:ttl

run SET smoke:persist 1
run DBSIZE
run INFO

echo "rocksdb_files="
find "${DATA_DIR}/rocksdb" -maxdepth 1 -type f -printf "%f %s\n" | sort

echo "server_log_tail="
tail -n 20 "${SERVER_LOG}"
