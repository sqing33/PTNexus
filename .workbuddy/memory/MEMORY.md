# PTNexus 长期项目约定

- **不使用 worktree**：2026-08-28 起，用户明确要求所有代码修改**直接在当前分支的主工作区进行**，不再创建 `worktree-lite` worktree / 临时会话分支。AGENTS.md 中对应的 Worktree 策略段落已替换为此约定。
- **Git 安全**：任何提交/回滚/改写历史类操作，必须用户在当前对话中明确要求才执行；默认只改文件、留在工作区。
- **通知约定**：代码写入场景用 `.codex/skills/wsl-win-notify` 发 Windows 原生通知（当前环境 Toast 与 msg.exe 均不可用，会静默失败，属正常）。
- 前端改动后建议跑 `cd webui && pnpm run type-check` 验证。
