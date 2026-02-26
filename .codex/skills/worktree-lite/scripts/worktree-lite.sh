#!/usr/bin/env bash
set -euo pipefail

die() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage:
  worktree-lite.sh init [--base <branch>] [--root <dir>] [--id <worktree-id>]
  worktree-lite.sh review [--base <branch>]
  worktree-lite.sh propose-message [--base <branch>]
  worktree-lite.sh merge --target <branch> [--message "动作：修改内容"] [--source <branch>]

Commands:
  init             Create a new worktree and branch.
  review           Print review summary for changes against base branch.
  propose-message  Generate commit title candidates in "动作：修改内容" format.
  merge            Squash merge source branch into target and commit.
EOF
}

repo_root() {
  git rev-parse --show-toplevel 2>/dev/null || die "not inside a git repository"
}

repo_common_root() {
  local common
  common="$(git rev-parse --git-common-dir 2>/dev/null)" || die "not inside a git repository"
  if [[ "$common" != /* ]]; then
    common="$(cd "$common" && pwd -P)"
  fi
  (cd "$common/.." && pwd -P)
}

current_branch() {
  local root="$1"
  local branch
  branch="$(git -C "$root" rev-parse --abbrev-ref HEAD)"
  [[ "$branch" != "HEAD" ]] || die "detached HEAD is not supported"
  echo "$branch"
}

ensure_branch_exists() {
  local root="$1"
  local branch="$2"
  git -C "$root" rev-parse --verify --quiet "$branch" >/dev/null || die "branch not found: $branch"
}

append_unique_line() {
  local file="$1"
  local line="$2"
  mkdir -p "$(dirname "$file")"
  touch "$file"
  grep -Fx -- "$line" "$file" >/dev/null 2>&1 || echo "$line" >>"$file"
}

resolve_container_path() {
  local common_root="$1"
  local root_arg="$2"
  if [[ "$root_arg" == /* ]]; then
    echo "$root_arg"
  else
    echo "$common_root/$root_arg"
  fi
}

allocate_worktree_id() {
  local container="$1"
  local prefix
  prefix="$(date +%y%m%d)"
  local _ i candidate
  for i in {1..64}; do
    _="$(od -An -N2 -tx1 /dev/urandom | tr -d ' \n')"
    candidate="${prefix}-${_}"
    if [[ ! -e "$container/$candidate" ]]; then
      echo "$candidate"
      return 0
    fi
  done
  die "unable to allocate worktree id under $container"
}

write_meta() {
  local worktree_root="$1"
  local worktree_id="$2"
  local worktree_branch="$3"
  local base_branch="$4"
  cat >"$worktree_root/.worktree-lite-meta" <<EOF
WORKTREE_ID=$worktree_id
WORKTREE_BRANCH=$worktree_branch
BASE_BRANCH=$base_branch
CREATED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
EOF
  local wt_exclude
  wt_exclude="$(git -C "$worktree_root" rev-parse --git-path info/exclude)"
  append_unique_line "$wt_exclude" "/.worktree-lite-meta"
}

meta_value() {
  local worktree_root="$1"
  local key="$2"
  local meta_file="$worktree_root/.worktree-lite-meta"
  [[ -f "$meta_file" ]] || return 1
  local line
  line="$(grep -E "^${key}=" "$meta_file" | head -n 1 || true)"
  [[ -n "$line" ]] || return 1
  echo "${line#*=}"
}

resolve_base_branch() {
  local worktree_root="$1"
  local explicit="${2:-}"
  local common_root="$3"

  if [[ -n "$explicit" ]]; then
    echo "$explicit"
    return 0
  fi

  local meta_base
  meta_base="$(meta_value "$worktree_root" "BASE_BRANCH" || true)"
  if [[ -n "$meta_base" ]]; then
    echo "$meta_base"
    return 0
  fi

  if git -C "$common_root" show-ref --verify --quiet refs/heads/main; then
    echo "main"
    return 0
  fi
  if git -C "$common_root" show-ref --verify --quiet refs/heads/master; then
    echo "master"
    return 0
  fi

  current_branch "$common_root"
}

count_non_empty() {
  local data="$1"
  printf '%s\n' "$data" | awk 'NF{c++} END{print c+0}'
}

build_subject() {
  local files="$1"
  local count
  count="$(count_non_empty "$files")"

  if [[ "$count" -eq 0 ]]; then
    echo "同步分支改动"
    return 0
  fi

  local first_file
  first_file="$(printf '%s\n' "$files" | awk 'NF{print; exit}')"
  if [[ "$count" -eq 1 ]]; then
    echo "调整 ${first_file} 相关逻辑"
    return 0
  fi

  local modules m1 m2
  modules="$(printf '%s\n' "$files" | awk -F/ 'NF{print $1}' | awk '!seen[$0]++')"
  m1="$(printf '%s\n' "$modules" | awk 'NF{print; exit}')"
  m2="$(printf '%s\n' "$modules" | awk 'NF{if (++n==2){print; exit}}')"

  if [[ -n "$m1" && -n "$m2" && "$m1" != "$m2" ]]; then
    echo "更新 ${m1} 与 ${m2} 相关改动"
    return 0
  fi
  if [[ -n "$m1" ]]; then
    echo "更新 ${m1} 模块相关改动"
    return 0
  fi
  echo "更新 ${count} 处文件改动"
}

detect_action() {
  local common_root="$1"
  local range="$2"
  local statuses="$3"
  local count="$4"

  if [[ "$count" -eq 0 ]]; then
    echo "合并"
    return 0
  fi

  local patch
  patch="$(git -C "$common_root" diff "$range" || true)"

  if printf '%s\n' "$patch" | grep -Eiq 'fix|bug|error|panic|crash|异常|错误|兼容|回归'; then
    echo "修复"
    return 0
  fi
  if printf '%s\n' "$statuses" | grep -Eq '^A[[:space:]]'; then
    echo "新增"
    return 0
  fi
  if printf '%s\n' "$patch" | grep -Eiq 'optimi|perf|cache|speed|性能|提速'; then
    echo "优化"
    return 0
  fi
  echo "修改"
}

title_parts() {
  local common_root="$1"
  local base_branch="$2"
  local source_branch="$3"
  local range files statuses count action subject

  range="${base_branch}...${source_branch}"
  files="$(git -C "$common_root" diff --name-only "$range" || true)"
  statuses="$(git -C "$common_root" diff --name-status "$range" || true)"
  count="$(count_non_empty "$files")"
  subject="$(build_subject "$files")"
  if [[ "$count" -eq 0 ]]; then
    subject="同步 ${source_branch} 到 ${base_branch}"
  fi
  action="$(detect_action "$common_root" "$range" "$statuses" "$count")"
  printf '%s\t%s\t%s\n' "$action" "$subject" "$count"
}

cmd_init() {
  local base_branch=""
  local root_arg=".worktree-lite"
  local worktree_id=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --base)
        [[ $# -ge 2 ]] || die "--base requires a value"
        base_branch="$2"
        shift 2
        ;;
      --root)
        [[ $# -ge 2 ]] || die "--root requires a value"
        root_arg="$2"
        shift 2
        ;;
      --id)
        [[ $# -ge 2 ]] || die "--id requires a value"
        worktree_id="$2"
        shift 2
        ;;
      *)
        die "unknown init option: $1"
        ;;
    esac
  done

  local common_root container branch worktree_root base_exclude
  common_root="$(repo_common_root)"
  if [[ -z "$base_branch" ]]; then
    base_branch="$(current_branch "$common_root")"
  fi
  ensure_branch_exists "$common_root" "$base_branch"

  container="$(resolve_container_path "$common_root" "$root_arg")"
  mkdir -p "$container"
  append_unique_line "$common_root/.git/info/exclude" "/.worktree-lite/"

  if [[ -z "$worktree_id" ]]; then
    worktree_id="$(allocate_worktree_id "$container")"
  fi
  worktree_root="$container/$worktree_id"
  [[ ! -e "$worktree_root" ]] || die "worktree path already exists: $worktree_root"

  branch="worktree-lite/$worktree_id"
  if git -C "$common_root" show-ref --verify --quiet "refs/heads/$branch"; then
    die "branch already exists: $branch"
  fi

  git -C "$common_root" worktree add -b "$branch" "$worktree_root" "$base_branch"
  write_meta "$worktree_root" "$worktree_id" "$branch" "$base_branch"

  base_exclude="$(git -C "$common_root" rev-parse --git-path info/exclude)"
  append_unique_line "$base_exclude" "/.worktree-lite/"

  echo "WORKTREE_ID=$worktree_id"
  echo "WORKTREE_ROOT=$worktree_root"
  echo "WORKTREE_BRANCH=$branch"
  echo "BASE_BRANCH=$base_branch"
  echo "NEXT=cd \"$worktree_root\""
}

cmd_review() {
  local base_branch=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --base)
        [[ $# -ge 2 ]] || die "--base requires a value"
        base_branch="$2"
        shift 2
        ;;
      *)
        die "unknown review option: $1"
        ;;
    esac
  done

  local worktree_root common_root source_branch range
  worktree_root="$(repo_root)"
  common_root="$(repo_common_root)"
  source_branch="$(current_branch "$worktree_root")"
  base_branch="$(resolve_base_branch "$worktree_root" "$base_branch" "$common_root")"

  ensure_branch_exists "$common_root" "$base_branch"
  ensure_branch_exists "$common_root" "$source_branch"

  range="${base_branch}...${source_branch}"
  echo "SOURCE_BRANCH=$source_branch"
  echo "BASE_BRANCH=$base_branch"
  echo "RANGE=$range"
  echo
  echo "== Changed Files =="
  git -C "$common_root" diff --name-status "$range" || true
  echo
  echo "== Diff Stat =="
  git -C "$common_root" diff --stat "$range" || true
  echo
  echo "== Commits =="
  git -C "$common_root" log --oneline --no-decorate "$range" || true
  echo
  echo "== Working Tree Status =="
  git -C "$worktree_root" status --short
}

cmd_propose_message() {
  local base_branch=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --base)
        [[ $# -ge 2 ]] || die "--base requires a value"
        base_branch="$2"
        shift 2
        ;;
      *)
        die "unknown propose-message option: $1"
        ;;
    esac
  done

  local worktree_root common_root source_branch action subject count rec alt1 alt2 reason
  worktree_root="$(repo_root)"
  common_root="$(repo_common_root)"
  source_branch="$(current_branch "$worktree_root")"
  base_branch="$(resolve_base_branch "$worktree_root" "$base_branch" "$common_root")"

  ensure_branch_exists "$common_root" "$base_branch"
  ensure_branch_exists "$common_root" "$source_branch"

  IFS=$'\t' read -r action subject count <<<"$(title_parts "$common_root" "$base_branch" "$source_branch")"
  rec="${action}：${subject}"
  alt1="${action}：处理 ${count} 处文件改动"
  if [[ "$action" == "修改" ]]; then
    alt2="优化：${subject}"
  else
    alt2="修改：${subject}"
  fi
  reason="根据 ${count} 个文件改动和 diff 关键词判断，动作词使用「${action}」。"

  echo "推荐：$rec"
  echo "备选1：$alt1"
  echo "备选2：$alt2"
  echo "理由：$reason"
}

cmd_merge() {
  local target_branch=""
  local source_branch=""
  local message=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --target)
        [[ $# -ge 2 ]] || die "--target requires a value"
        target_branch="$2"
        shift 2
        ;;
      --source)
        [[ $# -ge 2 ]] || die "--source requires a value"
        source_branch="$2"
        shift 2
        ;;
      --message)
        [[ $# -ge 2 ]] || die "--message requires a value"
        message="$2"
        shift 2
        ;;
      *)
        die "unknown merge option: $1"
        ;;
    esac
  done

  [[ -n "$target_branch" ]] || die "--target is required"

  local worktree_root common_root current_main_branch action subject count
  worktree_root="$(repo_root)"
  common_root="$(repo_common_root)"
  if [[ -z "$source_branch" ]]; then
    source_branch="$(current_branch "$worktree_root")"
  fi

  ensure_branch_exists "$common_root" "$target_branch"
  ensure_branch_exists "$common_root" "$source_branch"
  [[ "$target_branch" != "$source_branch" ]] || die "target and source branch must be different"

  if [[ -n "$(git -C "$worktree_root" status --porcelain)" ]]; then
    die "current worktree is dirty; commit or clean before merge"
  fi
  if [[ -n "$(git -C "$common_root" status --porcelain)" ]]; then
    die "main worktree is dirty; clean target branch worktree before merge"
  fi

  current_main_branch="$(current_branch "$common_root")"
  if [[ "$current_main_branch" != "$target_branch" ]]; then
    die "main worktree must be on target branch \"$target_branch\" (current: $current_main_branch)"
  fi

  if git -C "$common_root" diff --quiet "$target_branch...$source_branch"; then
    echo "INFO: no differences between $source_branch and $target_branch"
    return 0
  fi

  if [[ -z "$message" ]]; then
    IFS=$'\t' read -r action subject count <<<"$(title_parts "$common_root" "$target_branch" "$source_branch")"
    message="${action}：${subject}"
    echo "AUTO_MESSAGE=$message"
  fi

  if ! git -C "$common_root" merge --squash --no-commit "$source_branch"; then
    die "squash merge failed; resolve conflicts in $common_root and retry"
  fi
  git -C "$common_root" commit -m "$message"

  echo "MERGED_FROM=$source_branch"
  echo "MERGED_INTO=$target_branch"
  echo "COMMIT_MESSAGE=$message"
}

main() {
  [[ $# -gt 0 ]] || {
    usage
    exit 1
  }

  local cmd="$1"
  shift
  case "$cmd" in
    help|-h|--help)
      usage
      ;;
    init)
      cmd_init "$@"
      ;;
    review)
      cmd_review "$@"
      ;;
    propose-message)
      cmd_propose_message "$@"
      ;;
    merge)
      cmd_merge "$@"
      ;;
    *)
      die "unknown command: $cmd"
      ;;
  esac
}

main "$@"
