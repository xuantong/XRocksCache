#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
PROJECT_DIR="$PWD"
RUN_DIR="$(mktemp -d /tmp/xrc_capacity_java.XXXXXX)"
REPORT_DIR="${PROJECT_DIR}/benchmark/results/$(basename "${RUN_DIR}")"
mkdir -p "${REPORT_DIR}"
PORT=6687
SERVER_PID=""
MONITOR_PID=""
cleanup() {
  if [[ -n "${MONITOR_PID}" ]]; then kill "${MONITOR_PID}" 2>/dev/null || true; wait "${MONITOR_PID}" 2>/dev/null || true; fi
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill -TERM "${SERVER_PID}"; wait "${SERVER_PID}" || true
  fi
  if [[ -d "${RUN_DIR}/rocksdb" ]]; then
    cp "${RUN_DIR}"/rocksdb/LOG* "${REPORT_DIR}/" 2>/dev/null || true
    cp "${RUN_DIR}"/rocksdb/OPTIONS* "${REPORT_DIR}/" 2>/dev/null || true
  fi
  local resolved
  resolved="$(realpath -e -- "${RUN_DIR}")"
  if [[ "${resolved}" == /tmp/xrc_capacity_java.* && "$(dirname "${resolved}")" == /tmp && ! -L "${RUN_DIR}" ]]; then
    rm -rf -- "${resolved}"
  fi
}
trap cleanup EXIT
if (exec 3<>/dev/tcp/127.0.0.1/${PORT}) 2>/dev/null; then
  echo '测试端口已占用'; exit 1
fi
echo "data_dir=${RUN_DIR}" | tee "${REPORT_DIR}/environment.txt"
echo "report_dir=${REPORT_DIR}" | tee -a "${REPORT_DIR}/environment.txt"
df -h "${RUN_DIR}" >> "${REPORT_DIR}/environment.txt"
free -h >> "${REPORT_DIR}/environment.txt"
javac -d "${REPORT_DIR}" benchmark/java/CapacityCheck.java
start_server() {
  build/xrockscache -c xrockscache-4c8g.conf -dir "${RUN_DIR}" -bind 127.0.0.1 -port "${PORT}" >> "${REPORT_DIR}/server.log" 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 100); do
    if [[ "$(redis-cli -p "${PORT}" --raw PING 2>/dev/null || true)" == PONG ]]; then return; fi
    kill -0 "${SERVER_PID}" || return 1
    sleep 0.1
  done
  return 1
}
start_server
(
  while kill -0 "${SERVER_PID}" 2>/dev/null; do
    date -Ins
    redis-cli -p "${PORT}" --raw INFO
    cat "/proc/${SERVER_PID}/io" "/proc/${SERVER_PID}/status" /proc/diskstats /proc/pressure/io
    sleep 1
  done
) > "${REPORT_DIR}/timeline.log" 2>&1 &
MONITOR_PID=$!
java -Xmx256m -cp "${REPORT_DIR}" CapacityCheck "${PORT}" 107374182400 "${REPORT_DIR}/samples.txt" | tee "${REPORT_DIR}/load.log"
kill "${MONITOR_PID}" 2>/dev/null || true
wait "${MONITOR_PID}" 2>/dev/null || true
MONITOR_PID=""
cp "${RUN_DIR}"/rocksdb/LOG* "${REPORT_DIR}/"
cp "${RUN_DIR}"/rocksdb/OPTIONS* "${REPORT_DIR}/"
ps -p "${SERVER_PID}" -o pid,rss,vsz,pcpu,etime > "${REPORT_DIR}/resources.txt"
kill -TERM "${SERVER_PID}"
wait "${SERVER_PID}"
SERVER_PID=""
du -sh "${RUN_DIR}" | tee "${REPORT_DIR}/disk.txt"
start_server
java -Xmx256m -cp "${REPORT_DIR}" CapacityCheck "${PORT}" 107374182400 "${REPORT_DIR}/samples.txt" verify | tee "${REPORT_DIR}/restart-verify.log"
java -Xmx256m -cp "${REPORT_DIR}" CapacityCheck "${PORT}" 107374182400 "${REPORT_DIR}/samples.txt" latency | tee "${REPORT_DIR}/read-latency.log"
echo CAPACITY_AND_RESTART_PASS | tee "${REPORT_DIR}/status.txt"
