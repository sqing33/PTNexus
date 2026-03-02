#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DESKTOP_ROOT/.." && pwd)"
WEBUI_ROOT="$REPO_ROOT/webui"

if ! command -v pnpm >/dev/null 2>&1; then
  echo "pnpm is required but not found in PATH" >&2
  exit 1
fi

echo "[desktop] installing frontend dependencies from: $WEBUI_ROOT"
cd "$WEBUI_ROOT"
pnpm install
