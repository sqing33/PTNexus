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

move_path_with_cross_device_fallback() {
  local src_path="$1"
  local dst_path="$2"

  if mv "$src_path" "$dst_path" 2>/tmp/ptnexus-mv.err; then
    rm -f /tmp/ptnexus-mv.err
    return 0
  fi

  local move_err=""
  move_err="$(cat /tmp/ptnexus-mv.err 2>/dev/null || true)"
  rm -f /tmp/ptnexus-mv.err
  if ! printf '%s' "$move_err" | grep -qi 'cross-device link'; then
    echo "$move_err" >&2
    return 1
  fi

  log_bootstrap "目录迁移遇到跨设备限制，改用复制回退 src=${src_path} dst=${dst_path}"
  if ! cp -a "$src_path" "$dst_path"; then
    rm -rf "$dst_path"
    echo "跨设备复制失败：${src_path} -> ${dst_path}" >&2
    return 1
  fi
  if ! rm -rf "$src_path"; then
    echo "跨设备复制后删除源路径失败：${src_path}" >&2
    return 1
  fi
  return 0
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

normalize_version() {
  local version="${1:-}"
  version="${version#v}"
  version="${version#V}"
  if [[ ! "$version" =~ ^[0-9]+([.][0-9]+)*$ ]]; then
    return 1
  fi
  printf '%s\n' "$version"
}

version_is_older() {
  local current_version image_version
  current_version="$(normalize_version "${1:-}")" || return 1
  image_version="$(normalize_version "${2:-}")" || return 1

  local -a current_parts image_parts
  local index max_parts current_part image_part
  IFS='.' read -r -a current_parts <<< "$current_version"
  IFS='.' read -r -a image_parts <<< "$image_version"
  max_parts=${#current_parts[@]}
  if [ ${#image_parts[@]} -gt "$max_parts" ]; then
    max_parts=${#image_parts[@]}
  fi

  for ((index = 0; index < max_parts; index++)); do
    current_part="${current_parts[index]:-0}"
    image_part="${image_parts[index]:-0}"
    if ((10#$current_part < 10#$image_part)); then
      return 0
    fi
    if ((10#$current_part > 10#$image_part)); then
      return 1
    fi
  done
  return 1
}

runtime_version_from_target() {
  local target="${1:-}"
  if [ "$(basename "$target")" != "server" ]; then
    return 1
  fi
  local version
  version="$(basename "$(dirname "$target")")"
  normalize_version "$version" >/dev/null || return 1
  printf '%s\n' "$version"
}

restore_updated_runtime() {
  local base_dir="${PTNEXUS_BASE_DIR:-/app/server}"
  local update_root="${UPDATE_DIR:-/app/data/updates}"
  local current_link="${update_root}/current"
  local image_version_file="${PTNEXUS_VERSION_FILE:-/app/VERSION}"

  if [ ! -L "$current_link" ]; then
    return 0
  fi

  local resolved_target
  resolved_target="$(readlink -f "$current_link" 2>/dev/null || true)"
  if [ -z "$resolved_target" ] || [ ! -x "${resolved_target}/server" ]; then
    log_bootstrap "检测到持久化更新入口无效，继续使用镜像内运行时: ${current_link}"
    return 0
  fi

  local image_version=""
  local persisted_version=""
  image_version="$(tr -d '[:space:]' < "$image_version_file" 2>/dev/null || true)"
  persisted_version="$(runtime_version_from_target "$resolved_target" 2>/dev/null || true)"
  if [ -n "$image_version" ] && [ -n "$persisted_version" ] && version_is_older "$persisted_version" "$image_version"; then
    local active_target=""
    if [ -L "$base_dir" ]; then
      active_target="$(readlink -f "$base_dir" 2>/dev/null || true)"
    fi
    if [ "$active_target" = "$resolved_target" ]; then
      log_bootstrap "持久化运行时 ${persisted_version} 低于镜像 ${image_version}，但当前仍在使用该目录；请重建容器以切回镜像运行时"
      return 0
    fi
    rm -f "$current_link"
    log_bootstrap "忽略低于镜像版本的持久化运行时: persisted=${persisted_version} image=${image_version}"
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
    if ! move_path_with_cross_device_fallback "$base_dir" "$backup_dir"; then
      echo "备份镜像内运行时失败：${base_dir} -> ${backup_dir}" >&2
      return 1
    fi
  fi

  if ! update_symlink_atomic "$base_dir" "$current_link"; then
    if [ -n "$backup_dir" ] && [ ! -e "$base_dir" ]; then
      move_path_with_cross_device_fallback "$backup_dir" "$base_dir" || true
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


main() {
  ensure_system_deps
  restore_updated_runtime

  echo "启动 supervisord（Go 版）进行多服务编排..."
  local supervisord_bin
  supervisord_bin="$(find_supervisord)" || {
    echo "未找到 supervisord，无法启动编排。建议拉取/重建最新镜像。" >&2
    exit 1
  }
  exec "$supervisord_bin" -n -c /app/supervisord.conf
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
