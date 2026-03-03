#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$REPO_ROOT" ]; then
  echo "[setup-hooks] error: must run inside a git repository." >&2
  exit 1
fi

cd "$REPO_ROOT"
[ -d ".githooks" ] || { echo "[setup-hooks] error: .githooks not found." >&2; exit 1; }

for hook in pre-commit post-checkout post-merge; do
  if [ -f ".githooks/$hook" ]; then
    chmod +x ".githooks/$hook"
  fi
done

git config core.hooksPath .githooks
echo "[setup-hooks] core.hooksPath set to: $(git config --get core.hooksPath)"

if [ -x "scripts/ensure-skills-layout.sh" ]; then
  bash "scripts/ensure-skills-layout.sh" --fix
fi
