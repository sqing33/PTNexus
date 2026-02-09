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

log() {
  echo "[build-win-linux] $*"
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
  (cd "$ROOT_DIR/webui" && bun install && bun run build)

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

  log "注入 webui dist 到 runtime/server/dist"
  rm -rf "$RUNTIME_DIR/server/dist"
  cp -a "$ROOT_DIR/webui/dist" "$RUNTIME_DIR/server/dist"

  log "准备 Python Embed 运行时"
  prepare_python_embed

  log "安装 Windows Python 依赖"
  download_and_extract_windows_wheels

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

  cd "$DESKTOP_DIR"
  if [[ -n "${CARGO_BUILD_JOBS:-}" ]]; then
    PATH="$tools_bin_dir:$PATH" NSISDIR="$nsis_root/usr/share/nsis" CARGO_BUILD_JOBS="$CARGO_BUILD_JOBS" bunx tauri build --target x86_64-pc-windows-gnu
  else
    if ! PATH="$tools_bin_dir:$PATH" NSISDIR="$nsis_root/usr/share/nsis" bunx tauri build --target x86_64-pc-windows-gnu; then
      log "首次构建失败，自动回退 CARGO_BUILD_JOBS=1 重试"
      PATH="$tools_bin_dir:$PATH" NSISDIR="$nsis_root/usr/share/nsis" CARGO_BUILD_JOBS=1 bunx tauri build --target x86_64-pc-windows-gnu
    fi
  fi

  local installer_path="$DESKTOP_DIR/src-tauri/target/x86_64-pc-windows-gnu/release/bundle/nsis/PT Nexus_0.1.0_x64-setup.exe"
  if [[ ! -f "$installer_path" ]]; then
    echo "安装包未生成: $installer_path"
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

  echo "installer_ready:$installer_path"
}

main() {
  require_cmd bun
  require_cmd go
  require_cmd python3
  require_cmd curl
  require_cmd apt
  require_cmd dpkg-deb

  ensure_pip
  build_runtime

  log "安装 desktop 依赖"
  (cd "$DESKTOP_DIR" && bun install)

  log "构建单文件安装包（NSIS）"
  build_installer

  log "完成"
  log "安装包位置: $DESKTOP_DIR/src-tauri/target/x86_64-pc-windows-gnu/release/bundle/nsis/PT Nexus_0.1.0_x64-setup.exe"
}

main "$@"
