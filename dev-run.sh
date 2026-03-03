#!/usr/bin/env bash
set -euo pipefail

# One-command dev runner (server + updater + optional webui dev server).
#
# Default usage:
#   ./dev-run.sh
#
# Commands:
#   ./dev-run.sh up        # start, then tail logs (default)
#   ./dev-run.sh down      # stop started services
#   ./dev-run.sh status    # show pids + basic health checks
#
# Tunables (env):
#   SERVER_PORT=5275 UPDATER_PORT=5274 BATCH_PORT=5276 WEBUI_PORT=5173
#   WEBUI=1 WEBUI_DIR=./webui
#   AUTO_INSTALL_DEPS=1    # 缺失依赖时自动安装（前端 pnpm / 后端 go mod / air）
#   UPLOAD_TEST_MODE=true  # 跳过真实发布，模拟成功响应（仅用于调试）
#   CURL_CONNECT_TIMEOUT=1 CURL_MAX_TIME=2 TCP_PROBE_TIMEOUT=1

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVERGO_DIR="${SERVERGO_DIR:-$REPO_ROOT/server}"

SERVER_PORT="${SERVER_PORT:-5275}"
UPDATER_PORT="${UPDATER_PORT:-5274}"
BATCH_PORT="${BATCH_PORT:-5276}"

WEBUI="${WEBUI:-1}"
WEBUI_PORT="${WEBUI_PORT:-5173}"
WEBUI_DIR="${WEBUI_DIR:-$REPO_ROOT/webui}"
AUTO_INSTALL_DEPS="${AUTO_INSTALL_DEPS:-1}"

AIR_CONFIG_FILE="$SERVERGO_DIR/.air.toml"

LEGACY_SERVER_BIN="${LEGACY_SERVER_BIN:-/tmp/ptnexus-server.dev}"
SERVER_AIR_BIN="${SERVER_AIR_BIN:-/tmp/ptnexus-server.air}"
UPDATER_BIN="${UPDATER_BIN:-/tmp/ptnexus-updater.dev}"

SERVER_PIDFILE="${SERVER_PIDFILE:-/tmp/ptnexus-server.${SERVER_PORT}.pid}"
UPDATER_PIDFILE="${UPDATER_PIDFILE:-/tmp/ptnexus-updater.${UPDATER_PORT}.pid}"
WEBUI_PIDFILE="${WEBUI_PIDFILE:-/tmp/ptnexus-webui.${WEBUI_PORT}.pid}"

SERVER_LOG="${SERVER_LOG:-/tmp/ptnexus-server.${SERVER_PORT}.log}"
UPDATER_LOG="${UPDATER_LOG:-/tmp/ptnexus-updater.${UPDATER_PORT}.log}"
WEBUI_LOG="${WEBUI_LOG:-/tmp/ptnexus-webui.${WEBUI_PORT}.log}"

CURL_CONNECT_TIMEOUT="${CURL_CONNECT_TIMEOUT:-1}"
CURL_MAX_TIME="${CURL_MAX_TIME:-2}"
TCP_PROBE_TIMEOUT="${TCP_PROBE_TIMEOUT:-1}"

ensure_backend_deps() {
  [[ "$AUTO_INSTALL_DEPS" != "0" ]] || return 0

  if ! command -v go >/dev/null 2>&1; then
    echo "go not found. install go first, or set AUTO_INSTALL_DEPS=0 to skip dep install" >&2
    exit 1
  fi

  echo "deps: server go modules (go mod download)"
  (cd "$SERVERGO_DIR" && go mod download)

  echo "deps: updater go modules (go mod download)"
  (cd "$REPO_ROOT/updater" && go mod download)
}

ensure_webui_deps() {
  [[ "$WEBUI" != "0" ]] || return 0

  if [[ ! -d "$WEBUI_DIR" ]]; then
    echo "webui dir missing: $WEBUI_DIR (set WEBUI_DIR=... or WEBUI=0)" >&2
    exit 1
  fi
  if ! command -v pnpm >/dev/null 2>&1; then
    echo "pnpm not found. install pnpm first, or disable webui with WEBUI=0" >&2
    exit 1
  fi
  if [[ -x "$WEBUI_DIR/node_modules/.bin/vite" ]]; then
    return 0
  fi

  if [[ "$AUTO_INSTALL_DEPS" == "0" ]]; then
    echo "webui deps missing: $WEBUI_DIR/node_modules/.bin/vite not found" >&2
    echo "run: cd \"$WEBUI_DIR\" && pnpm install" >&2
    exit 1
  fi

  echo "deps: webui missing, running pnpm install (dir: $WEBUI_DIR)"
  (cd "$WEBUI_DIR" && pnpm install)

  if [[ ! -x "$WEBUI_DIR/node_modules/.bin/vite" ]]; then
    echo "webui deps install failed: $WEBUI_DIR/node_modules/.bin/vite still missing" >&2
    exit 1
  fi
}

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
  # - PT Nexus server (dev bin / air / go run ./cmd/server) bound to :SERVER_PORT
  # - PT Nexus updater (./updater) bound to :UPDATER_PORT
  # - PT Nexus webui (vite/pnpm dev) bound to :WEBUI_PORT

  local pid cwd cmd

  # Previous runs of this script (exact dev binaries).
  for pid in $(pgrep -f "$LEGACY_SERVER_BIN" 2>/dev/null || true); do
    echo "kill conflict: server (legacy dev bin) pid=${pid}"
    kill -TERM "$pid" 2>/dev/null || true
  done
  for pid in $(pgrep -f "$SERVER_AIR_BIN" 2>/dev/null || true); do
    echo "kill conflict: server (air bin) pid=${pid}"
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
      echo "kill conflict: server (docker-dev) pid=${pid}"
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
      echo "kill conflict: server (air) pid=${pid}"
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

  if [[ "$AUTO_INSTALL_DEPS" != "0" ]]; then
    echo "deps: air missing, running go install github.com/air-verse/air@latest" >&2
    if go install github.com/air-verse/air@latest >/dev/null 2>&1; then
      if command -v air >/dev/null 2>&1; then
        command -v air
        return 0
      fi
      gopath_bin="$(go env GOPATH 2>/dev/null || true)/bin/air"
      if [[ -x "$gopath_bin" ]]; then
        echo "$gopath_bin"
        return 0
      fi
    else
      echo "failed to install air automatically" >&2
    fi
  fi

  return 1
}

build_binaries() {
  ensure_backend_deps
  echo "build: server managed by air (skip prebuild)"
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

  echo "start: server :${SERVER_PORT} (hot reload: air, log: $SERVER_LOG)"
  rm -f "$SERVER_LOG" 2>/dev/null || true

  (cd "$SERVERGO_DIR" && nohup env \
    DEV_ENV=true \
    SERVER_PORT="$SERVER_PORT" \
    PTNEXUS_BASE_DIR="$SERVERGO_DIR" \
    PTNEXUS_DATA_DIR="$SERVERGO_DIR/data" \
    "$air_cmd" -c "$AIR_CONFIG_FILE" \
    >"$SERVER_LOG" 2>&1 </dev/null & echo $! >"$SERVER_PIDFILE")

  if ! wait_http_ok "http://127.0.0.1:${SERVER_PORT}/health" 12; then
    echo "server failed to become healthy; last logs:" >&2
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
  ensure_webui_deps

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
  echo "  server: http://127.0.0.1:${SERVER_PORT}"
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
  stop_pidfile "$SERVER_PIDFILE" "server"
  exit 0
fi

if [[ "$ACTION" == "status" ]]; then
  echo "health:"
  if [[ "$WEBUI" != "0" ]]; then
    curl_http_ok "http://127.0.0.1:${WEBUI_PORT}/" && echo "webui: up" || echo "webui: down"
  fi
  curl_http_ok "http://127.0.0.1:${UPDATER_PORT}/health" && echo "updater: up" || echo "updater: down"
  curl_http_ok "http://127.0.0.1:${SERVER_PORT}/health" && echo "server: up" || echo "server: down"
  exit 0
fi

ensure_port_free_or_fail "http://127.0.0.1:${SERVER_PORT}/health" "server" "$SERVER_PORT"
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
