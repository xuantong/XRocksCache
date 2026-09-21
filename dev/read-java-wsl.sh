#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
DATA_DIR=/tmp/xrc_capacity_java.coSavS
REPORT_DIR=benchmark/results/xrc_capacity_java.coSavS
PORT=6687
[[ -d "${DATA_DIR}/rocksdb" && -s "${REPORT_DIR}/samples.txt" ]]
if (exec 3<>/dev/tcp/127.0.0.1/${PORT}) 2>/dev/null; then
  echo '测试端口已占用'; exit 1
fi
javac -d "${REPORT_DIR}" benchmark/java/CapacityCheck.java
build/xrockscache -c xrockscache.conf -dir "${DATA_DIR}" -bind 127.0.0.1 -port "${PORT}" > "${REPORT_DIR}/read-server.log" 2>&1 &
SERVER_PID=$!
cleanup() { if kill -0 "${SERVER_PID}" 2>/dev/null; then kill -TERM "${SERVER_PID}"; wait "${SERVER_PID}" || true; fi; }
trap cleanup EXIT
READY=0
for _ in $(seq 1 100); do
  if [[ "$(redis-cli -p "${PORT}" --raw PING 2>/dev/null || true)" == PONG ]]; then READY=1; break; fi
  kill -0 "${SERVER_PID}"
  sleep 0.1
done
[[ "${READY}" == 1 ]]
for phase in first repeat; do
  java -Xmx256m -cp "${REPORT_DIR}" CapacityCheck "${PORT}" 107374182400 "${REPORT_DIR}/samples.txt" latency | tee "${REPORT_DIR}/read-latency-${phase}.log"
done
