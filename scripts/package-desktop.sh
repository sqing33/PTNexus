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

usage() {
  cat <<'EOF'
Usage:
  bash scripts/package-desktop.sh package         # 默认：构建 Windows 安装包
  bash scripts/package-desktop.sh frontend-install
  bash scripts/package-desktop.sh frontend-dev
  bash scripts/package-desktop.sh frontend-build
  bash scripts/package-desktop.sh desktop-dev
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

frontend_install() {
  require_cmd pnpm
  echo "[desktop] installing frontend dependencies from: $WEBUI_ROOT"
  cd "$WEBUI_ROOT"
  pnpm install
}

frontend_dev() {
  require_cmd pnpm
  echo "[desktop] starting frontend dev server from: $WEBUI_ROOT"
  cd "$WEBUI_ROOT"
  pnpm run dev
}

frontend_build() {
  require_cmd pnpm
  echo "[desktop] building frontend from: $WEBUI_ROOT"
  cd "$WEBUI_ROOT"
  pnpm run build

  echo "[desktop] syncing dist to: $TARGET_DIST"
  mkdir -p "$TARGET_DIST"
  # 保留 go:embed all:frontend/dist 的占位文件，避免未构建时目录缺失导致编译失败。
  rsync -a --delete --exclude 'placeholder.txt' "$WEBUI_DIST/" "$TARGET_DIST/"

  if [[ -f "$SOURCE_ICON" ]]; then
    echo "[desktop] syncing icon: $SOURCE_ICON -> $TARGET_ICON"
    cp "$SOURCE_ICON" "$TARGET_ICON"
  else
    echo "[desktop] warning: icon not found at $SOURCE_ICON" >&2
  fi

  if [[ -f "$UPDATER_ROOT/go.mod" ]]; then
    echo "[desktop] building updater sidecar: $UPDATER_EXE"
    mkdir -p "$UPDATER_OUT_DIR"
    (
      cd "$UPDATER_ROOT"
      GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "$UPDATER_EXE" .
    )
  else
    echo "[desktop] warning: updater module not found at $UPDATER_ROOT/go.mod" >&2
  fi

  if [[ -f "$SERVER_ROOT/cmd/server/main.go" ]]; then
    echo "[desktop] building server sidecar: $SERVER_EXE"
    mkdir -p "$UPDATER_OUT_DIR"
    (
      cd "$SERVER_ROOT"
      GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "$SERVER_EXE" ./cmd/server
    )
  else
    echo "[desktop] warning: server/cmd/server/main.go not found at $SERVER_ROOT/cmd/server/main.go" >&2
  fi

  if [[ -f "$SERVER_ROOT/sites_data.json" ]]; then
    echo "[desktop] syncing sites_data.json -> $SIDECAR_SITE_DATA"
    mkdir -p "$UPDATER_OUT_DIR"
    cp "$SERVER_ROOT/sites_data.json" "$SIDECAR_SITE_DATA"
  else
    echo "[desktop] warning: sites_data.json not found at $SERVER_ROOT/sites_data.json" >&2
  fi

  if [[ -f "$CHANGELOG_FILE" ]]; then
    echo "[desktop] syncing CHANGELOG.json -> $SIDECAR_CHANGELOG"
    mkdir -p "$UPDATER_OUT_DIR"
    cp "$CHANGELOG_FILE" "$SIDECAR_CHANGELOG"
  else
    echo "[desktop] warning: CHANGELOG.json not found at $CHANGELOG_FILE" >&2
  fi

  if [[ -f "$SERVER_ROOT/configs/global_mappings.yaml" ]]; then
    echo "[desktop] syncing global_mappings.yaml -> $SIDECAR_GLOBAL_MAP"
    mkdir -p "$SIDECAR_CONFIG_DIR"
    cp "$SERVER_ROOT/configs/global_mappings.yaml" "$SIDECAR_GLOBAL_MAP"
  else
    echo "[desktop] warning: global_mappings.yaml not found at $SERVER_ROOT/configs/global_mappings.yaml" >&2
  fi

  prepare_windows_bdinfo_sidecar
  prepare_optional_windows_tools

  echo "[desktop] frontend sync completed"
}

prepare_windows_bdinfo_sidecar() {
  local source_dir="$BDINFO_SRC_DIR"
  if [[ -n "${PTNEXUS_BDINFO_WIN_DIR:-}" ]]; then
    source_dir="${PTNEXUS_BDINFO_WIN_DIR}"
  fi

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

desktop_dev() {
  require_cmd wails
  echo "[desktop] starting wails dev"
  cd "$DESKTOP_ROOT"
  wails dev
}

package_windows() {
  require_cmd wails
  require_makensis

  if [[ ! -f "$CHANGELOG_FILE" ]]; then
    echo "CHANGELOG.json not found: $CHANGELOG_FILE" >&2
    exit 1
  fi

  local version_raw
  local version_safe
  version_raw="$(read_latest_version "$CHANGELOG_FILE")"
  version_safe="$(printf '%s' "$version_raw" | tr -cd 'A-Za-z0-9._-')"
  if [[ -z "$version_safe" ]]; then
    echo "invalid version parsed from CHANGELOG.json: $version_raw" >&2
    exit 1
  fi

  echo "[desktop] building Windows installer via Wails"
  cd "$DESKTOP_ROOT"
  wails build -platform windows/amd64 -nsis -clean -v 2

  local bin_dir="$DESKTOP_ROOT/build/bin"
  local default_installer="$bin_dir/pt-nexus-amd64-installer.exe"
  local versioned_installer="$bin_dir/pt-nexus-${version_safe}-amd64-installer.exe"

  if [[ -f "$default_installer" ]]; then
    mv -f "$default_installer" "$versioned_installer"
    echo "[desktop] versioned installer: $versioned_installer"
  else
    echo "[desktop] warning: installer not found at $default_installer" >&2
  fi

  echo "[desktop] done"
  echo "[desktop] app: $bin_dir/pt-nexus.exe"
  echo "[desktop] installer: $versioned_installer"
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
