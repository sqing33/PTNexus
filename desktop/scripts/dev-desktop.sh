#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "[desktop-go] starting wails dev (frontend from webui-go)"
cd "$DESKTOP_ROOT"
wails dev
