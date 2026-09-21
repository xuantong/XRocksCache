#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
DATA_DIR="${XRC_SWEEP_DATA_DIR:-$(mktemp -d /tmp/xrc_rate_sweep.XXXXXX)}"
RESULT="${XRC_SWEEP_RESULT:-benchmark/results/rate-sweep-$(date +%Y%m%d-%H%M%S)}"
PORT="${XRC_SWEEP_PORT:-6691}"
DATASET="${XRC_SWEEP_DATASET:-1GiB}"
RATES="${XRC_SWEEP_RATES:-10,20,30,35,40,50}"
DISK_TYPE="${XRC_DISK_TYPE:-cloud_essd}"; DISK_PL="${XRC_DISK_PL:-pl1}"; DISK_CAPACITY_GIB="${XRC_DISK_CAPACITY_GIB:-100}"
[[ "${DISK_TYPE}" == cloud_essd && "${DISK_PL}" == pl1 && "${DISK_CAPACITY_GIB}" -ge 20 ]] || { echo '仅支持 ESSD PL1，且容量至少 20GiB' >&2; exit 1; }
mkdir -p "${RESULT}"
case "${DATASET^^}" in
  *GIB) TARGET_BYTES=$(( ${DATASET%[Gg][Ii][Bb]} * 1073741824 )) ;;
  *MIB) TARGET_BYTES=$(( ${DATASET%[Mm][Ii][Bb]} * 1048576 )) ;;
  *B) TARGET_BYTES=${DATASET%B} ;;
  *) echo "不支持的 XRC_SWEEP_DATASET：${DATASET}" >&2; exit 1 ;;
esac
SERVER_PID=""
cleanup() { if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then kill -TERM "${SERVER_PID}"; wait "${SERVER_PID}" || true; fi; if [[ "${XRC_KEEP_SWEEP_DATA:-0}" != 1 && "${DATA_DIR}" == /tmp/xrc_rate_sweep.* ]]; then rm -rf -- "$(realpath -e "${DATA_DIR}")"; fi; }
trap cleanup EXIT
javac -d "${RESULT}" benchmark/java/CapacityCheck.java
for rate in ${RATES//,/ }; do
  cfg="${RESULT}/xrockscache-${rate}.conf"
  sed "s/^write-rate-mib .*/write-rate-mib ${rate}/; s/^port .*/port ${PORT}/; s/^disk-type .*/disk-type ${DISK_TYPE}/; s/^disk-pl .*/disk-pl ${DISK_PL}/; s/^disk-capacity-gib .*/disk-capacity-gib ${DISK_CAPACITY_GIB}/" xrockscache-4c8g.conf > "${cfg}"
  build/xrockscache -c "${cfg}" -dir "${DATA_DIR}" -bind 127.0.0.1 -port "${PORT}" > "${RESULT}/server-${rate}.log" 2>&1 & SERVER_PID=$!
  for _ in $(seq 1 100); do [[ "$(redis-cli -p "${PORT}" --raw PING 2>/dev/null || true)" == PONG ]] && break; kill -0 "${SERVER_PID}"; sleep .1; done
  timeout "${XRC_SWEEP_TIMEOUT:-180}" java -Xmx256m -cp "${RESULT}" CapacityCheck "${PORT}" "${TARGET_BYTES}" "${RESULT}/samples-${rate}.txt" > "${RESULT}/rate-${rate}.log" 2>&1 || true
  redis-cli -p "${PORT}" --raw INFO > "${RESULT}/info-${rate}.txt" || true
  kill -TERM "${SERVER_PID}" 2>/dev/null || true; wait "${SERVER_PID}" 2>/dev/null || true; SERVER_PID=""
  rm -rf -- "${DATA_DIR}/rocksdb"
done
echo "结果已保存：${RESULT}"
