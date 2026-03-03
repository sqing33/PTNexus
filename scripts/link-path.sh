#!/usr/bin/env bash
set -euo pipefail

# 默认路径（可直接在脚本里修改）
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_TARGET="$REPO_ROOT/.codex/skills"
DEFAULT_LINK_PATH="$REPO_ROOT/.claude/skills"

usage() {
  cat <<EOF
用法:
  bash scripts/link-path.sh [--force] [--no-backup] [--target 路径] [--link 路径]

说明:
  - 默认真实路径: $DEFAULT_TARGET
  - 默认软链路径: $DEFAULT_LINK_PATH
  - 不传参数时，会直接按上面两个默认路径执行。
  - --force: 若软链路径已存在，直接删除后重建（不备份）。
  - --no-backup: 若软链路径已存在，不备份，直接报错退出。
  - --target: 覆盖真实路径（可选）。
  - --link: 覆盖软链路径（可选）。

示例:
  bash scripts/link-path.sh
  bash scripts/link-path.sh --force
  bash scripts/link-path.sh --target /tmp/a.txt --link /tmp/b.txt
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

TARGET="$DEFAULT_TARGET"
LINK_PATH="$DEFAULT_LINK_PATH"

FORCE=false
BACKUP=true

while [ $# -gt 0 ]; do
  case "$1" in
    --target)
      [ $# -ge 2 ] || { echo "[link-path] error: --target requires a value." >&2; exit 2; }
      TARGET="$2"
      shift 2
      continue
      ;;
    --link)
      [ $# -ge 2 ] || { echo "[link-path] error: --link requires a value." >&2; exit 2; }
      LINK_PATH="$2"
      shift 2
      continue
      ;;
    --force)
      FORCE=true
      BACKUP=false
      ;;
    --no-backup)
      BACKUP=false
      ;;
    *)
      echo "[link-path] error: unknown option '$1'" >&2
      usage
      exit 2
      ;;
  esac
  shift
done

if [ ! -e "$TARGET" ]; then
  echo "[link-path] error: target path does not exist: $TARGET" >&2
  exit 1
fi

TARGET_ABS="$(realpath "$TARGET")"
LINK_ABS="$(realpath -m "$LINK_PATH")"

if [ -L "$LINK_PATH" ]; then
  CURRENT_TARGET="$(realpath "$LINK_PATH" || true)"
  if [ "$CURRENT_TARGET" = "$TARGET_ABS" ]; then
    echo "[link-path] already linked:"
    echo "  $LINK_PATH -> $TARGET_ABS"
    exit 0
  fi
fi

if [ "$TARGET_ABS" = "$LINK_ABS" ]; then
  echo "[link-path] error: target and link path are the same: $TARGET_ABS" >&2
  exit 1
fi

mkdir -p "$(dirname "$LINK_PATH")"

if [ -e "$LINK_PATH" ] || [ -L "$LINK_PATH" ]; then
  if [ "$FORCE" = true ]; then
    rm -rf "$LINK_PATH"
    echo "[link-path] removed existing path: $LINK_PATH"
  elif [ "$BACKUP" = true ]; then
    TS="$(date +%Y%m%d-%H%M%S)"
    BACKUP_PATH="${LINK_PATH}.bak.${TS}"
    mv "$LINK_PATH" "$BACKUP_PATH"
    echo "[link-path] backup created: $BACKUP_PATH"
  else
    echo "[link-path] error: link path already exists: $LINK_PATH" >&2
    echo "[link-path] tip: use --force or remove it manually." >&2
    exit 1
  fi
fi

ln -s "$TARGET_ABS" "$LINK_PATH"
echo "[link-path] created symlink:"
echo "  $LINK_PATH -> $TARGET_ABS"
ls -l "$LINK_PATH"
