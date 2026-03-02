#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DESKTOP_ROOT/.." && pwd)"
WEBUI_ROOT="$REPO_ROOT/webui"
WEBUI_DIST="$WEBUI_ROOT/dist"
TARGET_DIST="$DESKTOP_ROOT/frontend/dist"
SOURCE_ICON="$WEBUI_ROOT/public/favicon.ico"
TARGET_ICON="$DESKTOP_ROOT/build/windows/icon.ico"
UPDATER_ROOT="$REPO_ROOT/updater"
UPDATER_OUT_DIR="$DESKTOP_ROOT/build/windows/sidecar"
UPDATER_EXE="$UPDATER_OUT_DIR/updater.exe"

if ! command -v pnpm >/dev/null 2>&1; then
  echo "pnpm is required but not found in PATH" >&2
  exit 1
fi

echo "[desktop] building frontend from: $WEBUI_ROOT"
cd "$WEBUI_ROOT"
pnpm run build

echo "[desktop] syncing dist to: $TARGET_DIST"
mkdir -p "$TARGET_DIST"
rsync -a --delete "$WEBUI_DIST/" "$TARGET_DIST/"

if [[ -f "$SOURCE_ICON" ]]; then
  echo "[desktop] syncing icon: $SOURCE_ICON -> $TARGET_ICON"
  cp "$SOURCE_ICON" "$TARGET_ICON"
else
  echo "[desktop] warning: icon not found at $SOURCE_ICON" >&2
fi

if [[ -f "$UPDATER_ROOT/updater.go" ]]; then
  echo "[desktop] building updater sidecar: $UPDATER_EXE"
  mkdir -p "$UPDATER_OUT_DIR"
  (
    cd "$UPDATER_ROOT"
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "$UPDATER_EXE" updater.go
  )
else
  echo "[desktop] warning: updater.go not found at $UPDATER_ROOT/updater.go" >&2
fi

echo "[desktop] frontend sync completed"
