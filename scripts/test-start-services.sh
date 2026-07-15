#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# shellcheck source=../start-services.sh
source "${REPO_ROOT}/start-services.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

make_runtime() {
  local path="$1"
  mkdir -p "$path"
  printf '#!/bin/sh\nexit 0\n' > "${path}/server"
  chmod +x "${path}/server"
}

test_older_persisted_runtime_is_ignored() {
  local root base_dir update_dir version_file old_runtime
  root="$(mktemp -d)"
  base_dir="${root}/app/server"
  update_dir="${root}/data/updates"
  version_file="${root}/VERSION"
  old_runtime="${update_dir}/releases/v4.0.52/server"

  make_runtime "$base_dir"
  make_runtime "$old_runtime"
  ln -s "$old_runtime" "${update_dir}/current"
  printf 'v4.0.55\n' > "$version_file"

  PTNEXUS_BASE_DIR="$base_dir" UPDATE_DIR="$update_dir" PTNEXUS_VERSION_FILE="$version_file" restore_updated_runtime

  [ -d "$base_dir" ] || fail "镜像运行时目录不应被替换"
  [ ! -L "$base_dir" ] || fail "镜像运行时不应指向旧持久化版本"
  [ ! -e "${update_dir}/current" ] && [ ! -L "${update_dir}/current" ] || fail "旧 current 指针应被移除"
  rm -rf "$root"
}

test_newer_persisted_runtime_is_restored() {
  local root base_dir update_dir version_file new_runtime
  root="$(mktemp -d)"
  base_dir="${root}/app/server"
  update_dir="${root}/data/updates"
  version_file="${root}/VERSION"
  new_runtime="${update_dir}/releases/v4.0.56/server"

  make_runtime "$base_dir"
  make_runtime "$new_runtime"
  ln -s "$new_runtime" "${update_dir}/current"
  printf 'v4.0.55\n' > "$version_file"

  PTNEXUS_BASE_DIR="$base_dir" UPDATE_DIR="$update_dir" PTNEXUS_VERSION_FILE="$version_file" restore_updated_runtime

  [ -L "$base_dir" ] || fail "较新的持久化运行时应被恢复"
  [ "$(readlink -f "$base_dir")" = "$new_runtime" ] || fail "baseDir 未指向较新的持久化运行时"
  rm -rf "$root"
}

test_older_persisted_runtime_is_ignored
test_newer_persisted_runtime_is_restored
echo "start-services tests passed"
