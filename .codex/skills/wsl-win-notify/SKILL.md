---
name: wsl-win-notify
description: 'Send native Windows notifications from WSL during coding workflows. Use by default when code is being modified and the user should be alerted at checkpoints: before asking for human choices, before permission/escalation prompts, when blocked by errors, and when work is complete. Trigger for Chinese or English intents such as "修改后提醒我", "人工介入时通知", "任务完成通知", "notify me when done", or "send Windows notification".'
---

# WSL Win Notify

## Goal

Keep the user informed at high-signal checkpoints during coding tasks by sending Windows notifications from WSL.
Use this skill only for intervention points and final completion, not for every command.

## Invocation Priority

- Treat this skill as baseline behavior in code-editing tasks whenever the skill is available in the runtime.
- Use AGENTS.md rules as a stronger project override, not as the only trigger source.
- Keep behavior consistent with or without AGENTS.md: notify for `decision`, `permission`, `blocked`, and `done`.

## Event Policy

- Send `decision` before asking the user to choose between implementation options.
- Send `permission` before asking for permission or escalation.
- Send `blocked` when failures block progress and require user action.
- Send `done` immediately before the final completion response.

## Command

Run from repository root (or any git working tree):

```bash
./.codex/skills/wsl-win-notify/scripts/notify_windows.sh \
  --event done \
  --title "Feature update complete" \
  --summary "All requested code changes are implemented." \
  --next-action "Open the diff and review the final answer."
```

### Options

- `--event decision|permission|blocked|done` (required)
- `--title <text>` (required)
- `--summary <text>` (required)
- `--next-action <text>` (optional)
- `--stats <text>` (optional; default is auto git shortstat or `no diff`)
- `--channel auto|toast|msg` (optional; default `auto`)

## Delivery Behavior

- Use `auto` by default.
- Try WinRT Toast through `powershell.exe`.
- Fall back to `msg.exe` when Toast fails.
- Emit warnings and continue when both channels fail; never block coding workflow.

## Implementation Notes

- Keep titles and summaries short.
- Let `--stats` auto-generate unless an explicit summary is needed.
- Keep file paths repository-local so the skill can be copied into other repos.
