---
name: worktree-lite
description: 轻量 Git worktree 流程：修改前创建新 worktree、完成后输出审查摘要、明确询问用户是否合并、合并前生成“动作：修改内容”格式提交信息。Use when users want a simpler replacement for parafork or need create-review-merge flow with explicit merge approval.
---

# Worktree Lite

## 目标

- 用最小流程完成 `创建 worktree -> 修改 -> 审查 -> 询问是否合并 -> 合并`。
- 完全不依赖 parafork 的锁、门闩、审计文件。

## 命令入口

```bash
bash ".codex/skills/worktree-lite/scripts/worktree-lite.sh" <command> [args...]
```

## Commands

- `init [--base <branch>] [--root <dir>] [--id <worktree-id>]`
- `review [--base <branch>]`
- `propose-message [--base <branch>]`
- `merge --target <branch> [--message "动作：修改内容"] [--source <branch>]`

## 默认执行流程

1. 任务涉及写入时，先执行 `init` 并切换到 `WORKTREE_ROOT` 后再改代码。
2. 修改完成后执行 `review`，把变更摘要发给用户审查。
3. 明确询问用户：`是否合并到 <target-branch>？`
4. 只有用户明确同意后才继续。
5. 执行 `propose-message`，给出 `推荐/备选/理由`。
6. 用户确认提交标题后，执行 `merge`。

## 硬约束

- 禁止在未询问用户的情况下自动合并。
- 禁止在未确认提交标题的情况下自动 commit。
- `merge` 前若检测到目标分支工作区不干净，必须停止并提示用户先处理。

## 提交标题格式

- 模板：`动作：修改内容`（全角冒号）。
- 动作词优先级：`修复 > 新增 > 优化 > 修改 > 合并`。
- 输出格式：
  - `推荐：...`
  - `备选1：...`
  - `备选2：...`
  - `理由：...`
