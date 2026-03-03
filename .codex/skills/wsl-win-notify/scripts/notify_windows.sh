#!/usr/bin/env bash
set -u

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<'EOF'
Usage:
  notify_windows.sh --event <decision|permission|blocked|done> --title <text> --summary <text> [options]

Options:
  --next-action <text>   Next step for the user.
  --stats <text>         Change summary. Defaults to git shortstat or "no diff".
  --channel <mode>       auto|toast|msg (default: auto)
  --app-id <value>       Toast notifier app id (default: Snore.DesktopToasts.0.7.0).
  -h, --help             Show this help.
EOF
}

log_warn() {
  printf '[notify][warn] %s\n' "$*" >&2
}

require_value() {
  local flag="$1"
  local value="${2:-}"
  if [[ -z "$value" || "$value" == --* ]]; then
    log_warn "Missing value for ${flag}."
    usage
    exit 2
  fi
}

EVENT=""
TITLE=""
SUMMARY=""
NEXT_ACTION=""
STATS=""
CHANNEL="auto"
APP_ID="Snore.DesktopToasts.0.7.0"
PROJECT_NAME=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --event)
      require_value "$1" "${2:-}"
      EVENT="$2"
      shift 2
      ;;
    --title)
      require_value "$1" "${2:-}"
      TITLE="$2"
      shift 2
      ;;
    --summary)
      require_value "$1" "${2:-}"
      SUMMARY="$2"
      shift 2
      ;;
    --next-action)
      require_value "$1" "${2:-}"
      NEXT_ACTION="$2"
      shift 2
      ;;
    --stats)
      require_value "$1" "${2:-}"
      STATS="$2"
      shift 2
      ;;
    --channel)
      require_value "$1" "${2:-}"
      CHANNEL="$2"
      shift 2
      ;;
    --app-id)
      require_value "$1" "${2:-}"
      APP_ID="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      log_warn "Unknown option: $1"
      usage
      exit 2
      ;;
  esac
done

if [[ -z "$EVENT" || -z "$TITLE" || -z "$SUMMARY" ]]; then
  log_warn "--event, --title, and --summary are required."
  usage
  exit 2
fi

case "$EVENT" in
  decision|permission|blocked|done) ;;
  *)
    log_warn "Invalid --event value: $EVENT"
    exit 2
    ;;
esac

case "$CHANNEL" in
  auto|toast|msg) ;;
  *)
    log_warn "Invalid --channel value: $CHANNEL"
    exit 2
    ;;
esac

if [[ -z "$NEXT_ACTION" ]]; then
  NEXT_ACTION="Open the Codex output and review the requested action."
fi

trim_spaces() {
  sed -E 's/[[:space:]]+/ /g; s/^ //; s/ $//'
}

derive_project_name() {
  local repo_root=""
  local script_repo_root=""

  if git rev-parse --show-toplevel >/dev/null 2>&1; then
    repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
  fi

  if [[ -z "$repo_root" ]]; then
    script_repo_root="$(cd "${SCRIPT_DIR}/../../../.." 2>/dev/null && pwd -P || true)"
    if [[ -n "$script_repo_root" && "$(basename "$script_repo_root")" != ".codex" ]]; then
      repo_root="$script_repo_root"
    fi
  fi

  if [[ -z "$repo_root" ]]; then
    repo_root="$(pwd -P)"
  fi

  PROJECT_NAME="$(basename "$repo_root")"
  if [[ -z "$PROJECT_NAME" ]]; then
    PROJECT_NAME="unknown-project"
  fi
}

derive_stats() {
  local shortstat=""
  local untracked_count=""

  if [[ -n "$STATS" ]]; then
    return
  fi

  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    shortstat="$(git diff --shortstat HEAD -- . 2>/dev/null | tr '\n' ' ' | trim_spaces)"
    if [[ -z "$shortstat" ]]; then
      shortstat="$(git diff --shortstat -- . 2>/dev/null | tr '\n' ' ' | trim_spaces)"
    fi
    if [[ -z "$shortstat" ]]; then
      untracked_count="$(git ls-files --others --exclude-standard 2>/dev/null | wc -l | tr -d ' ')"
      if [[ "${untracked_count:-0}" -gt 0 ]]; then
        shortstat="${untracked_count} untracked files"
      fi
    fi
  fi

  if [[ -z "$shortstat" ]]; then
    STATS="no diff"
  else
    STATS="$shortstat"
  fi
}

compose_message() {
  local event_upper=""
  event_upper="$(printf '%s' "$EVENT" | tr '[:lower:]' '[:upper:]')"
  printf '[Codex][%s] %s\n%s\nProject: %s\nChanges: %s\nNext: %s' \
    "$event_upper" \
    "$TITLE" \
    "$SUMMARY" \
    "$PROJECT_NAME" \
    "$STATS" \
    "$NEXT_ACTION"
}

send_toast() {
  local ps_script="${SCRIPT_DIR}/notify_windows.ps1"
  local ps_win=""
  local title_b64=""
  local summary_b64=""
  local project_b64=""
  local next_action_b64=""
  local stats_b64=""

  if ! command -v powershell.exe >/dev/null 2>&1; then
    log_warn "powershell.exe not found."
    return 1
  fi

  if [[ ! -f "$ps_script" ]]; then
    log_warn "PowerShell script not found: $ps_script"
    return 1
  fi

  if command -v wslpath >/dev/null 2>&1; then
    ps_win="$(wslpath -w "$ps_script" 2>/dev/null || true)"
  fi
  if [[ -z "$ps_win" ]]; then
    ps_win="$ps_script"
  fi

  title_b64="$(printf '%s' "$TITLE" | base64 | tr -d '\r\n')"
  summary_b64="$(printf '%s' "$SUMMARY" | base64 | tr -d '\r\n')"
  project_b64="$(printf '%s' "$PROJECT_NAME" | base64 | tr -d '\r\n')"
  next_action_b64="$(printf '%s' "$NEXT_ACTION" | base64 | tr -d '\r\n')"
  stats_b64="$(printf '%s' "$STATS" | base64 | tr -d '\r\n')"

  powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$ps_win" \
    -Event "$EVENT" \
    -Title "$TITLE" \
    -Summary "$SUMMARY" \
    -Project "$PROJECT_NAME" \
    -NextAction "$NEXT_ACTION" \
    -Stats "$STATS" \
    -TitleB64 "$title_b64" \
    -SummaryB64 "$summary_b64" \
    -ProjectB64 "$project_b64" \
    -NextActionB64 "$next_action_b64" \
    -StatsB64 "$stats_b64" \
    -AppId "$APP_ID" \
    >/dev/null 2>&1
}

sanitize_for_msg() {
  local text="$1"
  text="${text//$'\r'/ }"
  text="${text//$'\n'/ }"
  text="${text//\"/\'}"
  printf '%s' "$text" | trim_spaces
}

send_msg() {
  local body=""
  local safe_body=""

  if ! command -v cmd.exe >/dev/null 2>&1; then
    log_warn "cmd.exe not found."
    return 1
  fi

  body="$(compose_message)"
  safe_body="$(sanitize_for_msg "$body")"

  cmd.exe /C "msg * /TIME:60 \"$safe_body\"" >/dev/null 2>&1
}

derive_project_name
derive_stats

case "$CHANNEL" in
  auto)
    if send_toast; then
      exit 0
    fi
    log_warn "Toast delivery failed; falling back to msg.exe."
    if send_msg; then
      exit 0
    fi
    log_warn "msg.exe fallback failed; continuing without notification."
    exit 0
    ;;
  toast)
    if ! send_toast; then
      log_warn "Toast delivery failed; continuing."
    fi
    exit 0
    ;;
  msg)
    if ! send_msg; then
      log_warn "msg.exe delivery failed; continuing."
    fi
    exit 0
    ;;
esac
