#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DESKTOP_ROOT/.." && pwd)"
CHANGELOG_FILE="$REPO_ROOT/CHANGELOG.json"

cd "$DESKTOP_ROOT"

if [[ ! -f "$CHANGELOG_FILE" ]]; then
  echo "CHANGELOG.json not found: $CHANGELOG_FILE" >&2
  exit 1
fi

VERSION_RAW="$(python - <<'PY' "$CHANGELOG_FILE"
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
)"

VERSION_SAFE="$(printf '%s' "$VERSION_RAW" | tr -cd 'A-Za-z0-9._-')"
if [[ -z "$VERSION_SAFE" ]]; then
  echo "invalid version parsed from CHANGELOG.json: $VERSION_RAW" >&2
  exit 1
fi

wails build -platform windows/amd64 -nsis -clean -v 2

BIN_DIR="$DESKTOP_ROOT/build/bin"
DEFAULT_INSTALLER="$BIN_DIR/pt-nexus-amd64-installer.exe"
VERSIONED_INSTALLER="$BIN_DIR/pt-nexus-${VERSION_SAFE}-amd64-installer.exe"

if [[ -f "$DEFAULT_INSTALLER" ]]; then
  mv -f "$DEFAULT_INSTALLER" "$VERSIONED_INSTALLER"
  echo "[desktop] versioned installer: $VERSIONED_INSTALLER"
else
  echo "[desktop] warning: installer not found at $DEFAULT_INSTALLER" >&2
fi
