#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESKTOP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ROOT_DIR="$(cd "$DESKTOP_DIR/.." && pwd)"
RUNTIME_DIR="$DESKTOP_DIR/runtime"
CACHE_DIR="$DESKTOP_DIR/.tools/win-installer-cache"
PYTHON_EMBED_VERSION="${PYTHON_EMBED_VERSION:-3.12.8}"
PYTHON_EMBED_ZIP="python-${PYTHON_EMBED_VERSION}-embed-amd64.zip"
PYTHON_EMBED_URL="https://www.python.org/ftp/python/${PYTHON_EMBED_VERSION}/${PYTHON_EMBED_ZIP}"
WHEEL_DIR="$CACHE_DIR/wheels-py312"
FFMPEG_ARCHIVE="ffmpeg-n8.0.1-56-g3201cd40a4-win64-gpl-8.0.zip"
FFMPEG_URL="https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-02-10-13-08/${FFMPEG_ARCHIVE}"
FFMPEG_SHA256="01aebfcdbbcf83d512c1edc960eb62a6ce5f56df2e35fb3643582f78ace5f39f"
MPV_ARCHIVE="mpv-0.41.0-x86_64.7z"
MPV_URL="https://downloads.sourceforge.net/project/mpv-player-windows/release/${MPV_ARCHIVE}"
MPV_SHA256="ef86fde0959d789d77a3ad7c3c2dca51c6999695363f493a6154f2c518634c0f"

log() {
  echo "[build-win-linux] $*"
}

get_latest_changelog_version() {
  local changelog_file="$DESKTOP_DIR/CHANGELOG.json"

  if [[ ! -f "$changelog_file" ]]; then
    echo "unknown"
    return 0
  fi

  python3 - <<PY
import json
import re
from pathlib import Path

path = Path(r"$changelog_file")
try:
    data = json.loads(path.read_text(encoding="utf-8"))
except Exception:
    print("unknown")
    raise SystemExit(0)

version = "unknown"
if isinstance(data, dict):
    history = data.get("history")
    if isinstance(history, list) and history:
        latest = history[0]
        if isinstance(latest, dict):
            version = str(latest.get("version") or "unknown")

version = version.strip() or "unknown"
version = re.sub(r"[^0-9A-Za-z._-]+", "_", version)
print(version)
PY
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "缺少命令: $1"
    exit 1
  fi
}

ensure_pip() {
  if python3 -m pip --version >/dev/null 2>&1; then
    return 0
  fi

  log "当前 python3 缺少 pip，尝试 ensurepip"
  python3 -m ensurepip --upgrade
  python3 -m pip --version >/dev/null 2>&1 || {
    echo "python3 的 pip 初始化失败，请手动安装 pip 后重试。"
    exit 1
  }
}

sync_server_source() {
  local src="$ROOT_DIR/server/"
  local dst="$RUNTIME_DIR/server/"

  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete \
      --exclude '.venv' \
      --exclude '__pycache__' \
      --exclude '*.pyc' \
      --exclude '.env' \
      --exclude 'data' \
      --exclude 'uv.lock' \
      "$src" "$dst"
  else
    rm -rf "$dst"
    mkdir -p "$dst"
    cp -a "$src". "$dst"
    rm -rf "$dst/.venv" "$dst/data"
    find "$dst" -name '__pycache__' -type d -prune -exec rm -rf {} + || true
    find "$dst" -name '*.pyc' -type f -delete || true
    rm -f "$dst/.env" "$dst/uv.lock"
  fi
}

prepare_python_embed() {
  local py_root="$RUNTIME_DIR/server/python"
  local cache_zip="$CACHE_DIR/$PYTHON_EMBED_ZIP"

  mkdir -p "$CACHE_DIR"
  if [[ ! -f "$cache_zip" ]]; then
    log "下载 Windows Python Embed: $PYTHON_EMBED_VERSION"
    curl -fL "$PYTHON_EMBED_URL" -o "$cache_zip"
  fi

  rm -rf "$py_root"
  mkdir -p "$py_root"

  log "解压 Python Embed"
  python3 - <<PY
import zipfile
from pathlib import Path
zip_path = Path(r"$cache_zip")
out_dir = Path(r"$py_root")
with zipfile.ZipFile(zip_path) as z:
    z.extractall(out_dir)
PY

  mkdir -p "$py_root/Lib/site-packages"

  local pth_file
  pth_file="$(find "$py_root" -maxdepth 1 -name 'python*._pth' | head -n1 || true)"
  if [[ -z "$pth_file" ]]; then
    echo "未找到 python*._pth，Python Embed 结构异常"
    exit 1
  fi

  local pth_name py_zip
  pth_name="$(basename "$pth_file")"
  py_zip="${pth_name%._pth}.zip"

  cat > "$pth_file" <<PTH
$py_zip
.
Lib/site-packages
import site
PTH
}

download_and_extract_windows_wheels() {
  local site_packages="$RUNTIME_DIR/server/python/Lib/site-packages"
  local req_file="$ROOT_DIR/server/requirements.txt"
  local req_binary="$CACHE_DIR/requirements.win.binary.txt"

  mkdir -p "$WHEEL_DIR"
  rm -f "$WHEEL_DIR"/*.whl

  log "下载 Windows wheels（cp312/win_amd64）"
  grep -v -E '^bencoder(==|>=|<=|>|<|$)' "$req_file" > "$req_binary"

  python3 -m pip download \
    -r "$req_binary" \
    --only-binary=:all: \
    --platform win_amd64 \
    --python-version 312 \
    --implementation cp \
    --abi cp312 \
    -d "$WHEEL_DIR"

  log "补充 Windows 平台标记依赖（colorama/win32-setctime）"
  python3 -m pip download \
    --only-binary=:all: \
    --platform win_amd64 \
    --python-version 312 \
    --implementation cp \
    --abi cp312 \
    -d "$WHEEL_DIR" \
    colorama \
    win32-setctime

  log "构建纯 Python 通用 wheel（bencoder）"
  python3 -m pip wheel --no-deps bencoder -w "$WHEEL_DIR"

  log "解压 wheels 到 site-packages"
  rm -rf "$site_packages"
  mkdir -p "$site_packages"

  python3 - <<PY
import zipfile
from pathlib import Path

wheel_dir = Path(r"$WHEEL_DIR")
site_packages = Path(r"$site_packages")
wheels = sorted(wheel_dir.glob("*.whl"))
if not wheels:
    raise SystemExit("未下载到任何 whl 文件")
for whl in wheels:
    with zipfile.ZipFile(whl) as z:
        z.extractall(site_packages)

required_entries = {
    "win32_setctime": (site_packages / "win32_setctime").exists(),
    "colorama": (site_packages / "colorama").exists(),
}
missing = [name for name, ok in required_entries.items() if not ok]
if missing:
    raise SystemExit(f"缺少关键 Windows 依赖: {', '.join(missing)}")

print(f"extracted_wheels={len(wheels)}")
PY
}


pack_python_runtime_zip() {
  local py_root="$RUNTIME_DIR/server/python"
  local py_zip="$RUNTIME_DIR/server/python.zip"

  if [[ ! -d "$py_root" ]]; then
    echo "未找到 Python 运行时目录: $py_root"
    exit 1
  fi

  log "压缩 Python 运行时为单文件资源（降低 Tauri 构建内存占用）"
  rm -f "$py_zip"

  python3 - <<PY
import zipfile
from pathlib import Path

py_root = Path(r"$py_root")
py_zip = Path(r"$py_zip")

with zipfile.ZipFile(py_zip, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as zf:
    for path in sorted(py_root.rglob("*")):
        if path.is_file():
            zf.write(path, path.relative_to(py_root))

print(f"python_zip_size_bytes={py_zip.stat().st_size}")
PY

  rm -rf "$py_root"

  if [[ ! -f "$py_zip" ]]; then
    echo "Python 运行时压缩失败：未生成 $py_zip"
    exit 1
  fi
}

prepare_windows_media_tools() {
  local tools_root="$RUNTIME_DIR/server/tools"
  local ffmpeg_zip="$CACHE_DIR/$FFMPEG_ARCHIVE"
  local mpv_7z="$CACHE_DIR/$MPV_ARCHIVE"
  local extract_root="$CACHE_DIR/extracted-media"
  local sevenz_bin

  sevenz_bin="$(ensure_7zip_local)"

  mkdir -p "$CACHE_DIR"

  if [[ ! -f "$ffmpeg_zip" ]]; then
    log "下载 Windows FFmpeg: $FFMPEG_ARCHIVE"
    curl -fL "$FFMPEG_URL" -o "$ffmpeg_zip"
  fi

  local ffmpeg_actual_sha
  ffmpeg_actual_sha="$(sha256sum "$ffmpeg_zip" | awk '{print $1}')"
  if [[ "$ffmpeg_actual_sha" != "$FFMPEG_SHA256" ]]; then
    echo "FFmpeg SHA256 校验失败: 期望 $FFMPEG_SHA256, 实际 $ffmpeg_actual_sha"
    exit 1
  fi

  if [[ ! -f "$mpv_7z" ]]; then
    log "下载 Windows MPV: $MPV_ARCHIVE"
    curl -fL "$MPV_URL" -o "$mpv_7z"
  fi

  local mpv_actual_sha
  mpv_actual_sha="$(sha256sum "$mpv_7z" | awk '{print $1}')"
  if [[ "$mpv_actual_sha" != "$MPV_SHA256" ]]; then
    echo "MPV SHA256 校验失败: 期望 $MPV_SHA256, 实际 $mpv_actual_sha"
    exit 1
  fi

  rm -rf "$extract_root" "$tools_root"
  mkdir -p "$extract_root" "$tools_root"

  log "解压并注入 Windows FFmpeg"
  python3 - <<PY
import zipfile
from pathlib import Path

ffmpeg_zip = Path(r"$ffmpeg_zip")
extract_root = Path(r"$extract_root")
tools_root = Path(r"$tools_root")

ffmpeg_extract = extract_root / "ffmpeg"
ffmpeg_extract.mkdir(parents=True, exist_ok=True)
with zipfile.ZipFile(ffmpeg_zip) as zf:
    zf.extractall(ffmpeg_extract)

top_dirs = [p for p in ffmpeg_extract.iterdir() if p.is_dir()]
if len(top_dirs) != 1:
    raise SystemExit(f"FFmpeg 解压结构异常: {top_dirs}")

ffmpeg_bin = top_dirs[0] / "bin"
if not (ffmpeg_bin / "ffmpeg.exe").exists() or not (ffmpeg_bin / "ffprobe.exe").exists():
    raise SystemExit("FFmpeg bin 目录缺少 ffmpeg.exe 或 ffprobe.exe")

ffmpeg_dst = tools_root / "ffmpeg" / "bin"
ffmpeg_dst.parent.mkdir(parents=True, exist_ok=True)
if ffmpeg_dst.exists():
    import shutil
    shutil.rmtree(ffmpeg_dst)
import shutil
shutil.copytree(ffmpeg_bin, ffmpeg_dst)
PY

  log "使用本地 7z 解压 MPV"
  mkdir -p "$extract_root/mpv" "$tools_root/mpv"
  "$sevenz_bin" x "$mpv_7z" "-o$extract_root/mpv" -y >/dev/null

  local mpv_source
  mpv_source="$(find "$extract_root/mpv" -type f -iname 'mpv.exe' | head -n1 || true)"
  if [[ -z "$mpv_source" ]]; then
    echo "MPV 解压后未找到 mpv.exe"
    exit 1
  fi
  cp -f "$mpv_source" "$tools_root/mpv/mpv.exe"

  if [[ ! -f "$tools_root/ffmpeg/bin/ffmpeg.exe" || ! -f "$tools_root/ffmpeg/bin/ffprobe.exe" ]]; then
    echo "FFmpeg 工具注入失败，缺少 ffmpeg.exe 或 ffprobe.exe"
    exit 1
  fi

  if [[ ! -f "$tools_root/mpv/mpv.exe" ]]; then
    echo "MPV 工具注入失败，缺少 mpv.exe"
    exit 1
  fi
}

ensure_7zip_local() {
  local tools_dir="$DESKTOP_DIR/.tools/p7zip-linux"
  local root_dir="$tools_dir/root"
  local bin_path="$root_dir/usr/lib/7zip/7z"
  local sevenzr_path="$root_dir/usr/lib/7zip/7zr"
  local alt_bin_path="$root_dir/usr/lib/p7zip/7z"
  local alt_sevenzr_path="$root_dir/usr/lib/p7zip/7zr"

  if [[ -x "$bin_path" ]]; then
    echo "$bin_path"
    return 0
  fi

  if [[ -x "$sevenzr_path" ]]; then
    echo "$sevenzr_path"
    return 0
  fi

  if [[ -x "$alt_bin_path" ]]; then
    echo "$alt_bin_path"
    return 0
  fi

  if [[ -x "$alt_sevenzr_path" ]]; then
    echo "$alt_sevenzr_path"
    return 0
  fi

  mkdir -p "$tools_dir" "$root_dir"

  local tmp_dir="$tools_dir/.tmp"
  mkdir -p "$tmp_dir"

  pushd "$tmp_dir" >/dev/null

  rm -f 7zip_*.deb p7zip_*.deb p7zip-full_*.deb || true

  # Ubuntu 24.04+ 推荐使用 7zip（非 p7zip），优先尝试该包
  if apt download 7zip >/dev/null 2>&1; then
    shopt -s nullglob
    local sevenzip_debs=(7zip_*.deb)
    shopt -u nullglob
    if (( ${#sevenzip_debs[@]} > 0 )); then
      dpkg-deb -x "${sevenzip_debs[0]}" "$root_dir"
    fi
  fi

  # 兼容旧环境（若上面未得到 7z/7zr）
  if [[ ! -x "$bin_path" && ! -x "$sevenzr_path" ]]; then
    apt download p7zip p7zip-full >/dev/null
    shopt -s nullglob
    local legacy_debs=(p7zip_*.deb p7zip-full_*.deb)
    shopt -u nullglob
    for deb in "${legacy_debs[@]}"; do
      dpkg-deb -x "$deb" "$root_dir"
    done
  fi

  popd >/dev/null

  if [[ -x "$bin_path" ]]; then
    echo "$bin_path"
    return 0
  fi
  if [[ -x "$sevenzr_path" ]]; then
    echo "$sevenzr_path"
    return 0
  fi
  if [[ -x "$alt_bin_path" ]]; then
    echo "$alt_bin_path"
    return 0
  fi
  if [[ -x "$alt_sevenzr_path" ]]; then
    echo "$alt_sevenzr_path"
    return 0
  fi

  echo "未找到可用 7z/7zr 二进制"
  echo "已检查:"
  echo "- $bin_path"
  echo "- $sevenzr_path"
  echo "- $alt_bin_path"
  echo "- $alt_sevenzr_path"
  exit 1
}

ensure_nsis_local() {
  local tools_dir="$DESKTOP_DIR/.tools/nsis-linux"
  local nsis_root="$tools_dir/root"
  local bin_dir="$tools_dir/bin"
  local wrapper="$bin_dir/makensis.exe"

  if [[ -x "$wrapper" ]]; then
    echo "$wrapper"
    return 0
  fi

  mkdir -p "$tools_dir" "$nsis_root" "$bin_dir"

  local tmp_dir="$tools_dir/.tmp"
  mkdir -p "$tmp_dir"

  pushd "$tmp_dir" >/dev/null
  apt download nsis nsis-common >/dev/null
  dpkg-deb -x nsis_*_amd64.deb "$nsis_root"
  dpkg-deb -x nsis-common_*_all.deb "$nsis_root"
  popd >/dev/null

  cat > "$wrapper" <<'WRAP'
#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../root" && pwd)"
export NSISDIR="${NSISDIR:-$ROOT_DIR/usr/share/nsis}"
exec "$ROOT_DIR/usr/bin/makensis" "$@"
WRAP
  chmod +x "$wrapper"

  echo "$wrapper"
}

build_runtime() {
  log "清理并重建 runtime"
  rm -rf "$RUNTIME_DIR"
  mkdir -p "$RUNTIME_DIR/server" "$RUNTIME_DIR/batch" "$RUNTIME_DIR/updater" "$RUNTIME_DIR/data/tmp"

  if [[ -f "$DESKTOP_DIR/templates/runtime.env.example" ]]; then
    cp "$DESKTOP_DIR/templates/runtime.env.example" "$RUNTIME_DIR/data/runtime.env.example"
  fi

  log "构建 webui"
  (
    cd "$ROOT_DIR/webui"
    bun install
    if ! bun run build; then
      log "bun run build 失败，回退使用 node 直接调用 vite"
      node ./node_modules/vite/bin/vite.js build
    fi
  )

  log "构建 Go sidecar (windows/amd64)"
  (
    cd "$ROOT_DIR/batch"
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags='-s -w' -o "$RUNTIME_DIR/batch/batch.exe" batch.go
  )
  (
    cd "$ROOT_DIR/updater"
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags='-s -w' -o "$RUNTIME_DIR/updater/updater.exe" updater.go
  )

  log "同步 server 源码"
  sync_server_source

  # 仅保留 Windows BDInfo 工具，避免把 Linux 大文件打进 Windows 安装包
  if [[ -d "$RUNTIME_DIR/server/core/bdinfo/linux" ]]; then
    rm -rf "$RUNTIME_DIR/server/core/bdinfo/linux"
  fi

  if [[ ! -f "$RUNTIME_DIR/server/background_runner.py" ]]; then
    echo "缺少 background_runner.py: $RUNTIME_DIR/server/background_runner.py"
    exit 1
  fi

  log "注入 webui dist 到 runtime/server/dist"
  rm -rf "$RUNTIME_DIR/server/dist"
  cp -a "$ROOT_DIR/webui/dist" "$RUNTIME_DIR/server/dist"

  log "准备 Python Embed 运行时"
  prepare_python_embed

  log "安装 Windows Python 依赖"
  download_and_extract_windows_wheels

  log "准备 Windows 媒体工具 (mpv/ffmpeg)"
  prepare_windows_media_tools

  pack_python_runtime_zip

  log "准备版本文件"
  cp "$ROOT_DIR/CHANGELOG.json" "$DESKTOP_DIR/CHANGELOG.json"
}

build_installer() {
  local nsis_wrapper
  nsis_wrapper="$(ensure_nsis_local)"

  local tools_bin_dir
  tools_bin_dir="$(dirname "$nsis_wrapper")"
  local nsis_root
  nsis_root="$(cd "$tools_bin_dir/../root" && pwd)"
  local tauri_cli="$DESKTOP_DIR/node_modules/@tauri-apps/cli/tauri.js"

  if [[ ! -f "$tauri_cli" ]]; then
    echo "未找到 Tauri CLI: $tauri_cli"
    echo "请先在 desktop 目录执行 bun install"
    exit 1
  fi

  cd "$DESKTOP_DIR"
  if [[ -n "${CARGO_BUILD_JOBS:-}" ]]; then
    PATH="$tools_bin_dir:$PATH" NSISDIR="$nsis_root/usr/share/nsis" CARGO_BUILD_JOBS="$CARGO_BUILD_JOBS" node "$tauri_cli" build --target x86_64-pc-windows-gnu
  else
    if ! PATH="$tools_bin_dir:$PATH" NSISDIR="$nsis_root/usr/share/nsis" node "$tauri_cli" build --target x86_64-pc-windows-gnu; then
      log "首次构建失败，自动回退 CARGO_BUILD_JOBS=1 重试"
      PATH="$tools_bin_dir:$PATH" NSISDIR="$nsis_root/usr/share/nsis" CARGO_BUILD_JOBS=1 node "$tauri_cli" build --target x86_64-pc-windows-gnu
    fi
  fi

  local installer_dir="$DESKTOP_DIR/src-tauri/target/x86_64-pc-windows-gnu/release/bundle/nsis"
  local installer_path
  installer_path="$(ls -1t "$installer_dir"/PT\ Nexus_*_x64-setup.exe 2>/dev/null | head -n1 || true)"
  if [[ -z "$installer_path" || ! -f "$installer_path" ]]; then
    echo "安装包未生成: $installer_dir/PT Nexus_*_x64-setup.exe"
    exit 1
  fi

  local installer_size_bytes
  installer_size_bytes="$(stat -c%s "$installer_path")"
  local min_expected_bytes=$((20 * 1024 * 1024))
  if (( installer_size_bytes < min_expected_bytes )); then
    echo "安装包体积异常（$(numfmt --to=iec "$installer_size_bytes")），疑似未打入 runtime。"
    echo "请检查 tauri.conf.json 的 bundle.resources 配置。"
    exit 1
  fi

  local changelog_version
  changelog_version="$(get_latest_changelog_version)"

  local release_dir="$DESKTOP_DIR/release"
  mkdir -p "$release_dir"

  local desktop_copy_path="$release_dir/PT Nexus_${changelog_version}_x64-setup.exe"
  cp -f "$installer_path" "$desktop_copy_path"

  echo "installer_ready:$installer_path"
  echo "installer_copied:$desktop_copy_path"
}

main() {
  require_cmd bun
  require_cmd node
  require_cmd go
  require_cmd python3
  require_cmd curl
  require_cmd sha256sum
  require_cmd apt
  require_cmd dpkg-deb

  ensure_pip
  build_runtime

  log "安装 desktop 依赖"
  (cd "$DESKTOP_DIR" && bun install)

  log "构建单文件安装包（NSIS）"
  build_installer

  log "完成"
  log "安装包目录: $DESKTOP_DIR/src-tauri/target/x86_64-pc-windows-gnu/release/bundle/nsis"
  log "发布目录副本（版本号）: $DESKTOP_DIR/release/PT Nexus_<changelog_version>_x64-setup.exe"
}

main "$@"
