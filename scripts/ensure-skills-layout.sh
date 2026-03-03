#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/ensure-skills-layout.sh [--strict|--fix]

  --strict  Validate current layout and fail if invalid.
  --fix     Repair safe cases automatically.

Expected layout:
  .codex/skills/      real directory
  .claude/skills      symlink -> ../.codex/skills
EOF
}

MODE="--strict"
if [ "${1:-}" = "--fix" ] || [ "${1:-}" = "--strict" ]; then
  MODE="$1"
elif [ $# -gt 0 ]; then
  usage
  exit 2
fi

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$REPO_ROOT" ]; then
  echo "[skills-layout] error: must run inside a git repository." >&2
  exit 1
fi

CODEX_SKILLS="$REPO_ROOT/.codex/skills"
CLAUDE_DIR="$REPO_ROOT/.claude"
CLAUDE_SKILLS="$CLAUDE_DIR/skills"
EXPECTED_LINK="../.codex/skills"

fail() {
  echo "[skills-layout] error: $1" >&2
  exit 1
}

ensure_canonical_exists() {
  [ -d "$CODEX_SKILLS" ] || fail "missing canonical directory: $CODEX_SKILLS"
}

validate_layout() {
  ensure_canonical_exists

  [ -L "$CLAUDE_SKILLS" ] || fail "$CLAUDE_SKILLS must be a symlink."

  local actual_link
  actual_link="$(readlink "$CLAUDE_SKILLS")"
  [ "$actual_link" = "$EXPECTED_LINK" ] || fail "$CLAUDE_SKILLS points to '$actual_link' (expected '$EXPECTED_LINK')."

  [ -d "$CLAUDE_SKILLS" ] || fail "$CLAUDE_SKILLS is a broken symlink."
  echo "[skills-layout] ok: .claude/skills -> $EXPECTED_LINK"
}

fix_layout() {
  ensure_canonical_exists
  mkdir -p "$CLAUDE_DIR"

  if [ -L "$CLAUDE_SKILLS" ]; then
    local actual_link
    actual_link="$(readlink "$CLAUDE_SKILLS")"
    if [ "$actual_link" != "$EXPECTED_LINK" ]; then
      rm -f "$CLAUDE_SKILLS"
      ln -s "$EXPECTED_LINK" "$CLAUDE_SKILLS"
      echo "[skills-layout] fixed: rewired symlink to $EXPECTED_LINK"
    fi
    validate_layout
    return 0
  fi

  if [ ! -e "$CLAUDE_SKILLS" ]; then
    ln -s "$EXPECTED_LINK" "$CLAUDE_SKILLS"
    echo "[skills-layout] fixed: created symlink .claude/skills -> $EXPECTED_LINK"
    validate_layout
    return 0
  fi

  if [ -d "$CLAUDE_SKILLS" ]; then
    if find "$CLAUDE_SKILLS" -mindepth 1 -print -quit | grep -q .; then
      fail "$CLAUDE_SKILLS is a non-empty directory. Move or back it up manually before fixing."
    fi
    rmdir "$CLAUDE_SKILLS"
    ln -s "$EXPECTED_LINK" "$CLAUDE_SKILLS"
    echo "[skills-layout] fixed: replaced empty directory with symlink"
    validate_layout
    return 0
  fi

  fail "$CLAUDE_SKILLS exists but is not a directory/symlink."
}

case "$MODE" in
  --strict) validate_layout ;;
  --fix) fix_layout ;;
esac
