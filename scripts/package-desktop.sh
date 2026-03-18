#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DESKTOP_ROOT="$REPO_ROOT/desktop"
WEBUI_ROOT="$REPO_ROOT/webui"
WEBUI_DIST="$WEBUI_ROOT/dist"
TARGET_DIST="$DESKTOP_ROOT/frontend/dist"
SOURCE_ICON="$WEBUI_ROOT/public/favicon.ico"
TARGET_ICON="$DESKTOP_ROOT/build/windows/icon.ico"
SERVER_ROOT="$REPO_ROOT/server"
UPDATER_ROOT="$REPO_ROOT/updater"
UPDATER_OUT_DIR="$DESKTOP_ROOT/build/windows/sidecar"
SERVER_EXE="$UPDATER_OUT_DIR/server.exe"
UPDATER_EXE="$UPDATER_OUT_DIR/updater.exe"
SIDECAR_SITE_DATA="$UPDATER_OUT_DIR/sites_data.json"
SIDECAR_CHANGELOG="$UPDATER_OUT_DIR/CHANGELOG.json"
SIDECAR_CONFIG_DIR="$UPDATER_OUT_DIR/configs"
SIDECAR_GLOBAL_MAP="$SIDECAR_CONFIG_DIR/global_mappings.yaml"
TOOLS_SRC_DIR="$DESKTOP_ROOT/tools/windows"
TOOLS_OUT_DIR="$UPDATER_OUT_DIR/tools"
BDINFO_SRC_DIR="$SERVER_ROOT/bdinfo/windows"
BDINFO_OUT_DIR="$UPDATER_OUT_DIR/bdinfo/windows"
CHANGELOG_FILE="$REPO_ROOT/CHANGELOG.json"
CACHE_ROOT="$DESKTOP_ROOT/build/.package-cache"
FORCE_PACKAGE_REBUILD="${PTNEXUS_PACKAGE_FORCE:-0}"

declare -a STAGE_NAMES=()
declare -a STAGE_STATUSES=()
declare -a STAGE_DURATIONS_MS=()

CURRENT_STAGE_STATUS=""
PACKAGE_VERSION_RAW=""
PACKAGE_VERSION_SAFE=""
PACKAGE_BIN_DIR="$DESKTOP_ROOT/build/bin"
PACKAGE_DEFAULT_INSTALLER=""
PACKAGE_VERSIONED_INSTALLER=""
PACKAGE_DEFAULT_UPDATE_INSTALLER=""
PACKAGE_VERSIONED_UPDATE_INSTALLER=""
NSIS_INSTALLER_DIR="$DESKTOP_ROOT/build/windows/installer"
WAILS_TOOLS_FILE="$NSIS_INSTALLER_DIR/wails_tools.nsh"
UPDATE_WAILS_TOOLS_FILE="$NSIS_INSTALLER_DIR/wails_update_tools.nsh"
PACKAGE_FILE_VERSION=""
PACKAGE_IS_DEV_ENV=0

usage() {
  cat <<'EOF'
Usage:
  bash scripts/package-desktop.sh package         # 默认：构建 Windows 安装包（支持增量缓存）
  bash scripts/package-desktop.sh frontend-install
  bash scripts/package-desktop.sh frontend-dev
  bash scripts/package-desktop.sh frontend-build
  bash scripts/package-desktop.sh desktop-dev

Environment:
  DEV_ENV=true                                   # 开发模式：跳过完整安装包，仅生成更新安装包
  PTNEXUS_PACKAGE_FORCE=1                        # 忽略缓存，强制全量重建 package/frontend-build
EOF
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "$cmd is required but not found in PATH" >&2
    exit 1
  fi
}

require_makensis() {
  if command -v makensis >/dev/null 2>&1 && makensis -VERSION >/dev/null 2>&1; then
    return
  fi

  cat >&2 <<'EOF'
[desktop] error: makensis not found in PATH.
[desktop] NSIS is required to build the Windows installer.
[desktop] install (Ubuntu/WSL):
  sudo apt update
  sudo apt install -y nsis
[desktop] verify:
  makensis -VERSION
EOF
  exit 1
}

detect_python() {
  if command -v python >/dev/null 2>&1; then
    echo "python"
    return
  fi
  if command -v python3 >/dev/null 2>&1; then
    echo "python3"
    return
  fi
  echo "python or python3 is required but not found in PATH" >&2
  exit 1
}

read_latest_version() {
  local changelog="$1"
  local py_bin
  py_bin="$(detect_python)"
  "$py_bin" - <<'PY' "$changelog"
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
obj = json.loads(path.read_text(encoding='utf-8'))
history = obj.get('history', [])
if not history:
    raise SystemExit("empty history in CHANGELOG.json")
version = str(history[0].get('version', '')).strip()
if not version:
    raise SystemExit("missing latest version in CHANGELOG.json")
print(version)
PY
}

normalize_windows_file_version() {
  local raw="$1"
  local cleaned
  local -a raw_parts=()
  local -a parts=()
  local count

  cleaned="${raw#[Vv]}"
  cleaned="${cleaned//[^0-9.]/}"
  IFS='.' read -r -a raw_parts <<< "$cleaned"
  for part in "${raw_parts[@]}"; do
    if [[ -n "$part" ]]; then
      parts+=("$part")
    fi
  done

  count="${#parts[@]}"
  if ((count < 3)); then
    echo "invalid Windows file version derived from CHANGELOG.json: $raw" >&2
    exit 1
  fi

  if ((count > 4)); then
    parts=("${parts[@]:0:4}")
    count=4
  fi

  while ((count < 4)); do
    parts+=("0")
    ((count += 1))
  done

  printf '%s.%s.%s.%s\n' "${parts[0]}" "${parts[1]}" "${parts[2]}" "${parts[3]}"
}

ensure_cache_root() {
  mkdir -p "$CACHE_ROOT"
}

cache_file_path() {
  local name="$1"
  printf '%s/%s.sha256\n' "$CACHE_ROOT" "$name"
}

force_package_rebuild() {
  [[ "$FORCE_PACKAGE_REBUILD" == "1" ]]
}

is_dev_env() {
  [[ "${DEV_ENV:-}" == "true" ]]
}

timestamp_ms() {
  date +%s%3N
}

format_duration_ms() {
  local ms="$1"
  awk -v ms="$ms" 'BEGIN { if (ms >= 1000) printf "%.2fs", ms / 1000; else printf "%dms", ms }'
}

skip_stage() {
  local stage="$1"
  local reason="$2"
  record_stage_result "$stage" "skipped" 0
  echo "[desktop] stage skipped: $stage ($reason)"
}

record_stage_result() {
  STAGE_NAMES+=("$1")
  STAGE_STATUSES+=("$2")
  STAGE_DURATIONS_MS+=("$3")
}

set_stage_status() {
  local status="$1"
  CURRENT_STAGE_STATUS="$status"
  if [[ -n "${STAGE_STATUS_FILE:-}" ]]; then
    printf '%s\n' "$status" >"$STAGE_STATUS_FILE"
  fi
}

run_stage() {
  local stage="$1"
  shift

  local start end elapsed rc status status_file
  echo "[desktop] stage start: $stage"
  start="$(timestamp_ms)"
  CURRENT_STAGE_STATUS="done"
  status_file="$(mktemp)"
  printf 'done\n' >"$status_file"

  set +e
  (
    set -e
    export STAGE_STATUS_FILE="$status_file"
    "$@"
  )
  rc=$?
  set -e

  if [[ "$rc" -ne 0 ]]; then
    end="$(timestamp_ms)"
    elapsed=$((end - start))
    status="$(<"$status_file")"
    rm -f "$status_file"
    if [[ -z "$status" || "$status" == "done" ]]; then
      status="failed"
    fi
    record_stage_result "$stage" "$status" "$elapsed"
    echo "[desktop] stage end: $stage ($status, $(format_duration_ms "$elapsed"))" >&2
    return 1
  fi

  end="$(timestamp_ms)"
  elapsed=$((end - start))
  status="$(<"$status_file")"
  rm -f "$status_file"
  if [[ -z "$status" ]]; then
    status="done"
  fi
  record_stage_result "$stage" "$status" "$elapsed"
  echo "[desktop] stage end: $stage ($status, $(format_duration_ms "$elapsed"))"
}

print_stage_summary() {
  local i
  echo "[desktop] stage summary:"
  for i in "${!STAGE_NAMES[@]}"; do
    printf '[desktop]   %-22s %-12s %s\n' \
      "${STAGE_NAMES[$i]}" \
      "${STAGE_STATUSES[$i]}" \
      "$(format_duration_ms "${STAGE_DURATIONS_MS[$i]}")"
  done
}

snapshot_hash() {
  local py_bin
  py_bin="$(detect_python)"
  "$py_bin" - "$@" <<'PY'
import hashlib
import sys
from pathlib import Path

h = hashlib.sha256()

def add(text: str) -> None:
    h.update(text.encode("utf-8", errors="surrogateescape"))

for raw in sys.argv[1:]:
    path = Path(raw)
    add(f"PATH\t{path.as_posix()}\n")
    if not path.exists():
        add("MISSING\n")
        continue

    stat = path.stat()
    if path.is_file():
        add(f"FILE\t{stat.st_size}\t{stat.st_mtime_ns}\t{oct(stat.st_mode)}\n")
        continue

    if path.is_dir():
        add(f"DIR\t{stat.st_mtime_ns}\t{oct(stat.st_mode)}\n")
        for child in sorted(path.rglob("*"), key=lambda item: item.as_posix()):
            child_stat = child.stat()
            rel = child.relative_to(path).as_posix()
            if child.is_dir():
                add(f"D\t{rel}\t{child_stat.st_mtime_ns}\t{oct(child_stat.st_mode)}\n")
            else:
                add(f"F\t{rel}\t{child_stat.st_size}\t{child_stat.st_mtime_ns}\t{oct(child_stat.st_mode)}\n")
        continue

    add(f"OTHER\t{stat.st_mtime_ns}\t{oct(stat.st_mode)}\n")

print(h.hexdigest())
PY
}

cache_matches() {
  local cache_file="$1"
  local expected="$2"
  shift 2

  local current output
  if force_package_rebuild; then
    return 1
  fi
  if [[ ! -f "$cache_file" ]]; then
    return 1
  fi

  current="$(<"$cache_file")"
  if [[ "$current" != "$expected" ]]; then
    return 1
  fi

  for output in "$@"; do
    if [[ ! -e "$output" ]]; then
      return 1
    fi
  done
}

write_cache() {
  local cache_file="$1"
  local value="$2"
  ensure_cache_root
  printf '%s\n' "$value" >"$cache_file"
}

collect_root_go_files() {
  local root="$1"
  local -n out_ref="$2"
  while IFS= read -r file; do
    out_ref+=("$file")
  done < <(find "$root" -maxdepth 1 -type f -name '*.go' | sort)
}

resolve_bdinfo_source_dir() {
  if [[ -n "${PTNEXUS_BDINFO_WIN_DIR:-}" ]]; then
    printf '%s\n' "${PTNEXUS_BDINFO_WIN_DIR}"
    return
  fi
  printf '%s\n' "$BDINFO_SRC_DIR"
}

resolve_effective_tools_inputs() {
  local -n out_ref="$1"
  out_ref=()

  if [[ -d "$TOOLS_SRC_DIR" ]]; then
    out_ref+=("$TOOLS_SRC_DIR")
  fi

  local env_key custom_source
  for env_key in PTNEXUS_MPV_WIN_BIN PTNEXUS_FFMPEG_WIN_BIN PTNEXUS_FFPROBE_WIN_BIN PTNEXUS_MEDIAINFO_WIN_BIN; do
    custom_source="${!env_key:-}"
    if [[ -n "$custom_source" ]]; then
      out_ref+=("$custom_source")
    fi
  done
}

build_webui_dist() {
  require_cmd pnpm
  echo "[desktop] building frontend from: $WEBUI_ROOT"
  (
    cd "$WEBUI_ROOT"
    pnpm run build
  )
}

sync_frontend_dist() {
  echo "[desktop] syncing dist to: $TARGET_DIST"
  mkdir -p "$TARGET_DIST"
  # 保留 go:embed all:frontend/dist 的占位文件，避免未构建时目录缺失导致编译失败。
  rsync -a --delete --exclude 'placeholder.txt' "$WEBUI_DIST/" "$TARGET_DIST/"
}

sync_desktop_icon() {
  if [[ -f "$SOURCE_ICON" ]]; then
    mkdir -p "$(dirname "$TARGET_ICON")"
    echo "[desktop] syncing icon: $SOURCE_ICON -> $TARGET_ICON"
    cp "$SOURCE_ICON" "$TARGET_ICON"
  else
    echo "[desktop] warning: icon not found at $SOURCE_ICON" >&2
  fi
}

build_updater_sidecar() {
  if [[ ! -f "$UPDATER_ROOT/go.mod" ]]; then
    echo "[desktop] warning: updater module not found at $UPDATER_ROOT/go.mod" >&2
    return 0
  fi

  echo "[desktop] building updater sidecar: $UPDATER_EXE"
  mkdir -p "$UPDATER_OUT_DIR"
  (
    cd "$UPDATER_ROOT"
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "$UPDATER_EXE" .
  )
}

build_server_sidecar() {
  if [[ ! -f "$SERVER_ROOT/cmd/server/main.go" ]]; then
    echo "[desktop] warning: server/cmd/server/main.go not found at $SERVER_ROOT/cmd/server/main.go" >&2
    return 0
  fi

  echo "[desktop] building server sidecar: $SERVER_EXE"
  mkdir -p "$UPDATER_OUT_DIR"
  (
    cd "$SERVER_ROOT"
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "$SERVER_EXE" ./cmd/server
  )
}

sync_static_sidecar_files() {
  mkdir -p "$UPDATER_OUT_DIR" "$SIDECAR_CONFIG_DIR"

  if [[ -f "$SERVER_ROOT/sites_data.json" ]]; then
    echo "[desktop] syncing sites_data.json -> $SIDECAR_SITE_DATA"
    cp "$SERVER_ROOT/sites_data.json" "$SIDECAR_SITE_DATA"
  else
    echo "[desktop] warning: sites_data.json not found at $SERVER_ROOT/sites_data.json" >&2
  fi

  if [[ -f "$CHANGELOG_FILE" ]]; then
    echo "[desktop] syncing CHANGELOG.json -> $SIDECAR_CHANGELOG"
    cp "$CHANGELOG_FILE" "$SIDECAR_CHANGELOG"
  else
    echo "[desktop] warning: CHANGELOG.json not found at $CHANGELOG_FILE" >&2
  fi

  if [[ -f "$SERVER_ROOT/configs/global_mappings.yaml" ]]; then
    echo "[desktop] syncing global_mappings.yaml -> $SIDECAR_GLOBAL_MAP"
    cp "$SERVER_ROOT/configs/global_mappings.yaml" "$SIDECAR_GLOBAL_MAP"
  else
    echo "[desktop] warning: global_mappings.yaml not found at $SERVER_ROOT/configs/global_mappings.yaml" >&2
  fi
}

prepare_windows_bdinfo_sidecar() {
  local source_dir
  source_dir="$(resolve_bdinfo_source_dir)"

  rm -rf "$UPDATER_OUT_DIR/bdinfo"
  if [[ ! -d "$source_dir" ]]; then
    echo "[desktop] warning: windows BDInfo directory not found: $source_dir" >&2
    return
  fi

  mkdir -p "$BDINFO_OUT_DIR"
  rsync -a "$source_dir/" "$BDINFO_OUT_DIR/"
  echo "[desktop] bundled BDInfo dir: $source_dir"

  if [[ ! -f "$BDINFO_OUT_DIR/BDInfo.exe" ]]; then
    echo "[desktop] warning: BDInfo.exe missing in bundled directory: $BDINFO_OUT_DIR" >&2
  fi
}

copy_override_windows_tool() {
  local tool_name="$1"
  local custom_source="$2"
  if [[ -z "$custom_source" ]]; then
    return
  fi
  if [[ ! -f "$custom_source" ]]; then
    echo "[desktop] warning: optional tool path is invalid: $custom_source" >&2
    return
  fi

  mkdir -p "$TOOLS_OUT_DIR"
  cp "$custom_source" "$TOOLS_OUT_DIR/$tool_name"
  echo "[desktop] bundled tool override: $tool_name"
}

prepare_optional_windows_tools() {
  rm -rf "$TOOLS_OUT_DIR"
  if [[ -d "$TOOLS_SRC_DIR" ]]; then
    mkdir -p "$TOOLS_OUT_DIR"
    rsync -a "$TOOLS_SRC_DIR/" "$TOOLS_OUT_DIR/"
    echo "[desktop] bundled tools dir: $TOOLS_SRC_DIR"
  fi

  copy_override_windows_tool "mpv.exe" "${PTNEXUS_MPV_WIN_BIN:-}"
  copy_override_windows_tool "ffmpeg.exe" "${PTNEXUS_FFMPEG_WIN_BIN:-}"
  copy_override_windows_tool "ffprobe.exe" "${PTNEXUS_FFPROBE_WIN_BIN:-}"
  copy_override_windows_tool "mediainfo.exe" "${PTNEXUS_MEDIAINFO_WIN_BIN:-}"

  local missing=0
  local tool
  for tool in mpv.exe ffmpeg.exe ffprobe.exe mediainfo.exe; do
    if [[ ! -f "$TOOLS_OUT_DIR/$tool" ]]; then
      echo "[desktop] warning: optional tool missing: $tool (place in $TOOLS_SRC_DIR or set PTNEXUS_*_WIN_BIN)" >&2
      missing=1
    fi
  done
  if [[ "$missing" -eq 0 ]]; then
    echo "[desktop] bundled media tools are ready"
  fi
}

frontend_install() {
  require_cmd pnpm
  echo "[desktop] installing frontend dependencies from: $WEBUI_ROOT"
  (
    cd "$WEBUI_ROOT"
    pnpm install
  )
}

frontend_dev() {
  require_cmd pnpm
  echo "[desktop] starting frontend dev server from: $WEBUI_ROOT"
  (
    cd "$WEBUI_ROOT"
    pnpm run dev
  )
}

ensure_frontend_dependencies() {
  local cache_file hash
  local -a inputs=("$WEBUI_ROOT/package.json" "$WEBUI_ROOT/pnpm-lock.yaml")

  cache_file="$(cache_file_path frontend-install)"
  hash="$(snapshot_hash "${inputs[@]}")"
  if cache_matches "$cache_file" "$hash" "$WEBUI_ROOT/node_modules"; then
    echo "[desktop] frontend dependencies cache hit"
    return
  fi

  frontend_install
  write_cache "$cache_file" "$hash"
}

prepare_frontend_stage() {
  local cache_file hash
  local -a inputs=(
    "$WEBUI_ROOT/src"
    "$WEBUI_ROOT/public"
    "$WEBUI_ROOT/index.html"
    "$WEBUI_ROOT/package.json"
    "$WEBUI_ROOT/pnpm-lock.yaml"
    "$WEBUI_ROOT/vite.config.ts"
    "$WEBUI_ROOT/tsconfig.json"
    "$WEBUI_ROOT/tsconfig.app.json"
    "$WEBUI_ROOT/tsconfig.node.json"
    "$WEBUI_ROOT/env.d.ts"
  )

  cache_file="$(cache_file_path frontend)"
  hash="$(snapshot_hash "${inputs[@]}")"
  if cache_matches "$cache_file" "$hash" "$TARGET_DIST/index.html" "$TARGET_ICON"; then
    echo "[desktop] frontend cache hit"
    set_stage_status "cache-hit"
    return
  fi

  ensure_frontend_dependencies
  build_webui_dist
  sync_frontend_dist
  sync_desktop_icon
  write_cache "$cache_file" "$hash"
  set_stage_status "rebuilt"
}

prepare_sidecars_stage() {
  local server_cache updater_cache assets_cache
  local server_hash updater_hash assets_hash
  local need_server=1
  local need_updater=1
  local need_assets=1
  local rebuilt=0
  local -a server_inputs=("$SERVER_ROOT/go.mod" "$SERVER_ROOT/go.sum" "$SERVER_ROOT/cmd/server" "$SERVER_ROOT/internal")
  local -a updater_inputs=("$UPDATER_ROOT/go.mod" "$UPDATER_ROOT/go.sum")
  local -a tools_inputs=()
  local -a asset_inputs=("$SOURCE_ICON" "$SERVER_ROOT/sites_data.json" "$CHANGELOG_FILE" "$SERVER_ROOT/configs/global_mappings.yaml")
  local -a asset_outputs=("$SIDECAR_SITE_DATA" "$SIDECAR_CHANGELOG" "$SIDECAR_GLOBAL_MAP" "$TARGET_ICON")
  local -a pids=()
  local -a labels=()
  local bdinfo_source
  local idx

  while IFS= read -r file; do
    updater_inputs+=("$file")
  done < <(find "$UPDATER_ROOT" -maxdepth 1 -type f -name '*.go' | sort)

  server_cache="$(cache_file_path server-sidecar)"
  updater_cache="$(cache_file_path updater-sidecar)"
  assets_cache="$(cache_file_path bundled-assets)"

  server_hash="$(snapshot_hash "${server_inputs[@]}")"
  updater_hash="$(snapshot_hash "${updater_inputs[@]}")"
  bdinfo_source="$(resolve_bdinfo_source_dir)"
  asset_inputs+=("$bdinfo_source")
  resolve_effective_tools_inputs tools_inputs
  if ((${#tools_inputs[@]} > 0)); then
    asset_inputs+=("${tools_inputs[@]}")
    asset_outputs+=("$TOOLS_OUT_DIR")
  fi
  if [[ -d "$bdinfo_source" ]]; then
    asset_outputs+=("$BDINFO_OUT_DIR")
  fi
  assets_hash="$(snapshot_hash "${asset_inputs[@]}")"

  if cache_matches "$server_cache" "$server_hash" "$SERVER_EXE"; then
    need_server=0
    echo "[desktop] server sidecar cache hit"
  fi

  if cache_matches "$updater_cache" "$updater_hash" "$UPDATER_EXE"; then
    need_updater=0
    echo "[desktop] updater sidecar cache hit"
  fi

  if cache_matches "$assets_cache" "$assets_hash" "${asset_outputs[@]}"; then
    need_assets=0
    echo "[desktop] bundled assets cache hit"
  fi

  if [[ "$need_updater" -eq 1 ]]; then
    build_updater_sidecar &
    pids+=("$!")
    labels+=("updater")
  fi

  if [[ "$need_server" -eq 1 ]]; then
    build_server_sidecar &
    pids+=("$!")
    labels+=("server")
  fi

  for idx in "${!pids[@]}"; do
    if ! wait "${pids[$idx]}"; then
      echo "[desktop] error: ${labels[$idx]} sidecar build failed" >&2
      return 1
    fi
  done

  if [[ "$need_updater" -eq 1 ]]; then
    write_cache "$updater_cache" "$updater_hash"
    rebuilt=1
  fi

  if [[ "$need_server" -eq 1 ]]; then
    write_cache "$server_cache" "$server_hash"
    rebuilt=1
  fi

  if [[ "$need_assets" -eq 1 ]]; then
    sync_desktop_icon
    sync_static_sidecar_files
    prepare_windows_bdinfo_sidecar
    prepare_optional_windows_tools
    write_cache "$assets_cache" "$assets_hash"
    rebuilt=1
  fi

  if [[ "$rebuilt" -eq 1 ]]; then
    set_stage_status "rebuilt"
  else
    set_stage_status "cache-hit"
  fi
}

frontend_build() {
  ensure_cache_root
  prepare_frontend_stage
  prepare_sidecars_stage
  echo "[desktop] frontend sync completed"
}

desktop_dev() {
  require_cmd wails
  echo "[desktop] starting wails dev"
  (
    cd "$DESKTOP_ROOT"
    wails dev
  )
}

prepare_package_context() {
  if [[ ! -f "$CHANGELOG_FILE" ]]; then
    echo "CHANGELOG.json not found: $CHANGELOG_FILE" >&2
    exit 1
  fi

  ensure_cache_root
  if is_dev_env; then
    PACKAGE_IS_DEV_ENV=1
  else
    PACKAGE_IS_DEV_ENV=0
  fi
  PACKAGE_VERSION_RAW="$(read_latest_version "$CHANGELOG_FILE")"
  PACKAGE_VERSION_SAFE="$(printf '%s' "$PACKAGE_VERSION_RAW" | tr -cd 'A-Za-z0-9._-')"
  if [[ -z "$PACKAGE_VERSION_SAFE" ]]; then
    echo "invalid version parsed from CHANGELOG.json: $PACKAGE_VERSION_RAW" >&2
    exit 1
  fi
  PACKAGE_FILE_VERSION="$(normalize_windows_file_version "$PACKAGE_VERSION_RAW")"

  PACKAGE_DEFAULT_INSTALLER="$PACKAGE_BIN_DIR/pt-nexus-amd64-installer.exe"
  PACKAGE_VERSIONED_INSTALLER="$PACKAGE_BIN_DIR/pt-nexus-${PACKAGE_VERSION_SAFE}-amd64-installer.exe"
  PACKAGE_DEFAULT_UPDATE_INSTALLER="$PACKAGE_BIN_DIR/pt-nexus-amd64-update.exe"
  PACKAGE_VERSIONED_UPDATE_INSTALLER="$PACKAGE_BIN_DIR/pt-nexus-${PACKAGE_VERSION_SAFE}-amd64-update.exe"
}

build_desktop_binary_stage() {
  local cache_file hash
  local -a desktop_root_go_files=()
  local -a inputs=(
    "$DESKTOP_ROOT/go.mod"
    "$DESKTOP_ROOT/go.sum"
    "$DESKTOP_ROOT/internal"
    "$DESKTOP_ROOT/wails.json"
    "$TARGET_DIST"
    "$TARGET_ICON"
    "$DESKTOP_ROOT/build/windows/info.json"
    "$DESKTOP_ROOT/build/windows/wails.exe.manifest"
    "$SCRIPT_DIR/package-desktop.sh"
  )

  collect_root_go_files "$DESKTOP_ROOT" desktop_root_go_files
  if ((${#desktop_root_go_files[@]} > 0)); then
    inputs+=("${desktop_root_go_files[@]}")
  fi

  cache_file="$(cache_file_path desktop-binary)"
  hash="$(snapshot_hash "${inputs[@]}")"
  if cache_matches "$cache_file" "$hash" "$PACKAGE_BIN_DIR/pt-nexus.exe"; then
    echo "[desktop] desktop binary cache hit"
    set_stage_status "cache-hit"
    return
  fi

  echo "[desktop] building Windows desktop binary via Wails"
  (
    cd "$DESKTOP_ROOT"
    wails build -platform windows/amd64 -clean -s -skipbindings -v 2
  )

  if [[ ! -f "$PACKAGE_BIN_DIR/pt-nexus.exe" ]]; then
    echo "[desktop] error: desktop binary not found at $PACKAGE_BIN_DIR/pt-nexus.exe" >&2
    return 1
  fi

  write_cache "$cache_file" "$hash"
  set_stage_status "rebuilt"
}

build_full_installer_stage() {
  local cache_file hash
  local -a desktop_root_go_files=()
  local -a inputs=(
    "$DESKTOP_ROOT/go.mod"
    "$DESKTOP_ROOT/go.sum"
    "$DESKTOP_ROOT/internal"
    "$DESKTOP_ROOT/wails.json"
    "$TARGET_DIST"
    "$TARGET_ICON"
    "$DESKTOP_ROOT/build/windows/info.json"
    "$DESKTOP_ROOT/build/windows/wails.exe.manifest"
    "$DESKTOP_ROOT/build/windows/installer/project.nsi"
    "$DESKTOP_ROOT/build/windows/installer/ptnexus-process-control.nsh"
    "$UPDATER_OUT_DIR"
    "$SCRIPT_DIR/package-desktop.sh"
  )

  collect_root_go_files "$DESKTOP_ROOT" desktop_root_go_files
  if ((${#desktop_root_go_files[@]} > 0)); then
    inputs+=("${desktop_root_go_files[@]}")
  fi

  cache_file="$(cache_file_path full-installer)"
  hash="$(snapshot_hash "${inputs[@]}")"
  if cache_matches "$cache_file" "$hash" "$PACKAGE_BIN_DIR/pt-nexus.exe" "$PACKAGE_VERSIONED_INSTALLER" "$WAILS_TOOLS_FILE"; then
    echo "[desktop] full installer cache hit"
    set_stage_status "cache-hit"
    return
  fi

  echo "[desktop] building Windows installer via Wails"
  (
    cd "$DESKTOP_ROOT"
    wails build -platform windows/amd64 -nsis -clean -s -skipbindings -v 2
  )

  if [[ -f "$PACKAGE_DEFAULT_INSTALLER" ]]; then
    mv -f "$PACKAGE_DEFAULT_INSTALLER" "$PACKAGE_VERSIONED_INSTALLER"
    echo "[desktop] versioned installer: $PACKAGE_VERSIONED_INSTALLER"
  fi

  if [[ ! -f "$PACKAGE_VERSIONED_INSTALLER" ]]; then
    echo "[desktop] error: installer not found at $PACKAGE_VERSIONED_INSTALLER" >&2
    return 1
  fi

  write_cache "$cache_file" "$hash"
  set_stage_status "rebuilt"
}

build_update_installer_stage() {
  local cache_file hash
  local -a inputs=(
    "$PACKAGE_BIN_DIR/pt-nexus.exe"
    "$UPDATER_EXE"
    "$SERVER_EXE"
    "$SIDECAR_SITE_DATA"
    "$SIDECAR_CHANGELOG"
    "$SIDECAR_GLOBAL_MAP"
    "$TARGET_ICON"
    "$DESKTOP_ROOT/build/windows/installer/project-update.nsi"
    "$UPDATE_WAILS_TOOLS_FILE"
    "$DESKTOP_ROOT/build/windows/installer/ptnexus-process-control.nsh"
    "$SCRIPT_DIR/package-desktop.sh"
  )

  cache_file="$(cache_file_path update-installer)"
  hash="$(snapshot_hash "${inputs[@]}")"
  if cache_matches "$cache_file" "$hash" "$PACKAGE_VERSIONED_UPDATE_INSTALLER" "$PACKAGE_BIN_DIR/pt-nexus.exe"; then
    echo "[desktop] update installer cache hit"
    set_stage_status "cache-hit"
    return
  fi

  echo "[desktop] building Windows update installer via NSIS"
  (
    cd "$NSIS_INSTALLER_DIR"
    makensis \
      -DARG_WAILS_AMD64_BINARY='..\..\bin\pt-nexus.exe' \
      -DINFO_PRODUCTVERSION="$PACKAGE_VERSION_RAW" \
      -DINFO_FILEVERSION="$PACKAGE_FILE_VERSION" \
      project-update.nsi
  )

  if [[ -f "$PACKAGE_DEFAULT_UPDATE_INSTALLER" ]]; then
    mv -f "$PACKAGE_DEFAULT_UPDATE_INSTALLER" "$PACKAGE_VERSIONED_UPDATE_INSTALLER"
    echo "[desktop] versioned update installer: $PACKAGE_VERSIONED_UPDATE_INSTALLER"
  fi

  if [[ ! -f "$PACKAGE_VERSIONED_UPDATE_INSTALLER" ]]; then
    echo "[desktop] error: update installer not found at $PACKAGE_VERSIONED_UPDATE_INSTALLER" >&2
    return 1
  fi

  write_cache "$cache_file" "$hash"
  set_stage_status "rebuilt"
}

package_windows() {
  require_cmd wails
  require_makensis
  prepare_package_context

  if force_package_rebuild; then
    echo "[desktop] PTNEXUS_PACKAGE_FORCE=1, cache disabled for this run"
  fi

  run_stage "prepare_frontend" prepare_frontend_stage
  run_stage "prepare_sidecars" prepare_sidecars_stage
  if [[ "$PACKAGE_IS_DEV_ENV" -eq 1 ]]; then
    run_stage "build_desktop_binary" build_desktop_binary_stage
    skip_stage "build_full_installer" "DEV_ENV=true"
  else
    run_stage "build_full_installer" build_full_installer_stage
  fi
  run_stage "build_update_installer" build_update_installer_stage

  echo "[desktop] done"
  print_stage_summary
  echo "[desktop] app: $PACKAGE_BIN_DIR/pt-nexus.exe"
  if [[ "$PACKAGE_IS_DEV_ENV" -eq 1 ]]; then
    echo "[desktop] installer: skipped (DEV_ENV=true)"
  else
    echo "[desktop] installer: $PACKAGE_VERSIONED_INSTALLER"
  fi
  echo "[desktop] update installer: $PACKAGE_VERSIONED_UPDATE_INSTALLER"
}

cmd="${1:-package}"
case "$cmd" in
  package)
    package_windows
    ;;
  frontend-install)
    frontend_install
    ;;
  frontend-dev)
    frontend_dev
    ;;
  frontend-build)
    frontend_build
    ;;
  desktop-dev)
    desktop_dev
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    echo "unknown subcommand: $cmd" >&2
    usage
    exit 1
    ;;
esac
