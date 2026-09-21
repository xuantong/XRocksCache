#!/usr/bin/env bash
set -euo pipefail
PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_DIR"
for tool in java mvn python3 redis-cli timeout; do command -v "$tool" >/dev/null; done
SERVER_BIN="${XRC_SERVER_BIN:-$PROJECT_DIR/build/xrockscache}"
test -x "$SERVER_BIN"
# 永远启动自己的实例，不允许传入生产地址。端口绑定失败时不得开始测试。
export XRC_JAVA_PORT="${XRC_JAVA_PORT:-6692}"
[[ "$XRC_JAVA_PORT" =~ ^[0-9]+$ ]] && (( XRC_JAVA_PORT > 0 && XRC_JAVA_PORT < 65536 ))
export XRC_JAVA_PASSWORD
XRC_JAVA_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
export REDISCLI_AUTH="$XRC_JAVA_PASSWORD"
DATA_DIR="$(mktemp -d /tmp/xrc_java_it.XXXXXX)"
mkdir -p "$PROJECT_DIR/benchmark/results"
RESULT_DIR="$(mktemp -d "$PROJECT_DIR/benchmark/results/java-it.XXXXXX")"
PID=""
cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n "$PID" ]]; then
    kill -TERM "$PID" 2>/dev/null || true
    for _ in $(seq 1 100); do
      kill -0 "$PID" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "$PID" 2>/dev/null; then
      echo "服务仍在运行，保留临时目录：$DATA_DIR" >&2
      exit 1
    fi
    wait "$PID" 2>/dev/null || true
  fi
  if [[ -d benchmark/spring-redis/target/surefire-reports ]]; then
    cp -a benchmark/spring-redis/target/surefire-reports "$RESULT_DIR/"
  fi
  if [[ "$DATA_DIR" == /tmp/xrc_java_it.* && ! -L "$DATA_DIR" && "$(realpath -- "$DATA_DIR")" == "$DATA_DIR" ]]; then
    rm -rf -- "$DATA_DIR"
  fi
  echo "报告目录：$RESULT_DIR"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if (exec 3<>"/dev/tcp/127.0.0.1/$XRC_JAVA_PORT") 2>/dev/null; then
  echo "测试端口已被占用" >&2; exit 1
fi
"$SERVER_BIN" -c xrockscache.conf -dir "$DATA_DIR" -bind 127.0.0.1 -port "$XRC_JAVA_PORT" \
  -requirepass "$XRC_JAVA_PASSWORD" >"$RESULT_DIR/server.log" 2>&1 &
PID=$!
ready=0
for _ in $(seq 1 100); do
  kill -0 "$PID" 2>/dev/null || break
  if grep -q 'server listening' "$RESULT_DIR/server.log" && \
      [[ "$(redis-cli -h 127.0.0.1 -p "$XRC_JAVA_PORT" --raw PING 2>/dev/null)" == PONG ]]; then
    ready=1; break
  fi
  sleep 0.1
done
if [[ "$ready" != 1 ]]; then cat "$RESULT_DIR/server.log" >&2; exit 1; fi
# clean 防止上一轮报告被误认为本轮成功；pipefail 保留 Maven/timeout 失败状态。
timeout 900 mvn -B -f benchmark/spring-redis/pom.xml clean test 2>&1 | tee "$RESULT_DIR/maven.log"
echo 'JAVA_COMPAT_PASS'
