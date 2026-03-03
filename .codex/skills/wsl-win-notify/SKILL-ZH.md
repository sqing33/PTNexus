---
name: wsl-win-notify
description: '在编码工作流中从 WSL 发送原生 Windows 通知。当代码被修改且应在关键检查点提醒用户时，默认使用：在请求人工选择前、在权限/升级提示前、在错误导致阻塞时，以及工作完成时。可由中文或英文意图触发，例如“修改后提醒我”、“人工介入时通知”、“任务完成通知”、“notify me when done”或“send Windows notification”。'
---

# WSL 人工通知

## 目标

在编码任务中的高信号检查点，通过从 WSL 发送 Windows 通知来让用户及时获知进展。
仅在需要人工介入的节点和最终完成时使用此技能，不要为每条命令都发送通知。

## 调用优先级

- 只要运行时可用，在代码编辑任务中将此技能视为默认基础行为。
- 将 AGENTS.md 规则作为更高优先级的项目覆盖，而不是唯一触发来源。
- 无论是否存在 AGENTS.md，都保持一致行为：在 `decision`、`permission`、`blocked` 和 `done` 时通知。

## 事件策略

- 在让用户在实现选项间做选择之前发送 `decision`。
- 在请求权限或升级之前发送 `permission`。
- 当失败阻塞进度且需要用户操作时发送 `blocked`。
- 在最终完成回复之前立即发送 `done`。

## 命令

在仓库根目录（或任意 git 工作树）中运行：

```bash
./.codex/skills/wsl-win-notify/scripts/notify_windows.sh \
  --event done \
  --title "Feature update complete" \
  --summary "All requested code changes are implemented." \
  --next-action "Open the diff and review the final answer."
```

### 选项

- `--event decision|permission|blocked|done`（必填）
- `--title <text>`（必填）
- `--summary <text>`（必填）
- `--next-action <text>`（可选）
- `--stats <text>`（可选；默认自动使用 git shortstat，或 `no diff`）
- `--channel auto|toast|msg`（可选；默认 `auto`）

## 发送行为

- 默认使用 `auto`。
- 先通过 `powershell.exe` 尝试 WinRT Toast。
- 当 Toast 失败时，回退到 `msg.exe`。
- 若两个通道都失败，则输出警告并继续；绝不阻塞编码工作流。

## 实现说明

- 标题和摘要保持简短。
- 除非需要明确摘要，否则让 `--stats` 自动生成。
- 文件路径保持为仓库相对路径，以便该技能可复制到其他仓库中使用。
