#!/usr/bin/env bash
set -euo pipefail

PF_OUTPUT_PRINTED="0"

pf_now_utc() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

pf_die() {
  echo "ERROR: $*" >&2
  exit 1
}

pf_warn() {
  echo "WARN: $*" >&2
}

pf_script_dir() {
  cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P
}

pf_root_dir() {
  cd -- "$(pf_script_dir)/.." && pwd -P
}

pf_entry_cmd() {
  local entry_path="$1"
  printf 'bash "%s"' "$entry_path"
}

pf_print_kv() {
  printf '%s=%s\n' "$1" "$2"
}

pf_print_output_block() {
  local worktree_id="$1"
  local pwd_now="$2"
  local status="$3"
  local next="$4"

  PF_OUTPUT_PRINTED="1"
  pf_print_kv WORKTREE_ID "$worktree_id"
  pf_print_kv PWD "$pwd_now"
  pf_print_kv STATUS "$status"
  pf_print_kv NEXT "$next"
}

pf_toml_get_raw() {
  local file="$1"
  local section="$2"
  local key="$3"

  [[ -f "$file" ]] || return 1

  awk -v section="$section" -v key="$key" '
    BEGIN { in_section=0; found=0 }
    /^[[:space:]]*#/ { next }
    /^[[:space:]]*$/ { next }
    /^[[:space:]]*\[/ {
      s=$0
      sub(/^[[:space:]]*\[/, "", s)
      sub(/\][[:space:]]*$/, "", s)
      in_section = (s == section)
      next
    }
    in_section {
      line=$0
      sub(/[[:space:]]*#.*$/, "", line)
      if (match(line, /^[[:space:]]*([A-Za-z0-9_.-]+)[[:space:]]*=[[:space:]]*(.*)$/, m)) {
        if (m[1] == key) {
          v=m[2]
          sub(/^[[:space:]]+/, "", v)
          sub(/[[:space:]]+$/, "", v)
          print v
          found=1
          exit
        }
      }
    }
    END { exit (found ? 0 : 1) }
  ' "$file"
}

pf_toml_get_str() {
  local file="$1"
  local section="$2"
  local key="$3"
  local default_val="$4"

  local raw
  if ! raw="$(pf_toml_get_raw "$file" "$section" "$key" 2>/dev/null)"; then
    printf '%s' "$default_val"
    return 0
  fi

  if [[ "${raw:0:1}" == '"' && "${raw: -1}" == '"' ]]; then
    raw="${raw:1:${#raw}-2}"
  elif [[ "${raw:0:1}" == "'" && "${raw: -1}" == "'" ]]; then
    raw="${raw:1:${#raw}-2}"
  fi

  if [[ -z "$raw" ]]; then
    printf '%s' "$default_val"
  else
    printf '%s' "$raw"
  fi
}

pf_toml_get_bool() {
  local file="$1"
  local section="$2"
  local key="$3"
  local default_val="$4"

  local raw
  if ! raw="$(pf_toml_get_raw "$file" "$section" "$key" 2>/dev/null)"; then
    printf '%s' "$default_val"
    return 0
  fi

  raw="$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')"
  case "$raw" in
    true|false)
      printf '%s' "$raw"
      ;;
    *)
      printf '%s' "$default_val"
      ;;
  esac
}

pf_git_toplevel() {
  git rev-parse --show-toplevel 2>/dev/null
}

pf_git_path_abs() {
  local repo_root="$1"
  local git_path="$2"
  local p

  p="$(git -C "$repo_root" rev-parse --git-path "$git_path")"
  if [[ "$p" == /* ]]; then
    printf '%s' "$p"
  else
    printf '%s' "$repo_root/$p"
  fi
}

pf_agent_id() {
  if [[ -n "${PARAFORK_AGENT_ID:-}" ]]; then
    printf '%s' "$PARAFORK_AGENT_ID"
    return 0
  fi
  if [[ -n "${CODEX_THREAD_ID:-}" ]]; then
    printf 'codex:%s' "$CODEX_THREAD_ID"
    return 0
  fi
  local user host
  user="${USER:-unknown}"
  host="$(hostname 2>/dev/null || printf 'unknown-host')"
  printf '%s@%s' "$user" "$host"
}

pf_symbol_find_upwards() {
  local start="$1"
  local cur="$start"
  while true; do
    if [[ -f "$cur/.worktree-symbol" ]]; then
      printf '%s' "$cur/.worktree-symbol"
      return 0
    fi
    if [[ "$cur" == "/" ]]; then
      return 1
    fi
    cur="$(cd -- "$cur/.." && pwd -P)"
  done
}

pf_symbol_get() {
  local symbol_path="$1"
  local wanted_key="$2"
  [[ -f "$symbol_path" ]] || return 1

  local line key value
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -n "$line" ]] || continue
    [[ "$line" =~ ^# ]] && continue
    [[ "$line" == *"="* ]] || continue
    key="${line%%=*}"
    value="${line#*=}"
    if [[ "$key" == "$wanted_key" ]]; then
      printf '%s' "$value"
      return 0
    fi
  done <"$symbol_path"

  return 1
}

pf_symbol_set() {
  local symbol_path="$1"
  local key="$2"
  local value="$3"
  [[ -f "$symbol_path" ]] || return 1

  local tmp found line k
  tmp="$(mktemp)"
  found="false"

  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == *"="* ]]; then
      k="${line%%=*}"
      if [[ "$k" == "$key" ]]; then
        printf '%s=%s\n' "$key" "$value" >>"$tmp"
        found="true"
        continue
      fi
    fi
    printf '%s\n' "$line" >>"$tmp"
  done <"$symbol_path"

  if [[ "$found" != "true" ]]; then
    printf '%s=%s\n' "$key" "$value" >>"$tmp"
  fi

  cat "$tmp" >"$symbol_path"
  rm -f "$tmp"
}

pf_write_worktree_lock() {
  local symbol_path="$1"
  local agent_id lock_at
  agent_id="$(pf_agent_id)"
  lock_at="$(pf_now_utc)"
  pf_symbol_set "$symbol_path" "WORKTREE_LOCK" "1"
  pf_symbol_set "$symbol_path" "WORKTREE_LOCK_OWNER" "$agent_id"
  pf_symbol_set "$symbol_path" "WORKTREE_LOCK_AT" "$lock_at"
}

pf_invoke_logged() {
  local worktree_root="$1"
  local script_name="$2"
  local argv_line="$3"
  local fn="$4"

  local log_dir="$worktree_root/paradoc"
  local log_file="$log_dir/Log.txt"
  mkdir -p "$log_dir"
  touch "$log_file"

  {
    echo "===== $(pf_now_utc) $script_name ====="
    echo "cmd: $argv_line"
    echo "pwd: $(pwd -P)"
  } >>"$log_file"

  set +e
  "$fn" 2>&1 | tee -a "$log_file"
  local code="${PIPESTATUS[0]}"
  set -e

  { echo "exit: $code"; echo; } >>"$log_file"
  return "$code"
}

pf_hex4() {
  tr -dc 'A-F0-9' </dev/urandom | head -c 4
}

pf_expand_worktree_rule() {
  local rule="$1"
  local yymmdd hex4
  yymmdd="$(date -u +%y%m%d)"
  hex4="$(pf_hex4)"
  printf '%s' "${rule//\{YYMMDD\}/$yymmdd}" | sed "s/{HEX4}/$hex4/g"
}

pf_append_unique_line() {
  local file="$1"
  local line="$2"
  touch "$file"
  if ! grep -Fqx -- "$line" "$file" 2>/dev/null; then
    printf '%s\n' "$line" >>"$file"
  fi
}

pf_in_conflict_state() {
  local repo_root="$1"
  local git_dir
  git_dir="$(pf_git_path_abs "$repo_root" ".")"

  if [[ -f "$git_dir/MERGE_HEAD" || -f "$git_dir/CHERRY_PICK_HEAD" || -d "$git_dir/rebase-apply" || -d "$git_dir/rebase-merge" ]]; then
    return 0
  fi
  return 1
}

