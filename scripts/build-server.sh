#!/bin/bash
set -euo pipefail

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
  echo -e "${YELLOW}[server/build] $*${NC}"
}

log_ok() {
  echo -e "${GREEN}[server/build] $*${NC}"
}

log_err() {
  echo -e "${RED}[server/build] $*${NC}" >&2
}

require_cmd() {
  local cmd="$1"
  local hint="${2:-}"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    log_err "缺少命令: $cmd"
    if [ -n "$hint" ]; then
      log_err "$hint"
    fi
    exit 1
  fi
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SERVER_DIR="${REPO_ROOT}/server"

if [ ! -d "$SERVER_DIR" ]; then
  log_err "未找到 server 目录: ${SERVER_DIR}"
  exit 1
fi

cd "$SERVER_DIR"

require_cmd go "请先安装 Go 工具链。"
require_cmd gcc "请先安装 gcc（Debian/Ubuntu: sudo apt-get install -y gcc）。"
require_cmd aarch64-linux-gnu-gcc "请先安装 arm64 交叉编译器（Debian/Ubuntu: sudo apt-get install -y gcc-aarch64-linux-gnu libc6-dev-arm64-cross）。"

build_server() {
  local arch="$1"
  local cc="$2"
  local output="$3"

  log_info "开始构建 linux/${arch} -> ${output}"
  CGO_ENABLED=1 GOOS=linux GOARCH="$arch" CC="$cc" \
    go build -ldflags="-s -w" -o "$output" ./cmd/server
  log_ok "构建完成: ${output}"
}

build_server "amd64" "gcc" "server-amd64"
build_server "arm64" "aarch64-linux-gnu-gcc" "server-arm64"

log_ok "双架构构建完成。"
