#!/bin/bash

# 添加环境变量
export no_proxy="localhost,127.0.0.1,::1"
export NO_PROXY="localhost,127.0.0.1,::1"

# --- 自检与自愈（用于旧镜像在线更新后依赖缺失） ---
is_truthy() {
    case "${1:-}" in
        1|true|TRUE|yes|YES|y|Y|on|ON) return 0 ;;
        *) return 1 ;;
    esac
}

log_bootstrap() {
    echo "[bootstrap] $*"
}

find_supervisord() {
    if command -v supervisord >/dev/null 2>&1; then
        command -v supervisord
        return 0
    fi
    for p in /usr/bin/supervisord /usr/local/bin/supervisord; do
        if [ -x "$p" ]; then
            echo "$p"
            return 0
        fi
    done
    return 1
}

ensure_bootstrap_dir() {
    mkdir -p /app/data/.bootstrap 2>/dev/null || true
}

ensure_system_deps() {
    # 默认开启（你也可以通过环境变量关闭）
    if ! is_truthy "${AUTO_INSTALL_SYSTEM_DEPS:-true}"; then
        if ! find_supervisord >/dev/null 2>&1; then
            echo "未找到 supervisord，且 AUTO_INSTALL_SYSTEM_DEPS=false，无法继续启动编排。" >&2
        fi
        return 0
    fi

    local missing=()
    local cmd
    for cmd in supervisord ffmpeg mpv mediainfo git; do
        command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
    done

    if [ ${#missing[@]} -eq 0 ]; then
        return 0
    fi

    log_bootstrap "检测到缺失命令: ${missing[*]}，尝试安装系统依赖..."

    if [ "$(id -u 2>/dev/null || echo 1)" -ne 0 ]; then
        echo "当前容器非 root，无法自动安装系统依赖。请拉取/重建最新镜像，或手动进入容器安装 supervisor 等依赖。" >&2
        return 1
    fi

    local apt_cmd=""
    if command -v apt-get >/dev/null 2>&1; then
        apt_cmd="apt-get"
    elif command -v apt >/dev/null 2>&1; then
        apt_cmd="apt"
    fi

    if [ -z "$apt_cmd" ]; then
        echo "当前镜像缺少 apt/apt-get，无法自动安装系统依赖。建议拉取/重建最新镜像。" >&2
        return 1
    fi

    ensure_bootstrap_dir

    local install_cmd="
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
$apt_cmd update
$apt_cmd install -y --no-install-recommends \
  supervisor \
  ffmpeg \
  mpv \
  mediainfo \
  fonts-noto-cjk \
  git \
  libicu-dev \
  ca-certificates
$apt_cmd clean || true
rm -rf /var/lib/apt/lists/* || true
"

    if command -v flock >/dev/null 2>&1; then
        local lock_file="/app/data/.bootstrap/deps.lock"
        # 等待最多 5 分钟，避免多实例同时 apt 导致锁冲突
        flock -w 300 "$lock_file" bash -lc "$install_cmd"
    else
        bash -lc "$install_cmd"
    fi

    if ! find_supervisord >/dev/null 2>&1; then
        echo "系统依赖安装后仍未找到 supervisord，无法继续启动。建议拉取/重建最新镜像。" >&2
        return 1
    fi
}

hash_file_sha256() {
    local file="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$file" | awk '{print $1}'
        return 0
    fi
    python3 - "$file" <<'PY'
import hashlib, sys
p = sys.argv[1]
h = hashlib.sha256()
with open(p, "rb") as f:
    for chunk in iter(lambda: f.read(1024 * 1024), b""):
        h.update(chunk)
print(h.hexdigest())
PY
}

ensure_python_deps() {
    if ! is_truthy "${AUTO_INSTALL_PIP_DEPS:-true}"; then
        log_bootstrap "pip 依赖同步已关闭（AUTO_INSTALL_PIP_DEPS=false）"
        return 0
    fi

    local req="/app/requirements.txt"
    if [ ! -f "$req" ]; then
        log_bootstrap "未找到 $req，跳过 pip 依赖同步。"
        return 0
    fi

    local mode="${AUTO_PIP_SYNC_MODE:-always}"
    log_bootstrap "pip 依赖同步检查（mode=${mode}, req=${req}）"

    ensure_bootstrap_dir
    local hash_file="/app/data/.bootstrap/requirements.sha256"
    local new_hash
    new_hash="$(hash_file_sha256 "$req" 2>/dev/null || true)"
    if [ "$mode" = "changed" ]; then
        if [ -z "$new_hash" ]; then
            echo "计算 requirements.txt 哈希失败，跳过 pip 依赖同步（mode=changed）。" >&2
            return 0
        fi

        local old_hash=""
        if [ -f "$hash_file" ]; then
            old_hash="$(cat "$hash_file" 2>/dev/null || true)"
        fi

        if [ "$new_hash" = "$old_hash" ]; then
            log_bootstrap "requirements 未变化（mode=changed），跳过 pip 依赖同步。"
            return 0
        fi
    fi

    log_bootstrap "开始安装/更新 pip 依赖..."
    if command -v flock >/dev/null 2>&1; then
        local pip_lock="/app/data/.bootstrap/pip.lock"
        if ! flock -w 300 "$pip_lock" python3 -m pip install --no-cache-dir -r "$req"; then
            echo "pip 依赖安装失败，无法继续启动。建议检查网络/镜像环境，或拉取/重建最新镜像。" >&2
            return 1
        fi
    elif ! python3 -m pip install --no-cache-dir -r "$req"; then
        echo "pip 依赖安装失败，无法继续启动。建议检查网络/镜像环境，或拉取/重建最新镜像。" >&2
        return 1
    fi

    if [ -n "$new_hash" ]; then
        echo "$new_hash" > "$hash_file"
    fi
}

# 自动应用容器内更新（如果repo有新版本）
auto_apply_update() {
    local REPO_CONFIG="/app/data/updates/repo/CHANGELOG.json"
    local LOCAL_CONFIG="/app/CHANGELOG.json"

    # 检查repo配置文件是否存在
    if [ ! -f "$REPO_CONFIG" ]; then
        echo "未找到repo更新配置，跳过自动更新检查"
        return
    fi

    # 获取版本号 (使用简单的grep提取)
    repo_version=$(grep '"version"' "$REPO_CONFIG" | head -1 | sed -E 's/.*"version": *"([^"]*)".*/\1/')
    local_version=$(grep '"version"' "$LOCAL_CONFIG" | head -1 | sed -E 's/.*"version": *"([^"]*)".*/\1/')

    echo "本地版本: $local_version, Repo版本: $repo_version"

    # ================= 修复部分开始 =================
    # 使用 Python 进行语义化版本比较 (Repo > Local)
    # 只有当 Repo 版本确实大于 Local 版本时，才输出 'update'
    should_update=$(python3 -c "
try:
    def parse_version(v):
        # 去掉 v 或 V，按 . 分割，转为数字列表
        return [int(x) for x in v.strip().lstrip('vV').split('.')]
    
    repo = parse_version('$repo_version')
    local = parse_version('$local_version')
    
    # 比较列表，Python 原生支持 [3,3,3] > [3,3,0] 这种比较
    if repo > local:
        print('update')
    else:
        print('skip')
except Exception as e:
    # 如果解析出错（比如版本号格式不对），默认不更新，防止破坏
    print('error')
")
    # ================= 修复部分结束 =================

    if [ "$should_update" = "update" ]; then
        echo "检测到新版本 ($repo_version > $local_version)，自动应用更新..."

        # 使用python解析JSON并同步文件
        python3 -c "
import json, os, shutil, sys

try:
    with open('$REPO_CONFIG', 'r') as f:
        config = json.load(f)

    for mapping in config['mappings']:
        source = os.path.join('/app/data/updates/repo', mapping['source'])
        target = mapping['target']
        exclude = mapping.get('exclude', []) + ['*.pyc', '__pycache__', '*.backup', '.env']
        executable = mapping.get('executable', False)
        
        print(f'同步 {source} -> {target}')
        if os.path.isdir(source):
            # 用shutil复制目录，跳过exclude
            for root, dirs, files in os.walk(source):
                rel_root = os.path.relpath(root, source)
                # 过滤目录
                for d in dirs[:]:
                    if any(d == pat or d.endswith(pat.replace('*', '')) for pat in exclude):
                        dirs.remove(d)
                # 过滤文件
                for file in files:
                    if any(file == pat or file.endswith(pat.replace('*', '')) for pat in exclude):
                        continue
                    src_file = os.path.join(root, file)
                    dst_file = os.path.join(target, rel_root, file)
                    os.makedirs(os.path.dirname(dst_file), exist_ok=True)
                    shutil.copy2(src_file, dst_file)
        elif os.path.isfile(source):
            os.makedirs(os.path.dirname(target), exist_ok=True)
            shutil.copy2(source, target)
        
        if executable:
            os.chmod(target, 0o755)
            
except Exception as e:
    print(f'更新文件时发生错误: {e}')
    sys.exit(1)
"
        # 只有 Python 脚本执行成功才覆盖版本文件
        if [ $? -eq 0 ]; then
            cp "$REPO_CONFIG" "$LOCAL_CONFIG"
            echo "更新应用完成，新版本: $repo_version"
        else
            echo "文件同步失败，跳过版本号更新"
        fi

    elif [ "$should_update" = "error" ]; then
        echo "版本号解析错误，跳过自动更新检查"
    else
        echo "本地版本 ($local_version) 已经是最新或更高，跳过启动更新"
    fi
}

# 执行自动更新检查
auto_apply_update


# 在线更新后，尝试补齐旧镜像可能缺失的依赖（系统包 + pip 包）
ensure_system_deps || exit 1
ensure_python_deps || exit 1


# 使用 supervisord 统一管理 updater/background_runner/server/batch
echo "启动 supervisord 进行多服务编排..."
SUPERVISORD_BIN="$(find_supervisord)" || {
    echo "未找到 supervisord，无法启动编排。建议拉取/重建最新镜像。" >&2
    exit 1
}
exec "$SUPERVISORD_BIN" -n -c /app/supervisord.conf
