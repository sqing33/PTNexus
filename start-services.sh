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

update_symlink_atomic() {
  local link_path="$1"
  local target="$2"
  local tmp="${link_path}.tmp.$$"

  mkdir -p "$(dirname "$link_path")"
  rm -f "$tmp"
  ln -s "$target" "$tmp"
  mv -Tf "$tmp" "$link_path"
}

restore_updated_runtime() {
  local base_dir="${PTNEXUS_BASE_DIR:-/app/server}"
  local update_root="${UPDATE_DIR:-/app/data/updates}"
  local current_link="${update_root}/current"

  if [ ! -L "$current_link" ]; then
    return 0
  fi

  local resolved_target
  resolved_target="$(readlink -f "$current_link" 2>/dev/null || true)"
  if [ -z "$resolved_target" ] || [ ! -x "${resolved_target}/server" ]; then
    log_bootstrap "检测到持久化更新入口无效，继续使用镜像内运行时: ${current_link}"
    return 0
  fi

  if [ -L "$base_dir" ]; then
    local current_target=""
    current_target="$(readlink -f "$base_dir" 2>/dev/null || true)"
    if [ "$current_target" = "$resolved_target" ]; then
      log_bootstrap "已使用持久化更新版本运行时: ${resolved_target}"
      return 0
    fi
  fi

  local backup_dir=""
  if [ -e "$base_dir" ] && [ ! -L "$base_dir" ]; then
    backup_dir="${base_dir}.image.$(date +%Y%m%d-%H%M%S)"
    mv "$base_dir" "$backup_dir"
  fi

  if ! update_symlink_atomic "$base_dir" "$current_link"; then
    if [ -n "$backup_dir" ] && [ ! -e "$base_dir" ]; then
      mv "$backup_dir" "$base_dir"
    fi
    echo "恢复持久化更新运行时失败：${base_dir} -> ${current_link}" >&2
    return 1
  fi

  if [ -n "$backup_dir" ]; then
    log_bootstrap "已切换为持久化更新版本运行时，镜像内运行时备份到: ${backup_dir}"
  fi
  log_bootstrap "已恢复在线更新版本运行时: ${resolved_target}"
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

has_icu_runtime() {
  if command -v ldconfig >/dev/null 2>&1; then
    if ldconfig -p 2>/dev/null | grep -Eq 'libicu(uc|i18n|data)\.so'; then
      return 0
    fi
  fi

  if find /usr/lib /lib -type f -name 'libicuuc.so*' -print -quit 2>/dev/null | grep -q .; then
    return 0
  fi

  return 1
}

ensure_system_deps() {
  if ! is_truthy "${AUTO_INSTALL_SYSTEM_DEPS:-true}"; then
    return 0
  fi

  local missing=()
  local cmd
  for cmd in supervisord ffmpeg mpv mediainfo; do
    command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
  done
  has_icu_runtime || missing+=("libicu-dev")

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


ensure_system_deps
restore_updated_runtime

echo "启动 supervisord（Go 版）进行多服务编排..."
SUPERVISORD_BIN="$(find_supervisord)" || {
  echo "未找到 supervisord，无法启动编排。建议拉取/重建最新镜像。" >&2
  exit 1
}
exec "$SUPERVISORD_BIN" -n -c /app/supervisord.conf
