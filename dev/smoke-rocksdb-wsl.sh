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

# 只向本次启动的实例写入测试 key，绝不复用端口上已有的服务。
if [[ ! "${PORT}" =~ ^[0-9]+$ ]] || (( PORT < 1 || PORT > 65535 )); then
  echo "测试端口不合法" >&2
  exit 1
fi
if (exec 3<>"/dev/tcp/127.0.0.1/${PORT}") 2>/dev/null; then
  echo "测试端口已被占用，拒绝向已有服务写入测试数据" >&2
  exit 1
fi

"${SERVER_BIN}" \
  -c xrockscache.conf \
  -dir "${DATA_DIR}" \
  -bind 127.0.0.1 \
  -port "${PORT}" \
  >"${SERVER_LOG}" 2>&1 &
SERVER_PID="$!"

READY=0
for _ in $(seq 1 50); do
  if grep -q 'server listening' "${SERVER_LOG}" && [[ "$(redis-cli -h 127.0.0.1 -p "${PORT}" --raw PING 2>/dev/null)" == PONG ]]; then
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

# 必须断言返回值；redis-cli 默认不会把所有协议错误映射为非零退出码。
expect() {
  local expected="$1"
  shift
  local actual
  actual="$(redis-cli -h 127.0.0.1 -p "${PORT}" --raw "$@")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "命令 $1 返回不符合预期：${actual}" >&2
    exit 1
  fi
}

echo "data_dir=${DATA_DIR}"
echo "server_pid=${SERVER_PID}"

expect PONG PING
expect OK SET smoke:hello world EX 60
expect world GET smoke:hello

expect OK SET smoke:ttl alive EX 3
expect alive GET smoke:ttl
run TTL smoke:ttl
sleep 4
expect '' GET smoke:ttl
expect -2 TTL smoke:ttl

expect OK SET smoke:counter 41 PX 1
sleep 0.1
expect 1 INCRBY smoke:counter 1
expect 1 GET smoke:counter
counter_ttl="$(redis-cli -h 127.0.0.1 -p "${PORT}" --raw TTL smoke:counter)"
if [[ ! "${counter_ttl}" =~ ^[0-9]+$ ]] || (( counter_ttl < 1295940 || counter_ttl > 1296000 )); then
  echo "过期计数器重建后 TTL 异常：${counter_ttl}" >&2
  exit 1
fi

expect OK SET smoke:persist 1
run DBSIZE
run INFO

echo "rocksdb_files="
find "${DATA_DIR}/rocksdb" -maxdepth 1 -type f -printf "%f %s\n" | sort

echo "server_log_tail="
tail -n 20 "${SERVER_LOG}"

kill -TERM "${SERVER_PID}"
wait "${SERVER_PID}"
SERVER_PID=""
echo "真实命令与正常退出检查通过"
