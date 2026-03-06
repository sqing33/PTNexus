#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing command: $cmd" >&2
    exit 1
  fi
}

read_version() {
  python3 - "$REPO_ROOT/CHANGELOG.json" <<'PY'
import json
import sys
with open(sys.argv[1], 'r', encoding='utf-8') as f:
    data = json.load(f)
print(str((data.get('history') or [{}])[0].get('version', '')).strip())
PY
}

sha256_of() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return 0
  fi
  python3 - "$file" <<'PY'
import hashlib
import sys
path = sys.argv[1]
h = hashlib.sha256()
with open(path, 'rb') as f:
    for chunk in iter(lambda: f.read(1024 * 1024), b''):
        h.update(chunk)
print(h.hexdigest())
PY
}

require_cmd go
require_cmd python3

VERSION="${VERSION:-$(read_version)}"
if [[ -z "$VERSION" ]]; then
  echo "Failed to resolve version from CHANGELOG.json" >&2
  exit 1
fi

ARCHES="${ARCHES:-amd64 arm64}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist/proxy/${VERSION}}"
PROXY_ROOT="${REPO_ROOT}/proxy"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

for arch in $ARCHES; do
  case "$arch" in
    amd64|arm64)
      ;;
    *)
      echo "Unsupported proxy arch: $arch" >&2
      exit 1
      ;;
  esac

  out_file="${OUT_DIR}/pt-nexus-box-proxy-${arch}"
  echo "[proxy/release] building linux/${arch} -> ${out_file}"
  (
    cd "$PROXY_ROOT"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -ldflags="-s -w" -o "$out_file" proxy.go bdinfo.go
  )
  chmod +x "$out_file"
done

install -m 0755 "$PROXY_ROOT/install-pt-nexus-box-proxy.sh" "${OUT_DIR}/install-pt-nexus-box-proxy.sh"

checksum_file="${OUT_DIR}/pt-nexus-box-proxy-sha256sums-${VERSION}.txt"
: > "$checksum_file"
while IFS= read -r -d '' file; do
  name="$(basename "$file")"
  printf '%s  %s\n' "$(sha256_of "$file")" "$name" >> "$checksum_file"
done < <(find "$OUT_DIR" -maxdepth 1 -type f ! -name "$(basename "$checksum_file")" -print0 | sort -z)

echo "[proxy/release] assets ready: $OUT_DIR"
ls -lh "$OUT_DIR"
