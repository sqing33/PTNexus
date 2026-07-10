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

artifact_urls() {
  local changelog_path="$1"
  local version="$2"
  local filename="$3"
  local fallback_base_url="$4"
  python3 - "$changelog_path" "$version" "$filename" "$fallback_base_url" <<'PY'
import json
import sys

changelog_path, version, filename, fallback_base_url = sys.argv[1:]
with open(changelog_path, "r", encoding="utf-8") as f:
    data = json.load(f)

sources = data.get("artifact_sources") or []
urls = []
for source in sources:
    template = ""
    if isinstance(source, dict):
        template = str(source.get("url", "")).strip()
    elif isinstance(source, str):
        template = source.strip()
    if not template:
        continue
    expanded = template.replace("{version}", version).replace("{filename}", filename)
    if expanded:
        urls.append(expanded)

if not urls and fallback_base_url:
    urls.append(f"{fallback_base_url.rstrip('/')}/{filename}")

seen = set()
for item in urls:
    if not item or item in seen:
        continue
    seen.add(item)
    print(item)
PY
}

# VERSION: 写入 manifest 的逻辑版本号（可覆盖，供 beta 使用 vX.Y.Z.<run>）
# RELEASE_TAG: 下载 URL 中的 Release tag 段（可覆盖为 beta；默认等于 VERSION）
CHANGELOG_VERSION="$(json_get "${REPO_ROOT}/CHANGELOG.json" | tr -d '\r\n')"
if [ -z "$CHANGELOG_VERSION" ] || [ "$CHANGELOG_VERSION" = "unknown" ]; then
  echo "Failed to parse version from CHANGELOG.json" >&2
  exit 1
fi

VERSION="$(printf '%s' "${VERSION:-$CHANGELOG_VERSION}" | tr -d '\r\n')"
if [ -z "$VERSION" ]; then
  echo "VERSION is empty" >&2
  exit 1
fi

RELEASE_TAG="$(printf '%s' "${RELEASE_TAG:-$VERSION}" | tr -d '\r\n')"
if [ -z "$RELEASE_TAG" ]; then
  echo "RELEASE_TAG is empty" >&2
  exit 1
fi

ARCHES="${ARCHES:-amd64 arm64}"
OS_NAME="${OS_NAME:-linux}"

OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist/updates/${VERSION}}"
# artifact 下载路径使用 RELEASE_TAG（beta 滚动发布时 tag=beta，版本号仍是 VERSION）
BASE_URL="${BASE_URL:-https://github.com/sqing33/PTNexus/releases/download/${RELEASE_TAG}}"

echo "[update/build] version=${VERSION} release_tag=${RELEASE_TAG} changelog_base=${CHANGELOG_VERSION}"

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
ARTIFACTS_JSONL="$(mktemp)"
BUILD_BIN_DIR="$(mktemp -d)"
trap 'rm -f "$ARTIFACTS_JSONL"; rm -rf "$BUILD_BIN_DIR"' EXIT

for arch in $ARCHES; do
  bin_src="${BUILD_BIN_DIR}/server-${arch}"

  build_server_bin "$arch" "$bin_src" || true

  if [ ! -f "$bin_src" ]; then
    echo "[update/build] Missing built server binary: $bin_src (skip ${OS_NAME}/${arch})" >&2
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
  # 模板 {version} 展开为 RELEASE_TAG，使产物挂到固定 tag（如 beta）时 URL 正确
  mapfile -t urls < <(artifact_urls "${REPO_ROOT}/CHANGELOG.json" "${RELEASE_TAG}" "${bundle_name}" "${BASE_URL}")
  if [ ${#urls[@]} -eq 0 ]; then
    echo "[update/build] No artifact source available for ${bundle_name}" >&2
    exit 1
  fi

  primary_url="${urls[0]}"
  mirror_urls=("${urls[@]:1}")

  python3 - "${OS_NAME}" "${arch}" "${primary_url}" "${sha}" "${size}" "${bundle_name}" "${mirror_urls[@]}" >> "$ARTIFACTS_JSONL" <<'PY'
import json
import sys

os_name = sys.argv[1]
arch = sys.argv[2]
primary_url = sys.argv[3]
sha = sys.argv[4]
size = int(sys.argv[5])
filename = sys.argv[6]
mirror_urls = [item for item in sys.argv[7:] if item.strip()]

entry = {
    "os": os_name,
    "arch": arch,
    "url": primary_url,
    "sha256": sha,
    "size": size,
    "format": "tar.gz",
}
if mirror_urls:
    entry["mirror_urls"] = mirror_urls

print(json.dumps(entry, ensure_ascii=False))
PY
done

if [ ! -s "$ARTIFACTS_JSONL" ]; then
  echo "[update/build] No artifacts built." >&2
  exit 1
fi

# Generate UPDATE_MANIFEST.json (for publishing).
manifest_path="${OUT_DIR}/UPDATE_MANIFEST.json"

python3 - "${REPO_ROOT}/CHANGELOG.json" "${VERSION}" "${CHANGELOG_VERSION}" "$ARTIFACTS_JSONL" "$manifest_path" <<'PY'
import json
import sys
from datetime import datetime, timezone

changelog_path, version, changelog_version, artifacts_jsonl, manifest_path = sys.argv[1:]
with open(changelog_path, "r", encoding="utf-8") as f:
    changelog = json.load(f)

history = list(changelog.get("history") or [])
latest_log = history[0] if history else {}

artifacts = []
with open(artifacts_jsonl, "r", encoding="utf-8") as f:
    for line in f:
        line = line.strip()
        if line:
            artifacts.append(json.loads(line))

latest = {
    "version": version,
    "artifacts": artifacts,
}
if latest_log.get("date"):
    latest["date"] = latest_log["date"]
if "force_update" in latest_log:
    latest["force_update"] = bool(latest_log.get("force_update"))
if "disable_update" in latest_log:
    latest["disable_update"] = bool(latest_log.get("disable_update"))
if latest_log.get("note"):
    latest["note"] = latest_log["note"]

# 覆盖 VERSION 时保证 history[0].version 与 latest.version 一致（beta 不改仓库 CHANGELOG）
if version != changelog_version:
    today = datetime.now(timezone.utc).strftime("%Y.%m.%d")
    beta_entry = {
        "version": version,
        "date": latest.get("date") or today,
        "changes": [
            f"Beta build based on {changelog_version}",
        ],
    }
    if "force_update" in latest:
        beta_entry["force_update"] = latest["force_update"]
    if "disable_update" in latest:
        beta_entry["disable_update"] = latest["disable_update"]
    if latest.get("note"):
        beta_entry["note"] = latest["note"]
    history = [beta_entry] + history
elif history:
    history = list(history)
    history[0] = dict(history[0])
    history[0]["version"] = version

manifest = {
    "schema": 2,
    "latest": latest,
    "history": history,
}

with open(manifest_path, "w", encoding="utf-8") as f:
    json.dump(manifest, f, ensure_ascii=False, indent=2)
    f.write("\n")
PY

echo "[update/build] Done"
echo "[update/build] Artifacts: ${OUT_DIR}"
echo "[update/build] Manifest:  ${manifest_path}"
