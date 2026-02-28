#!/usr/bin/env bash
set -euo pipefail

# One-command dev runner (server-go + updater + optional webui dev server).
#
# Default usage:
#   cd server-go && ./dev-run.sh
#
# Commands:
#   ./dev-run.sh up        # start, then tail logs (default)
#   ./dev-run.sh down      # stop started services
#   ./dev-run.sh status    # show pids + basic health checks
#
# Tunables (env):
#   SERVER_PORT=5275 UPDATER_PORT=5274 BATCH_PORT=5276 WEBUI_PORT=5173
#   WEBUI=1 WEBUI_DIR=/home/sqing/Codes/PTNexus/webui-go
#   UPLOAD_TEST_MODE=true  # 跳过真实发布，模拟成功响应（仅用于调试）
#   CURL_CONNECT_TIMEOUT=1 CURL_MAX_TIME=2 TCP_PROBE_TIMEOUT=1

SERVERGO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SERVERGO_DIR/.." && pwd)"

SERVER_PORT="${SERVER_PORT:-5275}"
UPDATER_PORT="${UPDATER_PORT:-5274}"
BATCH_PORT="${BATCH_PORT:-5276}"

WEBUI="${WEBUI:-1}"
WEBUI_PORT="${WEBUI_PORT:-5173}"
WEBUI_DIR="${WEBUI_DIR:-/home/sqing/Codes/PTNexus/webui-go}"

AIR_CONFIG_FILE="$SERVERGO_DIR/.air.toml"

LEGACY_SERVER_BIN="${LEGACY_SERVER_BIN:-/tmp/ptnexus-server-go.dev}"
SERVER_AIR_BIN="${SERVER_AIR_BIN:-/tmp/ptnexus-server-go.air}"
UPDATER_BIN="${UPDATER_BIN:-/tmp/ptnexus-updater.dev}"

SERVER_PIDFILE="${SERVER_PIDFILE:-/tmp/ptnexus-server-go.${SERVER_PORT}.pid}"
UPDATER_PIDFILE="${UPDATER_PIDFILE:-/tmp/ptnexus-updater.${UPDATER_PORT}.pid}"
WEBUI_PIDFILE="${WEBUI_PIDFILE:-/tmp/ptnexus-webui.${WEBUI_PORT}.pid}"

SERVER_LOG="${SERVER_LOG:-/tmp/ptnexus-server-go.${SERVER_PORT}.log}"
UPDATER_LOG="${UPDATER_LOG:-/tmp/ptnexus-updater.${UPDATER_PORT}.log}"
WEBUI_LOG="${WEBUI_LOG:-/tmp/ptnexus-webui.${WEBUI_PORT}.log}"

CURL_CONNECT_TIMEOUT="${CURL_CONNECT_TIMEOUT:-1}"
CURL_MAX_TIME="${CURL_MAX_TIME:-2}"
TCP_PROBE_TIMEOUT="${TCP_PROBE_TIMEOUT:-1}"

get_cmdline() {
  local pid="$1"
  [[ -r "/proc/${pid}/cmdline" ]] || return 0
  tr '\0' ' ' <"/proc/${pid}/cmdline" 2>/dev/null || true
}

get_cwd() {
  local pid="$1"
  readlink "/proc/${pid}/cwd" 2>/dev/null || true
}

stop_pidfile() {
  local pidfile="$1"
  local label="$2"

  [[ -f "$pidfile" ]] || return 0
  local pid
  pid="$(cat "$pidfile" 2>/dev/null || true)"
  rm -f "$pidfile" 2>/dev/null || true

  [[ -n "${pid}" ]] || return 0
  if ! kill -0 "$pid" 2>/dev/null; then
    return 0
  fi

  echo "stop: ${label} pid=${pid}"
  kill -TERM "$pid" 2>/dev/null || true
  for _ in {1..50}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
    sleep 0.1
  done
  kill -KILL "$pid" 2>/dev/null || true
}

stop_known_conflicts() {
  # Make "one command" deterministic: don't accidentally keep talking to an old checkout.
  #
  # This only kills processes that look like:
  # - PT Nexus server-go (dev bin / air / go run ./cmd/server) bound to :SERVER_PORT
  # - PT Nexus updater (./updater) bound to :UPDATER_PORT
  # - PT Nexus webui (vite/pnpm dev) bound to :WEBUI_PORT

  local pid cwd cmd

  # Previous runs of this script (exact dev binaries).
  for pid in $(pgrep -f "$LEGACY_SERVER_BIN" 2>/dev/null || true); do
    echo "kill conflict: server-go (legacy dev bin) pid=${pid}"
    kill -TERM "$pid" 2>/dev/null || true
  done
  for pid in $(pgrep -f "$SERVER_AIR_BIN" 2>/dev/null || true); do
    echo "kill conflict: server-go (air bin) pid=${pid}"
    kill -TERM "$pid" 2>/dev/null || true
  done
  for pid in $(pgrep -f "$UPDATER_BIN" 2>/dev/null || true); do
    echo "kill conflict: updater (dev bin) pid=${pid}"
    kill -TERM "$pid" 2>/dev/null || true
  done

  for pid in $(pgrep -f "go run ./cmd/server" 2>/dev/null || true); do
    cwd="$(get_cwd "$pid")"
    cmd="$(get_cmdline "$pid")"
    if [[ "$cmd" == *"go run ./cmd/server"* && "$cwd" == *"PT Nexus/server"* ]]; then
      echo "kill conflict: server-go (docker-dev) pid=${pid}"
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done

  for pid in $(pgrep -f "^\\./updater$" 2>/dev/null || true); do
    cwd="$(get_cwd "$pid")"
    if [[ "$cwd" == *"PT Nexus/updater"* ]]; then
      echo "kill conflict: updater (docker-dev) pid=${pid}"
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done

  for pid in $(pgrep -f "air" 2>/dev/null || true); do
    cwd="$(get_cwd "$pid")"
    cmd="$(get_cmdline "$pid")"
    if [[ "$cwd" == "$SERVERGO_DIR" && "$cmd" == *".air.toml"* ]]; then
      echo "kill conflict: server-go (air) pid=${pid}"
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done

  if [[ "$WEBUI" != "0" ]]; then
    for pid in $(pgrep -f "vite" 2>/dev/null || true); do
      cwd="$(get_cwd "$pid")"
      if [[ "$cwd" == "$WEBUI_DIR" ]]; then
        echo "kill conflict: webui (vite) pid=${pid}"
        kill -TERM "$pid" 2>/dev/null || true
      fi
    done
    for pid in $(pgrep -f "pnpm run dev" 2>/dev/null || true); do
      cwd="$(get_cwd "$pid")"
      if [[ "$cwd" == "$WEBUI_DIR" ]]; then
        echo "kill conflict: webui (pnpm run dev) pid=${pid}"
        kill -TERM "$pid" 2>/dev/null || true
      fi
    done
    for pid in $(pgrep -f "pnpm dev" 2>/dev/null || true); do
      cwd="$(get_cwd "$pid")"
      if [[ "$cwd" == "$WEBUI_DIR" ]]; then
        echo "kill conflict: webui (pnpm dev) pid=${pid}"
        kill -TERM "$pid" 2>/dev/null || true
      fi
    done
  fi

  sleep 0.6
}

curl_http_ok() {
  local url="$1"
  curl -fsS \
    --connect-timeout "$CURL_CONNECT_TIMEOUT" \
    --max-time "$CURL_MAX_TIME" \
    "$url" >/dev/null 2>&1
}

tcp_port_open() {
  local host="${1:-127.0.0.1}"
  local port="$2"
  timeout "$TCP_PROBE_TIMEOUT" bash -c "exec 3<>/dev/tcp/${host}/${port}" >/dev/null 2>&1
}

wait_http_ok() {
  local url="$1"
  local timeout_s="${2:-10}"
  local deadline=$((SECONDS + timeout_s))
  while (( SECONDS < deadline )); do
    if curl_http_ok "$url"; then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

ensure_port_free_or_fail() {
  local url="$1"
  local label="$2"
  local port="$3"
  local host="${4:-127.0.0.1}"

  if curl_http_ok "$url"; then
    stop_known_conflicts
  elif tcp_port_open "$host" "$port"; then
    echo "port check: ${label} tcp listener found at ${host}:${port}, trying to stop known conflicts" >&2
    stop_known_conflicts
  fi

  if curl_http_ok "$url"; then
    echo "port conflict: ${label} is already responding at ${url}" >&2
    exit 1
  fi
  if tcp_port_open "$host" "$port"; then
    echo "port conflict: ${label} has tcp listener at ${host}:${port} but no valid HTTP response" >&2
    exit 1
  fi
}

resolve_air_cmd() {
  if command -v air >/dev/null 2>&1; then
    command -v air
    return 0
  fi

  local gopath_bin
  gopath_bin="$(go env GOPATH 2>/dev/null || true)/bin/air"
  if [[ -x "$gopath_bin" ]]; then
    echo "$gopath_bin"
    return 0
  fi

  return 1
}

build_binaries() {
  echo "build: server-go managed by air (skip prebuild)"
  echo "build: updater -> $UPDATER_BIN"
  (cd "$REPO_ROOT/updater" && go build -o "$UPDATER_BIN" ./updater.go)
}

start_server_go() {
  local air_cmd
  if [[ ! -f "$AIR_CONFIG_FILE" ]]; then
    echo "air config missing: $AIR_CONFIG_FILE" >&2
    exit 1
  fi
  if ! air_cmd="$(resolve_air_cmd)"; then
    echo "air not found. install with: go install github.com/air-verse/air@latest" >&2
    exit 1
  fi

  echo "start: server-go :${SERVER_PORT} (hot reload: air, log: $SERVER_LOG)"
  rm -f "$SERVER_LOG" 2>/dev/null || true

  (cd "$SERVERGO_DIR" && nohup env \
    DEV_ENV=true \
    SERVER_PORT="$SERVER_PORT" \
    PTNEXUS_BASE_DIR="$SERVERGO_DIR" \
    PTNEXUS_DATA_DIR="$SERVERGO_DIR/data" \
    "$air_cmd" -c "$AIR_CONFIG_FILE" \
    >"$SERVER_LOG" 2>&1 </dev/null & echo $! >"$SERVER_PIDFILE")

  if ! wait_http_ok "http://127.0.0.1:${SERVER_PORT}/health" 12; then
    echo "server-go failed to become healthy; last logs:" >&2
    tail -n 120 "$SERVER_LOG" >&2 || true
    exit 1
  fi
}

start_updater() {
  echo "start: updater :${UPDATER_PORT} -> :${SERVER_PORT} (log: $UPDATER_LOG)"
  rm -f "$UPDATER_LOG" 2>/dev/null || true
  (cd "$REPO_ROOT" && nohup env \
    UPDATER_PORT="$UPDATER_PORT" \
    SERVER_PORT="$SERVER_PORT" \
    BATCH_PORT="$BATCH_PORT" \
    "$UPDATER_BIN" \
    >"$UPDATER_LOG" 2>&1 </dev/null & echo $! >"$UPDATER_PIDFILE")

  if ! wait_http_ok "http://127.0.0.1:${UPDATER_PORT}/health" 12; then
    echo "updater failed to become healthy; last logs:" >&2
    tail -n 120 "$UPDATER_LOG" >&2 || true
    exit 1
  fi
}

start_webui() {
  [[ "$WEBUI" != "0" ]] || return 0
  if [[ ! -d "$WEBUI_DIR" ]]; then
    echo "webui dir missing: $WEBUI_DIR (set WEBUI_DIR=... or WEBUI=0)" >&2
    exit 1
  fi
  if ! command -v pnpm >/dev/null 2>&1; then
    echo "pnpm not found. install pnpm first, or disable webui with WEBUI=0" >&2
    exit 1
  fi

  echo "start: webui dev :${WEBUI_PORT} (dir: $WEBUI_DIR log: $WEBUI_LOG)"
  rm -f "$WEBUI_LOG" 2>/dev/null || true
  (cd "$WEBUI_DIR" && nohup pnpm run dev -- --host 0.0.0.0 --port "$WEBUI_PORT" --strictPort \
    >"$WEBUI_LOG" 2>&1 </dev/null & echo $! >"$WEBUI_PIDFILE")

  if ! wait_http_ok "http://127.0.0.1:${WEBUI_PORT}/" 20; then
    echo "webui failed to become ready; last logs:" >&2
    tail -n 120 "$WEBUI_LOG" >&2 || true
    exit 1
  fi
}

print_ready() {
  echo "ready:"
  if [[ "$WEBUI" != "0" ]]; then
    echo "  webui:     http://127.0.0.1:${WEBUI_PORT}"
  fi
  echo "  updater:   http://127.0.0.1:${UPDATER_PORT}"
  echo "  server-go: http://127.0.0.1:${SERVER_PORT}"
  echo "  cookie API: http://127.0.0.1:${UPDATER_PORT}/api/sites/cookie_sync_targets"
}

ACTION="${1:-up}"
case "$ACTION" in
  up|start|down|status) ;;
  *)
    echo "usage: $0 [up|start|down|status]" >&2
    exit 2
    ;;
esac

if [[ "$ACTION" == "down" ]]; then
  stop_pidfile "$WEBUI_PIDFILE" "webui"
  stop_pidfile "$UPDATER_PIDFILE" "updater"
  stop_pidfile "$SERVER_PIDFILE" "server-go"
  exit 0
fi

if [[ "$ACTION" == "status" ]]; then
  echo "health:"
  if [[ "$WEBUI" != "0" ]]; then
    curl_http_ok "http://127.0.0.1:${WEBUI_PORT}/" && echo "webui: up" || echo "webui: down"
  fi
  curl_http_ok "http://127.0.0.1:${UPDATER_PORT}/health" && echo "updater: up" || echo "updater: down"
  curl_http_ok "http://127.0.0.1:${SERVER_PORT}/health" && echo "server-go: up" || echo "server-go: down"
  exit 0
fi

ensure_port_free_or_fail "http://127.0.0.1:${SERVER_PORT}/health" "server-go" "$SERVER_PORT"
ensure_port_free_or_fail "http://127.0.0.1:${UPDATER_PORT}/health" "updater" "$UPDATER_PORT"
if [[ "$WEBUI" != "0" ]]; then
  ensure_port_free_or_fail "http://127.0.0.1:${WEBUI_PORT}/" "webui" "$WEBUI_PORT"
fi

build_binaries
start_server_go
start_updater
start_webui
print_ready

cleanup() {
  "$0" down >/dev/null 2>&1 || true
}
trap cleanup INT TERM

echo "tail logs (Ctrl-C to stop):"
logs=("$SERVER_LOG" "$UPDATER_LOG")
if [[ "$WEBUI" != "0" ]]; then
  logs+=("$WEBUI_LOG")
fi
tail -n 40 -F "${logs[@]}" 2>/dev/null || true
