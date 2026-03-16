#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKFLOW_FILE="release-manual.yml"
WORKFLOW_PATH="$REPO_ROOT/.github/workflows/$WORKFLOW_FILE"
CHANGELOG_FILE="$REPO_ROOT/CHANGELOG.json"

TITLE_RUNTIME="在线更新文件"
TITLE_DESKTOP="Windows 桌面安装包"
TITLE_PROXY="盒子端代理"
TITLE_DOCKER="Docker 镜像"
TITLE_WATCH="自动跟踪进度"
TITLE_INSTALL_GH="是否现在尝试安装 gh"
PROMPT_LABEL_WIDTH=0
SUMMARY_LABEL_WIDTH=0

build_runtime=true
build_desktop_windows=true
build_proxy=true
build_docker_image=true
watch_workflow=true
dry_run=false
ref_override=""
build_flags_explicit=false

usage() {
  cat <<'USAGE'
Usage: ./scripts/release-manual-dispatch.sh [options]

Options:
  --ref <branch>     指定 push 与 workflow dispatch 的分支
  --all              直接启用全部四项发布并跳过交互选择
  --no-runtime       跳过 Runtime 发布产物
  --no-desktop       跳过 Windows 桌面安装包
  --no-proxy         跳过盒子端代理产物
  --no-docker        跳过 Docker 镜像
  --no-watch         触发 workflow 后不自动跟踪执行进度
  --dry-run          只打印命令，不实际执行
  --help, -h         显示帮助

Examples:
  ./scripts/release-manual-dispatch.sh
  ./scripts/release-manual-dispatch.sh --dry-run
  ./scripts/release-manual-dispatch.sh --all
  ./scripts/release-manual-dispatch.sh --ref go --no-desktop
  ./scripts/release-manual-dispatch.sh --all --no-watch

Notes:
  未显式传入构建开关时，脚本会逐项使用 Y/N 询问本次要发布的内容。
  使用 --all 时，会直接启用全部四项发布并跳过交互选择。
  默认会在 workflow_dispatch 成功后自动执行 gh run watch 跟踪进度。
USAGE
}

fail() {
  echo "Error: $*" >&2
  exit 1
}

warn() {
  echo "Warn: $*" >&2
}

is_interactive_terminal() {
  [[ -t 0 && -t 1 ]]
}

print_cmd() {
  printf '+ '
  printf '%q ' "$@"
  printf '\n'
}

run_cmd() {
  print_cmd "$@"
  if [[ "$dry_run" == "true" ]]; then
    return 0
  fi
  "$@"
}

run_install_cmd() {
  print_cmd "$@"
  "$@"
}

display_width() {
  python3 - "$1" <<'PY'
import sys
import unicodedata

text = sys.argv[1]
width = 0
for char in text:
    width += 2 if unicodedata.east_asian_width(char) in {'W', 'F'} else 1
print(width)
PY
}

compute_label_width() {
  local width=0
  local label label_width
  for label in "$@"; do
    label_width="$(display_width "$label")"
    if (( label_width > width )); then
      width=$label_width
    fi
  done
  echo "$width"
}

print_padded_label() {
  local label="$1"
  local target_width="$2"
  local label_width padding
  label_width="$(display_width "$label")"
  padding=$(( target_width - label_width ))
  if (( padding < 0 )); then
    padding=0
  fi
  printf '%s' "$label"
  printf '%*s' "$padding" ''
}

prompt_yes_no() {
  local label="$1"
  local default_choice="$2"
  local prompt_hint
  local reply

  if [[ "$default_choice" == "true" ]]; then
    prompt_hint='[Y/n]'
  else
    prompt_hint='[y/N]'
  fi

  while true; do
    printf '  '
    print_padded_label "$label" "$PROMPT_LABEL_WIDTH"
    printf ' %s ' "$prompt_hint"
    IFS= read -r reply || true
    case "$reply" in
      "")
        [[ "$default_choice" == "true" ]] && return 0
        return 1
        ;;
      [Yy]|[Yy][Ee][Ss])
        return 0
        ;;
      [Nn]|[Nn][Oo])
        return 1
        ;;
      *)
        echo "请输入 y 或 n。"
        ;;
    esac
  done
}

print_config_line() {
  printf '  '
  print_padded_label "$1" "$SUMMARY_LABEL_WIDTH"
  printf ' %s\n' "$2"
}

resolve_default_ref() {
  if git -C "$REPO_ROOT" rev-parse --verify --quiet refs/heads/go >/dev/null; then
    printf 'go\n'
    return 0
  fi

  local origin_head
  origin_head="$(git -C "$REPO_ROOT" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || true)"
  if [[ -n "$origin_head" ]]; then
    printf '%s\n' "${origin_head#origin/}"
    return 0
  fi

  git -C "$REPO_ROOT" symbolic-ref --quiet --short HEAD 2>/dev/null || true
}

resolve_repo_slug() {
  local remote_url="$1"
  case "$remote_url" in
    git@github.com:*)
      remote_url="${remote_url#git@github.com:}"
      ;;
    https://github.com/*)
      remote_url="${remote_url#https://github.com/}"
      ;;
    http://github.com/*)
      remote_url="${remote_url#http://github.com/}"
      ;;
    *)
      return 1
      ;;
  esac
  printf '%s\n' "${remote_url%.git}"
}

read_changelog_version() {
  python3 - "$CHANGELOG_FILE" <<'PY'
import json
import sys

with open(sys.argv[1], 'r', encoding='utf-8') as f:
    data = json.load(f)

history = data.get('history') or []
if not history:
    raise SystemExit(1)

version = str((history[0] or {}).get('version', '')).strip()
if not version:
    raise SystemExit(1)

print(version)
PY
}

prompt_build_selection() {
  if [[ "$build_flags_explicit" == "true" ]]; then
    return 0
  fi
  if ! is_interactive_terminal; then
    return 0
  fi

  echo "请选择本次需要执行的发布内容："
  echo "  直接回车会采用默认值 Y。"

  if prompt_yes_no "$TITLE_RUNTIME" true; then
    build_runtime=true
  else
    build_runtime=false
  fi

  if prompt_yes_no "$TITLE_DESKTOP" true; then
    build_desktop_windows=true
  else
    build_desktop_windows=false
  fi

  if prompt_yes_no "$TITLE_PROXY" true; then
    build_proxy=true
  else
    build_proxy=false
  fi

  if prompt_yes_no "$TITLE_DOCKER" true; then
    build_docker_image=true
  else
    build_docker_image=false
  fi

  echo
}

install_gh_cli() {
  local -a prefix=()

  if [[ "$(id -u)" -ne 0 ]]; then
    command -v sudo >/dev/null 2>&1 || fail "需要 sudo 才能自动安装 gh，请先手动安装：https://cli.github.com/"
    prefix=(sudo)
  fi

  if command -v apt-get >/dev/null 2>&1; then
    echo "尝试使用 apt 安装 gh..."
    run_install_cmd "${prefix[@]}" apt-get update
    run_install_cmd "${prefix[@]}" apt-get install -y gh
    return 0
  fi

  if command -v dnf >/dev/null 2>&1; then
    echo "尝试使用 dnf 安装 gh..."
    run_install_cmd "${prefix[@]}" dnf install -y gh
    return 0
  fi

  if command -v yum >/dev/null 2>&1; then
    echo "尝试使用 yum 安装 gh..."
    run_install_cmd "${prefix[@]}" yum install -y gh
    return 0
  fi

  if command -v pacman >/dev/null 2>&1; then
    echo "尝试使用 pacman 安装 gh..."
    run_install_cmd "${prefix[@]}" pacman -Sy --noconfirm github-cli
    return 0
  fi

  if command -v zypper >/dev/null 2>&1; then
    echo "尝试使用 zypper 安装 gh..."
    run_install_cmd "${prefix[@]}" zypper install -y gh
    return 0
  fi

  if command -v brew >/dev/null 2>&1; then
    echo "尝试使用 brew 安装 gh..."
    run_install_cmd brew install gh
    return 0
  fi

  fail "未找到可自动安装 gh 的包管理器，请手动安装：https://cli.github.com/"
}

ensure_gh_cli() {
  if command -v gh >/dev/null 2>&1; then
    return 0
  fi

  if is_interactive_terminal; then
    echo "未检测到 GitHub CLI (gh)。"
    if prompt_yes_no "$TITLE_INSTALL_GH" false; then
      if install_gh_cli && command -v gh >/dev/null 2>&1; then
        return 0
      fi
      if [[ "$dry_run" == "true" ]]; then
        warn "自动安装 gh 失败；dry-run 仅打印命令，不校验 GitHub CLI。"
        return 1
      fi
      fail "自动安装 gh 失败，请手动安装后重试：https://cli.github.com/"
    fi
  fi

  if [[ "$dry_run" == "true" ]]; then
    warn "gh 未安装；dry-run 仅打印命令，不校验 GitHub CLI。"
    return 1
  fi

  fail "未找到 gh，请先安装 GitHub CLI：https://cli.github.com/"
}

find_dispatched_run_id() {
  local repo_slug="$1"
  local ref="$2"
  local head_sha="$3"
  local dispatch_started_at="$4"
  local run_id=""
  local attempt=0

  while (( attempt < 20 )); do
    run_id="$({
      gh run list \
        --repo "$repo_slug" \
        --workflow "$WORKFLOW_FILE" \
        --branch "$ref" \
        --event workflow_dispatch \
        --limit 20 \
        --json databaseId,headBranch,headSha,createdAt 2>/dev/null || true
    } | python3 - "$head_sha" "$ref" "$dispatch_started_at" <<'PY'
import datetime as dt
import json
import sys

head_sha = sys.argv[1]
ref = sys.argv[2]
dispatch_started_at = sys.argv[3]

try:
    threshold = dt.datetime.fromisoformat(dispatch_started_at.replace('Z', '+00:00'))
except ValueError:
    threshold = None

try:
    runs = json.load(sys.stdin)
except json.JSONDecodeError:
    runs = []

candidates = []
for item in runs:
    if item.get('headSha') != head_sha:
        continue
    if item.get('headBranch') != ref:
        continue

    created_at = item.get('createdAt') or ''
    if threshold is not None and created_at:
        try:
            created = dt.datetime.fromisoformat(created_at.replace('Z', '+00:00'))
        except ValueError:
            continue
        if created < threshold:
            continue

    run_id = item.get('databaseId')
    if run_id:
        candidates.append(str(run_id))

if candidates:
    print(candidates[0])
PY
    )"

    if [[ -n "$run_id" ]]; then
      printf '%s\n' "$run_id"
      return 0
    fi

    attempt=$(( attempt + 1 ))
    sleep 3
  done

  return 1
}

PROMPT_LABEL_WIDTH="$(compute_label_width "$TITLE_RUNTIME" "$TITLE_DESKTOP" "$TITLE_PROXY" "$TITLE_DOCKER" "$TITLE_INSTALL_GH")"
SUMMARY_LABEL_WIDTH="$(compute_label_width 仓库 发布分支 版本 "$TITLE_RUNTIME" "$TITLE_DESKTOP" "$TITLE_PROXY" "$TITLE_DOCKER" "$TITLE_WATCH" 模式)"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ref)
      [[ $# -ge 2 ]] || fail "--ref 需要一个分支名"
      ref_override="$2"
      shift 2
      ;;
    --all)
      build_runtime=true
      build_desktop_windows=true
      build_proxy=true
      build_docker_image=true
      build_flags_explicit=true
      shift
      ;;
    --no-runtime)
      build_runtime=false
      build_flags_explicit=true
      shift
      ;;
    --no-desktop)
      build_desktop_windows=false
      build_flags_explicit=true
      shift
      ;;
    --no-proxy)
      build_proxy=false
      build_flags_explicit=true
      shift
      ;;
    --no-docker)
      build_docker_image=false
      build_flags_explicit=true
      shift
      ;;
    --no-watch)
      watch_workflow=false
      shift
      ;;
    --dry-run)
      dry_run=true
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      fail "未知参数：$1"
      ;;
  esac
done

[[ -f "$WORKFLOW_PATH" ]] || fail "未找到 workflow：$WORKFLOW_PATH"
[[ -f "$CHANGELOG_FILE" ]] || fail "未找到 CHANGELOG.json：$CHANGELOG_FILE"

git -C "$REPO_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1 || fail "当前目录不在 Git 仓库内"
git -C "$REPO_ROOT" remote get-url origin >/dev/null 2>&1 || fail "未找到 origin 远端"

current_branch="$(git -C "$REPO_ROOT" symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
[[ -n "$current_branch" ]] || fail "当前不在可 push 的本地分支上"

ref="${ref_override:-$(resolve_default_ref)}"
[[ -n "$ref" ]] || fail "无法推断默认发布分支，请使用 --ref 显式指定"

if [[ "$current_branch" != "$ref" ]]; then
  fail "当前分支是 $current_branch，但发布目标分支是 $ref。请切换到 $ref 后重试，或显式传入 --ref $current_branch。"
fi

prompt_build_selection

if [[ "$build_runtime" == "false" && "$build_desktop_windows" == "false" && "$build_proxy" == "false" && "$build_docker_image" == "false" ]]; then
  fail "至少需要启用一项构建内容。"
fi

origin_url="$(git -C "$REPO_ROOT" remote get-url origin)"
repo_slug="$(resolve_repo_slug "$origin_url" || true)"
[[ -n "$repo_slug" ]] || fail "origin 不是 GitHub 仓库地址：$origin_url"

version="$(read_changelog_version 2>/dev/null || true)"
[[ -n "$version" ]] || fail "CHANGELOG.json 缺少 history[0].version"

head_sha="$(git -C "$REPO_ROOT" rev-parse HEAD)"

if ensure_gh_cli && [[ "$dry_run" != "true" ]]; then
  gh auth status >/dev/null 2>&1 || fail "gh 尚未登录，请先执行 gh auth login"
fi

echo "Build config:"
print_config_line "仓库" "$repo_slug"
print_config_line "发布分支" "$ref"
print_config_line "版本" "$version"
print_config_line "$TITLE_RUNTIME" "$build_runtime"
print_config_line "$TITLE_DESKTOP" "$build_desktop_windows"
print_config_line "$TITLE_PROXY" "$build_proxy"
print_config_line "$TITLE_DOCKER" "$build_docker_image"
print_config_line "$TITLE_WATCH" "$watch_workflow"

if [[ "$dry_run" == "true" ]]; then
  print_config_line "模式" "dry-run"
fi

run_cmd git -C "$REPO_ROOT" push origin "$ref"
dispatch_started_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
run_cmd gh workflow run "$WORKFLOW_FILE" \
  --repo "$repo_slug" \
  --ref "$ref" \
  -f "build_runtime=$build_runtime" \
  -f "build_desktop_windows=$build_desktop_windows" \
  -f "build_proxy=$build_proxy" \
  -f "build_docker_image=$build_docker_image"

if [[ "$dry_run" == "true" ]]; then
  if [[ "$watch_workflow" == "true" ]]; then
    print_cmd gh run watch --repo "$repo_slug"
  fi
  exit 0
fi

echo
echo "构建 workflow 已触发。"

if [[ "$watch_workflow" == "true" ]]; then
  echo "正在定位本次 workflow run，并自动跟踪进度..."
  run_id="$(find_dispatched_run_id "$repo_slug" "$ref" "$head_sha" "$dispatch_started_at" || true)"
  if [[ -n "$run_id" ]]; then
    run_cmd gh run watch "$run_id" --repo "$repo_slug"
    exit 0
  fi

  warn "未能自动定位本次 workflow run，请改用手动查看状态。"
fi

echo "可使用以下命令查看状态："
echo "  gh run list --repo $repo_slug --workflow $WORKFLOW_FILE --limit 5"
echo "  gh run watch --repo $repo_slug"
