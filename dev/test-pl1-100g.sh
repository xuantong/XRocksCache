#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
if [[ "${1:-}" == --help ]]; then
  echo '用法：bash dev/test-pl1-100g.sh'
  echo '默认：/data/xrc-bench，PL1 300GiB，100GiB 数据，35MiB/s，端口 6691。'
  echo '可选：XRC_TEST_ROOT、XRC_DISK_CAPACITY_GIB、XRC_TEST_PORT。'
  exit 0
fi
for tool in java javac redis-cli ss timeout realpath df awk mktemp; do command -v "${tool}" >/dev/null; done
test -x build/xrockscache
test -f benchmark/java/CapacityCheck.java
test -f xrockscache-4c8g.conf
PORT="${XRC_TEST_PORT:-6691}"
CAPACITY="${XRC_DISK_CAPACITY_GIB:-300}"
[[ "${PORT}" =~ ^[0-9]{1,5}$ && "${CAPACITY}" =~ ^[0-9]{1,5}$ ]] || exit 1
(( 10#${PORT} >= 1 && 10#${PORT} <= 65535 && 10#${CAPACITY} >= 20 && 10#${CAPACITY} <= 65536 )) || exit 1
listeners="$(ss -ltnH "sport = :${PORT}")"
[[ -z "${listeners}" ]] || { echo '测试端口已占用，停止'; exit 1; }
ROOT="${XRC_TEST_ROOT:-/data/xrc-bench}"
mkdir -p "${ROOT}" benchmark/results
ROOT="$(realpath -e -- "${ROOT}")"
free_bytes="$(df -B1 --output=avail "${ROOT}" | awk 'NR==2 {print $1}')"
(( free_bytes >= 250*1024*1024*1024 )) || { echo '测试盘可用空间不足 250GiB，停止'; exit 1; }
REPORT="$(mktemp -d "$PWD/benchmark/results/pl1-100g-$(date +%Y%m%d-%H%M%S).XXXXXX")"
DATA="$(mktemp -d "${ROOT}/xrc-pl1-100g.XXXXXX")"
SERVER_PID=""
cleanup() {
  status=$?
  trap - EXIT
  set +e
  if [[ -n "${SERVER_PID}" ]]; then
    kill -TERM "${SERVER_PID}" 2>/dev/null
    for _ in $(seq 1 40); do kill -0 "${SERVER_PID}" 2>/dev/null || break; sleep 1; done
    if kill -0 "${SERVER_PID}" 2>/dev/null; then
      echo "服务未退出，保留数据：${DATA}" >&2
      echo CLEANUP_BLOCKED > "${REPORT}/status.txt"
      exit 1
    fi
    wait "${SERVER_PID}"
  fi
  for file in "${DATA}"/rocksdb/LOG* "${DATA}"/rocksdb/OPTIONS*; do
    [[ -f "${file}" ]] && cp -- "${file}" "${REPORT}/"
  done
  if [[ ! -L "${DATA}" && "$(realpath -e -- "${DATA}")" == "${DATA}" && "$(dirname "${DATA}")" == "${ROOT}" && "$(basename "${DATA}")" == xrc-pl1-100g.* ]]; then
    rm -rf -- "${DATA}" || status=1
  else
    status=1
  fi
  if (( status == 0 )); then echo PASS > "${REPORT}/status.txt"; else echo FAIL > "${REPORT}/status.txt"; fi
  echo "报告：${REPORT}，退出码：${status}"
  exit "${status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
echo "报告：${REPORT}"
echo "临时数据：${DATA}（退出时清理）"
{ date -Ins; uname -a; nproc; free -h; df -h "${ROOT}"; echo "declared_disk=cloud_essd/PL1 capacity_gib=${CAPACITY}"; } > "${REPORT}/environment.txt"
echo 'PL1 是声明值，请在阿里云控制台确认实际磁盘等级。'
sed "s/^write-rate-mib .*/write-rate-mib 35/; s/^disk-type .*/disk-type cloud_essd/; s/^disk-pl .*/disk-pl pl1/; s/^disk-capacity-gib .*/disk-capacity-gib ${CAPACITY}/" xrockscache-4c8g.conf > "${REPORT}/config.conf"
javac -d "${REPORT}" benchmark/java/CapacityCheck.java
build/xrockscache -c "${REPORT}/config.conf" -dir "${DATA}" -bind 127.0.0.1 -port "${PORT}" > "${REPORT}/server.log" 2>&1 &
SERVER_PID=$!
ready=0
for _ in $(seq 1 100); do
  kill -0 "${SERVER_PID}" || { cat "${REPORT}/server.log"; exit 1; }
  if [[ "$(timeout 2 redis-cli -p "${PORT}" --raw PING 2>/dev/null || true)" == PONG ]]; then ready=1; break; fi
  sleep 0.1
done
(( ready == 1 )) || { cat "${REPORT}/server.log"; exit 1; }
timeout 5 redis-cli -p "${PORT}" --raw INFO > "${REPORT}/info-before.txt"
grep -q '^write_rate_bytes_sec:36700160' "${REPORT}/info-before.txt" || { echo '程序未启用 35MiB/s 限速，请重新编译'; exit 1; }
timeout 14400 java -Xmx256m -cp "${REPORT}" CapacityCheck "${PORT}" 107374182400 "${REPORT}/samples.txt" 2>&1 | tee "${REPORT}/rate-35.log"
grep '^LOAD_PASS ' "${REPORT}/rate-35.log"
grep '^VERIFY_PASS ' "${REPORT}/rate-35.log"
timeout 5 redis-cli -p "${PORT}" --raw INFO > "${REPORT}/info-35.txt"
echo '100GiB 加载及内容、TTL 抽样检查通过。此测试不包含重启或读取 RT 验收。'
