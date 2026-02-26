#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/_lib.sh"

INVOCATION_PWD="$(pwd -P)"
ENTRY_PATH="$SCRIPT_DIR/parafork.sh"
ENTRY_CMD="$(pf_entry_cmd "$ENTRY_PATH")"
CONFIG_PATH="$(pf_root_dir)/settings/config.toml"
LAST_INIT_ROOT=""
WT_ID=""
WT_ROOT=""
WT_BASE=""
WT_SYMBOL=""

fallback() {
  local code="$1"
  if [[ "$code" -ne 0 && "${PF_OUTPUT_PRINTED:-0}" != "1" ]]; then
    pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "FAIL" "$ENTRY_CMD help --debug"
  fi
}
trap 'fallback $?' EXIT

cfg_str() { pf_toml_get_str "$CONFIG_PATH" "$1" "$2" "$3"; }
cfg_bool() { pf_toml_get_bool "$CONFIG_PATH" "$1" "$2" "$3"; }
base_branch() { cfg_str base branch autodetect; }
workdir_root() { cfg_str workdir root .parafork; }
workdir_rule() { cfg_str workdir rule '{YYMMDD}-{HEX4}'; }
autoplan() { cfg_bool custom autoplan false; }
autoformat() { cfg_bool custom autoformat true; }
squash_mode() { cfg_bool control squash true; }

usage_main() {
  cat <<TXT
Parafork proposed (minimal)
Usage: $ENTRY_CMD [cmd] [args...]
Commands:
  help [debug|--debug]
  init [--new|--reuse] [--yes] [--i-am-maintainer]
  do <exec|commit>
  check [status|merge]
  merge [--yes] [--i-am-maintainer]
Default: no args => init --new + do exec
TXT
}
usage_init() { echo "Usage: $ENTRY_CMD init [--new|--reuse] [--yes] [--i-am-maintainer]"; }
usage_check() { echo "Usage: $ENTRY_CMD check [status|merge] [--strict]"; }
usage_do() { echo "Usage: $ENTRY_CMD do <exec|commit>"; }
usage_exec() { echo "Usage: $ENTRY_CMD do exec [--strict]"; }
usage_commit() { echo "Usage: $ENTRY_CMD do commit --message \"<msg>\" [--no-check]"; }
usage_merge() {
  cat <<TXT
Usage: $ENTRY_CMD merge [--yes] [--i-am-maintainer]
CLI gate: --yes --i-am-maintainer
TXT
}

wt_container() { local base="$1"; echo "$base/$(workdir_root)"; }
list_wt_newest() {
  local base="$1" dir
  dir="$(wt_container "$base")"
  [[ -d "$dir" ]] || return 0
  while IFS= read -r d; do [[ -f "$d/.worktree-symbol" ]] && echo "$d"; done < <(ls -1dt "$dir"/* 2>/dev/null || true)
}

resolve_init_base_branch() {
  local base="$1" configured current answer

  configured="$(base_branch)"
  current="$(git -C "$base" rev-parse --abbrev-ref HEAD 2>/dev/null | head -n1 | tr -d '\r\n')"

  [[ -n "$current" ]] || pf_die "cannot detect current branch from base repo"

  if [[ -z "$configured" || "$configured" == "autodetect" ]]; then
    [[ "$current" != "HEAD" ]] || pf_die "autodetect requires a branch checkout (detached HEAD is not supported)"
    printf '%s' "$current"
    return 0
  fi

  if [[ "$configured" != "$current" ]]; then
    if [[ "$current" == "HEAD" ]]; then
      pf_warn "config base.branch=$configured differs from detached HEAD; keep configured branch"
      printf '%s' "$configured"
      return 0
    fi

    if [[ -t 0 ]]; then
      printf 'WARN: config base.branch=%s differs from current branch=%s\n' "$configured" "$current" >&2
      read -r -p "Use current branch '$current' for init --new? [Y/n]: " answer
      case "$answer" in
        ''|y|Y|yes|YES) printf '%s' "$current"; return 0 ;;
        *) printf '%s' "$configured"; return 0 ;;
      esac
    fi

    pf_warn "config base.branch=$configured differs from current branch=$current; non-interactive mode defaults to current branch"
    printf '%s' "$current"
    return 0
  fi

  printf '%s' "$configured"
}

guard_conflict() {
  local repo="$1" wid="$2" pwd_now="$3"
  if pf_in_conflict_state "$repo"; then
    echo "REFUSED: repository in conflict state (merge/rebase/cherry-pick)"
    pf_print_output_block "$wid" "$pwd_now" "FAIL" "diagnose conflicts and request human approval before continuing"
    return 1
  fi
  return 0
}

guard_worktree() {
  local pwd_now symbol pf_wt used lock owner aid safe takeover
  pwd_now="$(pwd -P)"
  symbol="$(pf_symbol_find_upwards "$pwd_now" 2>/dev/null || true)"
  WT_ID=""; WT_ROOT=""; WT_BASE=""; WT_SYMBOL=""
  if [[ -z "$symbol" ]]; then
    if pf_git_toplevel >/dev/null 2>&1; then
      pf_print_output_block "UNKNOWN" "$pwd_now" "FAIL" "$ENTRY_CMD help --debug"
    else
      pf_print_output_block "UNKNOWN" "$pwd_now" "FAIL" "cd <BASE_ROOT> && $ENTRY_CMD init --new"
    fi
    return 1
  fi
  pf_wt="$(pf_symbol_get "$symbol" PARAFORK_WORKTREE || true)"
  [[ "$pf_wt" == "1" ]] || { pf_print_output_block "UNKNOWN" "$pwd_now" "FAIL" "$ENTRY_CMD help --debug"; return 1; }
  WT_ID="$(pf_symbol_get "$symbol" WORKTREE_ID || true)"; [[ -n "$WT_ID" ]] || WT_ID="UNKNOWN"
  WT_ROOT="$(pf_symbol_get "$symbol" WORKTREE_ROOT || true)"
  WT_BASE="$(pf_symbol_get "$symbol" BASE_ROOT || true)"
  WT_SYMBOL="$symbol"
  [[ -n "$WT_ROOT" ]] || { pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "$ENTRY_CMD help --debug"; return 1; }
  used="$(pf_symbol_get "$symbol" WORKTREE_USED || true)"
  [[ "$used" == "1" ]] || { echo "REFUSED: worktree not entered (WORKTREE_USED!=1)"; pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "$ENTRY_CMD init --reuse --yes --i-am-maintainer"; return 1; }
  lock="$(pf_symbol_get "$symbol" WORKTREE_LOCK || true)"
  owner="$(pf_symbol_get "$symbol" WORKTREE_LOCK_OWNER || true)"
  aid="$(pf_agent_id)"
  if [[ "$lock" != "1" || -z "$owner" ]]; then pf_write_worktree_lock "$symbol"; owner="$aid"; fi
  if [[ "$owner" != "$aid" ]]; then
    safe="$ENTRY_CMD init --new"
    takeover="cd \"$WT_ROOT\" && $ENTRY_CMD init --reuse --yes --i-am-maintainer"
    echo "REFUSED: worktree locked by another agent"
    pf_print_kv LOCK_OWNER "$owner"
    pf_print_kv AGENT_ID "$aid"
    pf_print_kv SAFE_NEXT "$safe"
    pf_print_kv TAKEOVER_NEXT "$takeover"
    pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "$safe"
    return 1
  fi
  return 0
}

check_files() {
  local phase="$1" strict="$2"
  local root plan execf mergef logf ap af
  root="$WT_ROOT"; [[ -n "$root" ]] || root="$(pwd -P)"
  ap="$(autoplan)"; af="$(autoformat)"
  [[ "$strict" == "true" ]] && ap="true" af="true"
  plan="$root/paradoc/Plan.md"; execf="$root/paradoc/Exec.md"; mergef="$root/paradoc/Merge.md"; logf="$root/paradoc/Log.txt"
  local -a errs=()
  [[ -f "$execf" ]] || errs+=("missing file: $execf")
  [[ -f "$mergef" ]] || errs+=("missing file: $mergef")
  [[ -f "$logf" ]] || errs+=("missing file: $logf")
  [[ "$ap" == "true" && ! -f "$plan" ]] && errs+=("missing file: $plan")
  if [[ "$ap" == "true" && "$af" == "true" && -f "$plan" ]]; then
    grep -Fq '## Milestones' "$plan" || errs+=("Plan.md missing heading: ## Milestones")
    grep -Fq '## Tasks' "$plan" || errs+=("Plan.md missing heading: ## Tasks")
    grep -Eq '^- \[.\] ' "$plan" || errs+=("Plan.md has no checkboxes")
  fi
  if [[ "$af" == "true" && -f "$mergef" ]]; then
    grep -Ei 'Acceptance|Repro' "$mergef" >/dev/null || errs+=("Merge.md missing Acceptance/Repro section keywords")
  fi
  if [[ "$phase" == "merge" || "$strict" == "true" ]]; then
    for f in "$execf" "$mergef"; do
      [[ -f "$f" ]] && grep -Eq 'PARAFORK_TBD|TODO_TBD' "$f" && errs+=("placeholder remains: $f") || true
    done
    [[ "$ap" == "true" && -f "$plan" ]] && grep -Eq 'PARAFORK_TBD|TODO_TBD' "$plan" && errs+=("placeholder remains: $plan") || true
  fi
  if [[ "$phase" == "merge" ]]; then
    git ls-files -- 'paradoc/' | grep -q . && errs+=("git pollution: tracked files under paradoc/") || true
    git ls-files -- '.worktree-symbol' | grep -q . && errs+=("git pollution: .worktree-symbol is tracked") || true
    git diff --cached --name-only -- | grep -E '^(paradoc/|\.worktree-symbol$)' >/dev/null && errs+=("git pollution: staged includes paradoc/ or .worktree-symbol") || true
  fi
  if [[ "${#errs[@]}" -gt 0 ]]; then
    echo "CHECK_RESULT=FAIL"; local e; for e in "${errs[@]}"; do echo "FAIL: $e"; done; return 1
  fi
  echo "CHECK_RESULT=PASS"; return 0
}

print_status() {
  local branch baseb wtb dirty untracked
  branch="$(git rev-parse --abbrev-ref HEAD)"
  baseb="$(pf_symbol_get "$WT_SYMBOL" BASE_BRANCH || true)"
  wtb="$(pf_symbol_get "$WT_SYMBOL" WORKTREE_BRANCH || true)"
  dirty="$(git status --porcelain --untracked-files=no | wc -l | tr -d ' ')"
  untracked="$(git status --porcelain | awk '/^\?\?/ {c++} END {print c+0}')"
  pf_print_kv CURRENT_BRANCH "$branch"
  pf_print_kv BASE_BRANCH "$baseb"
  pf_print_kv WORKTREE_BRANCH "$wtb"
  pf_print_kv TRACKED_DIRTY "$dirty"
  pf_print_kv UNTRACKED_COUNT "$untracked"
}

print_review() {
  local baseb wtb
  baseb="$(pf_symbol_get "$WT_SYMBOL" BASE_BRANCH || true)"
  wtb="$(pf_symbol_get "$WT_SYMBOL" WORKTREE_BRANCH || true)"
  echo "### Review material"
  echo "#### Commits ($baseb..$wtb)"
  git log --oneline "$baseb..$wtb" || true
  echo
  echo "#### Files ($baseb...$wtb)"
  git diff --name-status "$baseb...$wtb" || true
}

cmd_help_debug() {
  local pwd_now symbol base dir safe takeover chosen cid
  pwd_now="$(pwd -P)"
  symbol="$(pf_symbol_find_upwards "$pwd_now" 2>/dev/null || true)"
  if [[ -n "$symbol" ]]; then
    local wid wroot body
    wid="$(pf_symbol_get "$symbol" WORKTREE_ID || true)"; [[ -n "$wid" ]] || wid="UNKNOWN"
    wroot="$(pf_symbol_get "$symbol" WORKTREE_ROOT || true)"
    if [[ -n "$wroot" ]]; then
      body() { pf_print_kv SYMBOL_PATH "$symbol"; pf_print_output_block "$wid" "$INVOCATION_PWD" "PASS" "$ENTRY_CMD do exec"; }
      pf_invoke_logged "$wroot" "parafork-proposed help --debug" "$ENTRY_CMD help --debug" body
      return $?
    fi
    pf_print_kv SYMBOL_PATH "$symbol"; pf_print_output_block "$wid" "$INVOCATION_PWD" "PASS" "$ENTRY_CMD do exec"; return 0
  fi
  base="$(pf_git_toplevel || true)"
  [[ -n "$base" ]] || { pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "FAIL" "$ENTRY_CMD help"; pf_die "not in git repo and no .worktree-symbol found"; }
  dir="$(wt_container "$base")"
  if [[ ! -d "$dir" ]]; then
    pf_print_kv BASE_ROOT "$base"; pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "PASS" "$ENTRY_CMD init --new"; echo; echo "No worktree container found at: $dir"; return 0
  fi
  mapfile -t _roots < <(list_wt_newest "$base")
  if [[ "${#_roots[@]}" -eq 0 ]]; then
    pf_print_kv BASE_ROOT "$base"; pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "PASS" "$ENTRY_CMD init --new"; echo; echo "No worktrees found under: $dir"; return 0
  fi
  echo "Found worktrees (newest first):"
  local d wid
  for d in "${_roots[@]}"; do wid="$(pf_symbol_get "$d/.worktree-symbol" WORKTREE_ID || true)"; [[ -n "$wid" ]] || wid="UNKNOWN"; echo "- $wid  $d"; done
  chosen="${_roots[0]}"; cid="$(pf_symbol_get "$chosen/.worktree-symbol" WORKTREE_ID || true)"; [[ -n "$cid" ]] || cid="UNKNOWN"
  safe="$ENTRY_CMD init --new"; takeover="cd \"$chosen\" && $ENTRY_CMD init --reuse --yes --i-am-maintainer"
  echo; pf_print_kv BASE_ROOT "$base"; pf_print_kv SAFE_NEXT "$safe"; pf_print_kv TAKEOVER_NEXT "$takeover"; pf_print_output_block "$cid" "$INVOCATION_PWD" "PASS" "$safe"
}

cmd_help() {
  local topic="${1:-}"
  case "$topic" in
    "") pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "PASS" "$ENTRY_CMD"; usage_main ;;
    debug|--debug) shift || true; [[ $# -eq 0 ]] || pf_die "unknown arg for help debug: $1"; cmd_help_debug ;;
    *) pf_die "unknown help topic: $topic" ;;
  esac
}

cmd_init_new() {
  local base="$1" branch root rule ap id wr wb body
  [[ -f "$CONFIG_PATH" ]] || pf_die "missing config: $CONFIG_PATH"
  branch="$(resolve_init_base_branch "$base")"; root="$(workdir_root)"; rule="$(workdir_rule)"; ap="$(autoplan)"
  mkdir -p "$base/$root"
  id="$(pf_expand_worktree_rule "$rule")"; wr="$base/$root/$id"; wb="parafork/$id"
  [[ ! -e "$wr" ]] || pf_die "worktree already exists: $wr"
  git -C "$base" worktree add -b "$wb" "$wr" "$branch"
  cat >"$wr/.worktree-symbol" <<SYMBOL
PARAFORK_WORKTREE=1
WORKTREE_ID=$id
BASE_ROOT=$base
WORKTREE_ROOT=$wr
WORKTREE_BRANCH=$wb
BASE_BRANCH=$branch
WORKTREE_USED=1
WORKTREE_LOCK=1
WORKTREE_LOCK_OWNER=$(pf_agent_id)
WORKTREE_LOCK_AT=$(pf_now_utc)
CREATED_AT=$(pf_now_utc)
SYMBOL
  local b_exc w_exc
  b_exc="$(pf_git_path_abs "$base" info/exclude)"; w_exc="$(pf_git_path_abs "$wr" info/exclude)"
  mkdir -p "$(dirname "$b_exc")" "$(dirname "$w_exc")"; touch "$b_exc" "$w_exc"
  pf_append_unique_line "$b_exc" "/$root/"
  pf_append_unique_line "$w_exc" '/.worktree-symbol'
  pf_append_unique_line "$w_exc" '/paradoc/'
  mkdir -p "$wr/paradoc"
  [[ -f "$(pf_root_dir)/assets/Exec.md" ]] && cp "$(pf_root_dir)/assets/Exec.md" "$wr/paradoc/Exec.md"
  [[ -f "$(pf_root_dir)/assets/Merge.md" ]] && cp "$(pf_root_dir)/assets/Merge.md" "$wr/paradoc/Merge.md"
  [[ "$ap" == "true" && -f "$(pf_root_dir)/assets/Plan.md" ]] && cp "$(pf_root_dir)/assets/Plan.md" "$wr/paradoc/Plan.md"
  touch "$wr/paradoc/Log.txt"
  LAST_INIT_ROOT="$wr"
  body() { echo "MODE=new"; pf_print_kv WORKTREE_ROOT "$wr"; pf_print_kv WORKTREE_BRANCH "$wb"; pf_print_output_block "$id" "$INVOCATION_PWD" "PASS" "cd \"$wr\" && $ENTRY_CMD do exec"; }
  pf_invoke_logged "$wr" "parafork-proposed init --new" "$ENTRY_CMD init --new" body
}

cmd_init() {
  local mode="auto" yes="false" iam="false" pwd_now symbol in_wt="false" swid="" swroot="" sbase=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --new) [[ "$mode" == auto || "$mode" == new ]] || pf_die "--new and --reuse are mutually exclusive"; mode="new"; shift ;;
      --reuse) [[ "$mode" == auto || "$mode" == reuse ]] || pf_die "--new and --reuse are mutually exclusive"; mode="reuse"; shift ;;
      --yes) yes="true"; shift ;;
      --i-am-maintainer) iam="true"; shift ;;
      -h|--help) usage_init; return 0 ;;
      *) pf_die "unknown arg: $1" ;;
    esac
  done
  pwd_now="$(pwd -P)"; symbol="$(pf_symbol_find_upwards "$pwd_now" 2>/dev/null || true)"
  if [[ -n "$symbol" && "$(pf_symbol_get "$symbol" PARAFORK_WORKTREE || true)" == "1" ]]; then
    in_wt="true"; swid="$(pf_symbol_get "$symbol" WORKTREE_ID || true)"; swroot="$(pf_symbol_get "$symbol" WORKTREE_ROOT || true)"; sbase="$(pf_symbol_get "$symbol" BASE_ROOT || true)"
  elif [[ -n "$symbol" ]]; then
    pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "FAIL" "$ENTRY_CMD help --debug"; pf_die "found .worktree-symbol but not parafork worktree: $symbol"
  fi
  if [[ "$mode" == auto && "$in_wt" == true ]]; then
    [[ -n "$swid" ]] || swid="UNKNOWN"
    echo "REFUSED: init called from inside a worktree without --reuse or --new"
    pf_print_kv SYMBOL_PATH "$symbol"; pf_print_kv WORKTREE_ID "$swid"; pf_print_kv WORKTREE_ROOT "$swroot"; pf_print_kv BASE_ROOT "$sbase"
    echo; echo "Choose one:"; echo "- Reuse current worktree: $ENTRY_CMD init --reuse --yes --i-am-maintainer"; echo "- Create new worktree:    $ENTRY_CMD init --new"
    pf_print_output_block "$swid" "$INVOCATION_PWD" "FAIL" "$ENTRY_CMD init --new"
    return 1
  fi
  [[ "$mode" != reuse || "$in_wt" == true ]] || { pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "FAIL" "$ENTRY_CMD help --debug"; pf_die "--reuse requires being inside existing parafork worktree"; }
  [[ "$mode" != auto ]] || mode="new"
  if [[ "$mode" == reuse ]]; then
    if [[ "$yes" != true || "$iam" != true ]]; then
      echo "REFUSED: missing CLI gate"
      pf_print_output_block "${swid:-UNKNOWN}" "$INVOCATION_PWD" "FAIL" "$ENTRY_CMD init --reuse --yes --i-am-maintainer"
      return 1
    fi
    [[ -n "$swroot" ]] || pf_die "missing WORKTREE_ROOT in .worktree-symbol"
    pf_symbol_set "$symbol" WORKTREE_USED 1; pf_write_worktree_lock "$symbol"
    local body
    body() { echo "MODE=reuse"; pf_print_kv WORKTREE_USED 1; pf_print_output_block "${swid:-UNKNOWN}" "$INVOCATION_PWD" "PASS" "cd \"$swroot\" && $ENTRY_CMD do exec"; }
    pf_invoke_logged "$swroot" "parafork-proposed init --reuse" "$ENTRY_CMD init --reuse --yes --i-am-maintainer" body
    return $?
  fi
  local base
  [[ "$in_wt" == true ]] && base="$sbase" || base="$(pf_git_toplevel || true)"
  [[ -n "$base" ]] || { pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "FAIL" "cd <BASE_ROOT> && $ENTRY_CMD init --new"; pf_die "not in a git repo"; }
  cmd_init_new "$base"
}

cmd_check_status() {
  [[ $# -eq 0 ]] || pf_die "unknown arg: $1"
  guard_worktree || return 1
  cd "$WT_ROOT"; guard_conflict "$WT_ROOT" "$WT_ID" "$(pwd -P)" || return 1
  local body
  body() { print_status; pf_print_output_block "$WT_ID" "$(pwd -P)" "PASS" "$ENTRY_CMD do exec"; }
  pf_invoke_logged "$WT_ROOT" "parafork-proposed check status" "$ENTRY_CMD check status" body
}

cmd_check_merge() {
  local strict="$1"; shift || true; [[ $# -eq 0 ]] || pf_die "unknown arg: $1"
  guard_worktree || return 1
  cd "$WT_ROOT"; guard_conflict "$WT_ROOT" "$WT_ID" "$(pwd -P)" || return 1
  local argv="$ENTRY_CMD check merge"; [[ "$strict" == true ]] && argv+=" --strict"
  local body
  body() {
    local pwd_now; pwd_now="$(pwd -P)"
    print_status; print_review
    if ! check_files merge "$strict"; then pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "fix issues then rerun: $ENTRY_CMD check merge"; return 1; fi
    pf_print_output_block "$WT_ID" "$pwd_now" "PASS" "$ENTRY_CMD merge --yes --i-am-maintainer"
  }
  pf_invoke_logged "$WT_ROOT" "parafork-proposed check merge" "$argv" body
}

cmd_check() {
  local strict="false" topic="status" seen="false"; local -a rest=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --strict) strict="true"; shift ;;
      -h|--help) usage_check; return 0 ;;
      *) [[ "$seen" == false ]] && topic="$1" seen="true" || rest+=("$1"); shift ;;
    esac
  done
  [[ "$strict" != true || "$topic" == merge ]] || pf_die "--strict is only valid for check merge"
  case "$topic" in
    status) cmd_check_status "${rest[@]}" ;;
    merge) cmd_check_merge "$strict" "${rest[@]}" ;;
    *) pf_die "unknown topic: $topic" ;;
  esac
}

cmd_do_exec() {
  local strict="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --strict) strict="true"; shift ;;
      -h|--help) usage_exec; return 0 ;;
      *) pf_die "unknown arg: $1" ;;
    esac
  done
  guard_worktree || return 1
  cd "$WT_ROOT"; guard_conflict "$WT_ROOT" "$WT_ID" "$(pwd -P)" || return 1
  local argv="$ENTRY_CMD do exec"; [[ "$strict" == true ]] && argv+=" --strict"
  local body
  body() {
    local pwd_now changes
    pwd_now="$(pwd -P)"
    print_status
    if ! check_files exec "$strict"; then pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "fix issues and rerun: $ENTRY_CMD do exec"; return 1; fi
    changes="$(git status --porcelain | wc -l | tr -d ' ')"
    if [[ "$changes" != 0 ]]; then
      pf_print_output_block "$WT_ID" "$pwd_now" "PASS" "$ENTRY_CMD do commit --message \"<msg>\""
    else
      pf_print_output_block "$WT_ID" "$pwd_now" "PASS" "edit files (rerun: $ENTRY_CMD do exec)"
    fi
  }
  pf_invoke_logged "$WT_ROOT" "parafork-proposed do exec" "$argv" body
}

cmd_do_commit() {
  local message="" no_check="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --message) message="${2:-}"; shift 2 ;;
      --no-check) no_check="true"; shift ;;
      -h|--help) usage_commit; return 0 ;;
      *) pf_die "unknown arg: $1" ;;
    esac
  done
  [[ -n "$message" ]] || pf_die "missing --message"
  guard_worktree || return 1
  cd "$WT_ROOT"; guard_conflict "$WT_ROOT" "$WT_ID" "$(pwd -P)" || return 1
  local body
  body() {
    local pwd_now head
    pwd_now="$(pwd -P)"
    if [[ "$no_check" != true ]] && ! check_files exec false; then
      echo "REFUSED: check failed"
      pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "fix issues then retry: $ENTRY_CMD do commit --message \"...\""
      return 1
    fi
    git add -A -- .
    if git diff --cached --name-only -- | grep -E '^(paradoc/|\.worktree-symbol$)' >/dev/null; then
      echo "REFUSED: git pollution staged"
      pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "unstage pollution and retry: git reset -q && $ENTRY_CMD do commit --message \"...\""
      return 1
    fi
    if git diff --cached --quiet --; then
      echo "REFUSED: nothing staged"
      pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "edit files then retry: $ENTRY_CMD do commit --message \"...\""
      return 1
    fi
    git commit -m "$message"
    head="$(git rev-parse --short HEAD)"
    pf_print_kv COMMIT "$head"
    pf_print_output_block "$WT_ID" "$pwd_now" "PASS" "$ENTRY_CMD do exec"
  }
  pf_invoke_logged "$WT_ROOT" "parafork-proposed do commit" "$ENTRY_CMD do commit --message \"...\"" body
}

cmd_do() {
  local action="${1:-}"
  if [[ -z "$action" || "$action" == -h || "$action" == --help ]]; then usage_do; return 0; fi
  shift || true
  case "$action" in
    exec) cmd_do_exec "$@" ;;
    commit) cmd_do_commit "$@" ;;
    *) pf_die "unknown action: $action" ;;
  esac
}

cmd_merge() {
  local yes="false" iam="false"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --yes) yes="true"; shift ;;
      --i-am-maintainer) iam="true"; shift ;;
      -h|--help) usage_merge; return 0 ;;
      *) pf_die "unknown arg: $1" ;;
    esac
  done
  guard_worktree || return 1
  cd "$WT_ROOT"; guard_conflict "$WT_ROOT" "$WT_ID" "$(pwd -P)" || return 1
  local body
  body() {
    local pwd_now branch wt_branch cur base_dirty base_cur squash message
    pwd_now="$(pwd -P)"
    branch="$(pf_symbol_get "$WT_SYMBOL" BASE_BRANCH || true)"
    wt_branch="$(pf_symbol_get "$WT_SYMBOL" WORKTREE_BRANCH || true)"
    print_status; print_review
    if ! check_files merge false; then echo "REFUSED: check merge failed"; pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "fix issues then rerun: $ENTRY_CMD check merge"; return 1; fi
    if [[ "$yes" != true || "$iam" != true ]]; then echo "REFUSED: missing CLI gate"; pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "rerun with --yes --i-am-maintainer"; return 1; fi
    cur="$(git rev-parse --abbrev-ref HEAD)"
    [[ -n "$wt_branch" && "$cur" != "$wt_branch" ]] && { echo "REFUSED: wrong worktree branch"; pf_print_kv EXPECTED_WORKTREE_BRANCH "$wt_branch"; pf_print_kv CURRENT_BRANCH "$cur"; pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "checkout correct branch and retry"; return 1; }
    base_dirty="$(git -C "$WT_BASE" status --porcelain --untracked-files=no | wc -l | tr -d ' ')"
    [[ "$base_dirty" == 0 ]] || { echo "REFUSED: base repo not clean (tracked)"; pf_print_kv BASE_TRACKED_DIRTY "$base_dirty"; pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "clean base repo tracked changes then retry"; return 1; }
    base_cur="$(git -C "$WT_BASE" rev-parse --abbrev-ref HEAD)"
    [[ "$base_cur" == "$branch" ]] || { echo "REFUSED: base branch mismatch"; pf_print_kv BASE_BRANCH "$branch"; pf_print_kv BASE_CURRENT_BRANCH "$base_cur"; pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "cd \"$WT_BASE\" && git checkout \"$branch\""; return 1; }
    squash="$(squash_mode)"; message="parafork: merge $WT_ID"; pf_print_kv SQUASH "$squash"
    echo "PREVIEW_COMMITS=$branch..$wt_branch"; git -C "$WT_BASE" log --oneline "$branch..$wt_branch" || true
    echo "PREVIEW_FILES=$branch...$wt_branch"; git -C "$WT_BASE" diff --name-status "$branch...$wt_branch" || true
    if [[ "$squash" == true ]]; then
      if ! git -C "$WT_BASE" merge --squash "$wt_branch"; then echo "REFUSED: squash merge stopped (likely conflicts)"; pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "resolve conflicts in \"$WT_BASE\" then commit"; return 1; fi
      git -C "$WT_BASE" commit -m "$message"
    else
      if ! git -C "$WT_BASE" merge --no-ff "$wt_branch" -m "$message"; then echo "REFUSED: merge stopped (likely conflicts)"; pf_print_output_block "$WT_ID" "$pwd_now" "FAIL" "resolve then git -C \"$WT_BASE\" merge --continue"; return 1; fi
    fi
    pf_print_kv MERGED_COMMIT "$(git -C "$WT_BASE" rev-parse --short HEAD)"
    pf_print_output_block "$WT_ID" "$pwd_now" "PASS" "run acceptance steps in paradoc/Merge.md"
  }
  pf_invoke_logged "$WT_ROOT" "parafork-proposed merge" "$ENTRY_CMD merge" body
}

cmd_default() {
  local symbol in_wt="false" base
  symbol="$(pf_symbol_find_upwards "$(pwd -P)" 2>/dev/null || true)"
  if [[ -n "$symbol" && "$(pf_symbol_get "$symbol" PARAFORK_WORKTREE || true)" == 1 ]]; then in_wt="true"; fi
  [[ "$in_wt" == true ]] && base="$(pf_symbol_get "$symbol" BASE_ROOT || true)" || base="$(pf_git_toplevel || true)"
  [[ -n "$base" ]] || { pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "FAIL" "$ENTRY_CMD help"; pf_die "not in a git repo and no .worktree-symbol found"; }
  cd "$base"; cmd_init --new
  [[ -n "$LAST_INIT_ROOT" ]] || pf_die "failed to resolve new worktree root"
  cd "$LAST_INIT_ROOT"; cmd_do exec
}

main() {
  local cmd="${1:-}"
  if [[ -z "$cmd" ]]; then cmd_default; return $?; fi
  [[ "$cmd" != -h && "$cmd" != --help ]] || cmd="help"
  shift || true
  case "$cmd" in
    help) cmd_help "$@" ;;
    init) cmd_init "$@" ;;
    do) cmd_do "$@" ;;
    check) cmd_check "$@" ;;
    merge) cmd_merge "$@" ;;
    *) echo "ERROR: unknown command: $cmd"; pf_print_output_block "UNKNOWN" "$INVOCATION_PWD" "FAIL" "$ENTRY_CMD help"; usage_main; return 1 ;;
  esac
}

main "$@"
