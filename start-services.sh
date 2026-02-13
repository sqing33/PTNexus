#!/bin/bash
set -euo pipefail

export no_proxy="localhost,127.0.0.1,::1"
export NO_PROXY="localhost,127.0.0.1,::1"

is_truthy() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|y|Y|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

log_bootstrap() {
  echo "[bootstrap] $*"
}

find_supervisord() {
  if command -v supervisord >/dev/null 2>&1; then
    command -v supervisord
    return 0
  fi
  for p in /usr/bin/supervisord /usr/local/bin/supervisord; do
    if [ -x "$p" ]; then
      echo "$p"
      return 0
    fi
  done
  return 1
}

ensure_bootstrap_dir() {
  mkdir -p /app/data/.bootstrap 2>/dev/null || true
}

ensure_system_deps() {
  if ! is_truthy "${AUTO_INSTALL_SYSTEM_DEPS:-true}"; then
    return 0
  fi

  local missing=()
  local cmd
  for cmd in supervisord ffmpeg mpv mediainfo git; do
    command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
  done

  if [ ${#missing[@]} -eq 0 ]; then
    return 0
  fi

  log_bootstrap "检测到缺失命令: ${missing[*]}，尝试安装系统依赖..."

  if [ "$(id -u)" -ne 0 ]; then
    echo "当前容器非 root，无法自动安装系统依赖。建议拉取/重建最新镜像。" >&2
    return 1
  fi

  local apt_cmd=""
  if command -v apt-get >/dev/null 2>&1; then
    apt_cmd="apt-get"
  elif command -v apt >/dev/null 2>&1; then
    apt_cmd="apt"
  fi

  if [ -z "$apt_cmd" ]; then
    echo "当前镜像缺少 apt/apt-get，无法自动安装系统依赖。建议拉取/重建最新镜像。" >&2
    return 1
  fi

  ensure_bootstrap_dir

  local install_cmd="
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
$apt_cmd update
$apt_cmd install -y --no-install-recommends \
  supervisor \
  ffmpeg \
  mpv \
  mediainfo \
  fonts-noto-cjk \
  git \
  libicu-dev \
  ca-certificates
$apt_cmd clean || true
rm -rf /var/lib/apt/lists/* || true
"

  if command -v flock >/dev/null 2>&1; then
    local lock_file="/app/data/.bootstrap/deps.lock"
    flock -w 300 "$lock_file" bash -lc "$install_cmd"
  else
    bash -lc "$install_cmd"
  fi

  find_supervisord >/dev/null 2>&1
}

hash_file_sha256() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return 0
  fi
  python3 - "$file" <<'PY'
import hashlib, sys
p = sys.argv[1]
h = hashlib.sha256()
with open(p, "rb") as f:
    for chunk in iter(lambda: f.read(1024 * 1024), b""):
        h.update(chunk)
print(h.hexdigest())
PY
}

ensure_python_deps() {
  if ! is_truthy "${AUTO_INSTALL_PIP_DEPS:-true}"; then
    log_bootstrap "pip 依赖同步已关闭（AUTO_INSTALL_PIP_DEPS=false）"
    return 0
  fi

  local req="/app/requirements.txt"
  if [ ! -f "$req" ]; then
    return 0
  fi

  local mode="${AUTO_PIP_SYNC_MODE:-always}"
  log_bootstrap "pip 依赖同步检查（mode=${mode}, req=${req}）"

  ensure_bootstrap_dir
  local hash_file="/app/data/.bootstrap/requirements.sha256"
  local new_hash
  new_hash="$(hash_file_sha256 "$req" 2>/dev/null || true)"
  if [ "$mode" = "changed" ]; then
    if [ -z "$new_hash" ]; then
      echo "计算 requirements.txt 哈希失败，跳过 pip 依赖同步（mode=changed）。" >&2
      return 0
    fi

    local old_hash=""
    if [ -f "$hash_file" ]; then
      old_hash="$(cat "$hash_file" 2>/dev/null || true)"
    fi

    if [ "$new_hash" = "$old_hash" ]; then
      log_bootstrap "requirements 未变化（mode=changed），跳过 pip 依赖同步。"
      return 0
    fi
  fi

  log_bootstrap "开始安装/更新 pip 依赖..."
  if command -v flock >/dev/null 2>&1; then
    local pip_lock="/app/data/.bootstrap/pip.lock"
    flock -w 300 "$pip_lock" python3 -m pip install --no-cache-dir -r "$req"
  else
    python3 -m pip install --no-cache-dir -r "$req"
  fi
  if [ -n "$new_hash" ]; then
    echo "$new_hash" > "$hash_file"
  fi
}


ensure_system_deps
ensure_python_deps

echo "启动 supervisord（Go 版）进行多服务编排..."
SUPERVISORD_BIN="$(find_supervisord)" || {
  echo "未找到 supervisord，无法启动编排。建议拉取/重建最新镜像。" >&2
  exit 1
}
exec "$SUPERVISORD_BIN" -n -c /app/supervisord.conf
