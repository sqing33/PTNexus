#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "$REPO_ROOT"

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing command: $cmd" >&2
    exit 1
  fi
}

json_get() {
  python3 - "$1" <<'PY'
import json, sys
path = sys.argv[1]
with open(path, 'r', encoding='utf-8') as f:
    data = json.load(f)
print(data.get('history', [{}])[0].get('version', 'unknown'))
PY
}

VERSION="$(json_get "${REPO_ROOT}/CHANGELOG.json" | tr -d '\r\n')"
if [ -z "$VERSION" ] || [ "$VERSION" = "unknown" ]; then
  echo "Failed to parse version from CHANGELOG.json" >&2
  exit 1
fi

ARCHES="${ARCHES:-amd64 arm64}"
OS_NAME="${OS_NAME:-linux}"

OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist/updates/${VERSION}}"
BASE_URL="${BASE_URL:-https://github.com/sqing33/PTNexus/releases/download/${VERSION}}"

mkdir -p "$OUT_DIR"

if [ "${SKIP_WEBUI_BUILD:-false}" != "true" ]; then
  require_cmd node
  require_cmd pnpm

  echo "[update/build] Building webui..."
  (cd webui && pnpm install && pnpm run build)
fi

# Ensure server dist exists if webui/dist exists.
if [ -d "${REPO_ROOT}/webui/dist" ]; then
  echo "[update/build] Using webui/dist"
else
  echo "[update/build] webui/dist not found. Set SKIP_WEBUI_BUILD=false to build it." >&2
fi

build_server_bin() {
  local arch="$1"
  local out_name="$2"

  local cc=""
  if [ "$arch" = "amd64" ]; then
    cc="gcc"
  elif [ "$arch" = "arm64" ]; then
    cc="aarch64-linux-gnu-gcc"
  else
    echo "Unsupported arch: $arch" >&2
    return 1
  fi

  if ! command -v go >/dev/null 2>&1; then
    echo "Missing command: go" >&2
    return 1
  fi
  if ! command -v "$cc" >/dev/null 2>&1; then
    echo "Missing compiler: $cc (skip $arch build)" >&2
    return 2
  fi

  echo "[update/build] Building server linux/${arch} -> ${out_name}"
  (cd server && CGO_ENABLED=1 GOOS=linux GOARCH="$arch" CC="$cc" go build -ldflags="-s -w" -o "$out_name" ./cmd/server)
}

sha256_of() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return 0
  fi
  python3 - "$file" <<'PY'
import hashlib, sys
p = sys.argv[1]
h = hashlib.sha256()
with open(p, 'rb') as f:
    for chunk in iter(lambda: f.read(1024*1024), b''):
        h.update(chunk)
print(h.hexdigest())
PY
}

file_size() {
  local file="$1"
  if stat -c%s "$file" >/dev/null 2>&1; then
    stat -c%s "$file"
    return 0
  fi
  python3 - "$file" <<'PY'
import os, sys
print(os.path.getsize(sys.argv[1]))
PY
}

# Build bundles.
ARTIFACT_JSON_LINES=()

for arch in $ARCHES; do
  bin_src="${REPO_ROOT}/server/server-${arch}"

  if [ ! -f "$bin_src" ]; then
    build_server_bin "$arch" "server-${arch}" || true
  fi

  if [ ! -f "$bin_src" ]; then
    echo "[update/build] Missing server binary: $bin_src (skip ${OS_NAME}/${arch})" >&2
    continue
  fi

  tmp_dir="$(mktemp -d)"
  mkdir -p "${tmp_dir}/server"

  # Runtime tree.
  install -m 0755 "$bin_src" "${tmp_dir}/server/server"
  cp -R "${REPO_ROOT}/server/configs" "${tmp_dir}/server/configs"
  cp "${REPO_ROOT}/server/sites_data.json" "${tmp_dir}/server/sites_data.json"

  if [ -d "${REPO_ROOT}/webui/dist" ]; then
    mkdir -p "${tmp_dir}/server/dist"
    cp -R "${REPO_ROOT}/webui/dist/." "${tmp_dir}/server/dist/"
  fi

  bundle_name="ptnexus-runtime-${OS_NAME}-${arch}.tar.gz"
  bundle_path="${OUT_DIR}/${bundle_name}"

  echo "[update/build] Packaging ${bundle_name}"
  tar -C "$tmp_dir" -czf "$bundle_path" server
  rm -rf "$tmp_dir"

  sha="$(sha256_of "$bundle_path")"
  size="$(file_size "$bundle_path")"
  url="${BASE_URL}/${bundle_name}"

  ARTIFACT_JSON_LINES+=("    {\n      \"os\": \"${OS_NAME}\",\n      \"arch\": \"${arch}\",\n      \"url\": \"${url}\",\n      \"sha256\": \"${sha}\",\n      \"size\": ${size},\n      \"format\": \"tar.gz\"\n    }")
done

if [ ${#ARTIFACT_JSON_LINES[@]} -eq 0 ]; then
  echo "[update/build] No artifacts built." >&2
  exit 1
fi

# Generate UPDATE_MANIFEST.json (for publishing).
manifest_path="${OUT_DIR}/UPDATE_MANIFEST.json"

{
  echo '{'
  echo '  "schema": 1,'
  echo '  "latest": {'
  echo "    \"version\": \"${VERSION}\","
  echo '    "artifacts": ['

  for i in "${!ARTIFACT_JSON_LINES[@]}"; do
    echo -e "${ARTIFACT_JSON_LINES[$i]}" | sed 's/^/      /'
    if [ "$i" -lt $((${#ARTIFACT_JSON_LINES[@]} - 1)) ]; then
      echo '      ,'
    fi
  done

  echo '    ]'
  echo '  }'
  echo '}'
} > "$manifest_path"

echo "[update/build] Done"
echo "[update/build] Artifacts: ${OUT_DIR}"
echo "[update/build] Manifest:  ${manifest_path}"

