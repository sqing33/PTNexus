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

- `init [--base <branch>] [--root <dir>] [--id <worktree-id>] [--topic <summary>]`
- `review [--base <branch>]`
- `merge-options [--target <branch>] [--source <branch>] [--format <plain|codex>]`
- `propose-message [--base <branch>]`（兼容旧流程）
- `merge --target <branch> [--message "动作：修改内容"] [--source <branch>]`

## 默认执行流程

1. 任务涉及写入时，先用 `init --topic "<问题摘要>"` 创建 worktree 并切换到 `WORKTREE_ROOT` 后再改代码。
2. 修改完成后执行 `review`，把变更摘要发给用户审查。
3. 执行 `merge-options --target <target-branch>`：
   - 若判定与上次同系列改动“内容接近”，直接复用上次提交标题并返回 `AUTO_DECISION`（跳过提问）。
   - 否则在一个提问里给出 5 个选项：
   - 1/2/3：`合并到 <target-branch>（提交标题）`
   - 4：`继续修改（暂不合并）`
   - 5：`暂不合并，等待后续指令`
4. 用户选择 1/2/3 时，使用对应提交标题执行 `merge`。
5. 用户选择 4/5 时，不执行合并。

## 硬约束

- 禁止在未询问用户的情况下自动合并。
- 禁止在未确认提交标题的情况下自动 commit。
- 当目标分支工作区存在已跟踪本地改动，或当前不在目标分支时，`merge` 必须在临时 merge worktree 中执行，避免直接污染用户当前主工作区。
- 若目标分支最新一次也是同系列来源分支合并，`merge` 应把前一次提交折叠（fold）进当前提交，避免碎片化提交历史。

## 脏工作区处理

- 允许 `main`（或目标分支）存在未暂存/未提交改动时继续创建新 worktree。
- 允许在目标分支工作区有本地改动时继续执行 `merge`：
  - 保留这些本地改动原样不动（不自动 stash / reset / restore）。
  - 合并在临时 worktree 完成后，仅更新目标分支指针。
  - 若目标分支正被该脏工作区检出，需提示用户：分支前进后 `git status` 展示可能变化，但本地改动内容不会被清空。
  - 合并后自动对齐“由合并提交变更且用户未触碰”的路径，避免出现整批反向 staged 假象。
  - 若某路径同时被用户本地改动与合并提交修改，跳过该路径自动对齐并输出提示。

## 合并后清理

- 合并成功后默认自动清理本次来源 worktree，避免无用 worktree 堆积。
- 若来源分支是 `worktree-lite/*`，清理 worktree 后尝试删除该来源分支。
- 下一次同会话继续修改时，重新执行 `init --topic "<问题摘要>"` 创建新 worktree。

## 提问样式

- 默认模板（`plain`，单次 5 选项）：
  - `请选择下一步：`
  - `1. 合并到 <target-branch>（推荐：<标题1>）`
  - `2. 合并到 <target-branch>（备选1：<标题2>）`
  - `3. 合并到 <target-branch>（备选2：<标题3>）`
  - `4. 继续修改（暂不合并）`
  - `5. 暂不合并，等待后续指令`
- 原生选项（Codex）：
  - 执行 `merge-options --target <target-branch> --format codex`，输出可直接用于 `request_user_input` 的 JSON payload。
  - 由于 Codex 原生选项单题最多 3 个选项，payload 使用两题：
    - `merge_choice`：3 个“合并并提交”选项（推荐/备选1/备选2）
    - `hold_choice`：2 个“不合并”选项（继续修改/暂不合并）
  - 处理规则：若用户在 `merge_choice` 选了项 1/2/3，执行对应 `merge` 命令；否则按 `hold_choice` 结果不合并。
- 自动复用规则：
  - `merge-options` 会比较本次与上次同系列来源分支的变更文件集合相似度。
  - 相似度 >= 60% 时，输出自动合并决策（复用上次提交标题），无需再次提问。

## 提交标题格式

- 模板：`动作：修改内容`（全角冒号）。
- 动作词优先级：`修复 > 新增 > 优化 > 修改 > 合并`。
- 输出格式：
  - `推荐：...`
  - `备选1：...`
  - `备选2：...`
  - `理由：...`
