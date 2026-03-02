#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "[desktop] starting wails dev (frontend from webui)"
cd "$DESKTOP_ROOT"
wails dev
