---
name: parafork
description: "执行型 Core-Lite Git worktree skill（仅 help/init/do/check/merge）。默认无参执行 init --new + do exec；高风险命令仅通过 CLI 门闩 --yes --i-am-maintainer 放行；锁冲突默认 SAFE_NEXT=init --new。执行命令只读 route-bash/route-powershell。"
---
---------------------- Parafork SKILL.md开始符号 ----------------------
# Parafork 执行协议卡片
- 当你看到此段文字，代表用户意图显式激活此 SKILL, 同意并接受本文件约束。
- 读取并使用本 skill，即默认接受本文件全部硬约束。本文件是必须遵守的任务协议，不是参考建议。任何修改都必须遵循本协议，不得绕开流程直接改仓库。

## Hard Gates（硬门闩）
- **术语定义**
  - `SIDE-EFFECT（状态变更操作）`：任何会改变工作区、git 状态、worktree 元数据或审计状态的操作
  - 例如 `init --new/--reuse`、`do commit`、`merge`、`apply_patch`、写 `paradoc/Log.txt`）。

- **1) 目录与写权限**
  - base repo 默认只读；在 base repo 仅允许 `help/init`。
  - 任何状态变更操作前，必须先进入 parafork worktree 根目录。

- **2) 命令白名单**
  - 仅允许：`help/init/do/check/merge`。
  - `do` 仅允许：`exec|commit`；`check` 仅允许：`status|merge`。
  - `do exec` 仅允许参数：`--strict`。

- **2.1) base 分支策略（init --new）**
  - 默认：`base.branch=autodetect`，直接使用当前分支作为 `BASE_BRANCH`。
  - 若 `base.branch` 为显式值且与当前分支不同：交互模式询问是否改用当前分支；非交互模式默认改用当前分支。

- **3) 统一审批门闩（唯一规则）**
  - 凡命令包含 `--yes --i-am-maintainer`，执行前必须先获得人类明确批准。
  - 未获批准时必须 `FAIL`，禁止自行补门闩继续执行。

- **4) 复用门闩（init --reuse）**
  - 仅允许通过 `init --reuse` 复用，且仅允许在已有 parafork worktree 内执行。
  - 必须携带 CLI 门闩：`--yes --i-am-maintainer`。

- **5) 并发锁门闩**
  - 若 `WORKTREE_LOCK_OWNER` 非当前 agent，必须拒绝。
  - 锁冲突时必须输出：`LOCK_OWNER/AGENT_ID/SAFE_NEXT/TAKEOVER_NEXT`。
  - 锁冲突时必须满足：`NEXT=SAFE_NEXT`，且 `SAFE_NEXT=init --new`。
  - `TAKEOVER_NEXT` 仅为高风险备选（获批后才可执行）。

- **6) 合并门闩（merge）**
  - `merge` 禁止自动执行；仅 maintainer 显式批准后运行。
  - 必须携带 CLI 门闩，并通过 merge 前检查链。

- **7) 执行安全规则**
  - 脚本优先：有脚本能力时禁止裸 `git` 替代。
  - 检测到 `merge/rebase/cherry-pick` 冲突态时，仅可诊断与建议；未经人类明确批准，禁止继续推进冲突相关 git 动作。

- **8) 数据与审计规则**
  - `.worktree-symbol` 仅作 `KEY=VALUE` 数据文件（按首个 `=` 解析），禁止 `source/eval/dot-source/Invoke-Expression`。
  - `.worktree-symbol` 与 `paradoc/` 默认不得进入 git history。
  - worktree 内执行输出应追加到 `paradoc/Log.txt`。

## 固定顺序（执行算法）
1. 判定请求类型：
   - `READ-ONLY`（解释/检索/审阅）
   - `WRITE/SIDE-EFFECT`（状态变更操作）
2. 若为状态变更操作：
   - 先写/更新 plan（优先遵守人类给定计划）
   - 再按平台只读取一个执行路由（Bash 或 PowerShell）
   - 若命令含 CLI 门闩，先发审批请求并等待明确同意
3. 审批请求必须复用以下固定模板：

```text
审批请求：
- 目的：<为什么要执行>
- 命令：<完整可复制命令>
- 风险：<可能影响/失败后果>
- 回退：<失败后的回滚或退出路径>
```

## 失败分支（Failure Output）
- **目录不确定**：先 `help --debug`；仍不确定则停止并请求人类确认目标 repo/worktree。
- **收到状态变更操作请求但不在 worktree**：先 `init --new`，再 `do exec`，之后才可改动。
- **复用缺少 CLI 门闩或用户未明确复用**：直接 `FAIL`，`NEXT` 指向补齐门闩后的完整命令。
- **需要 CLI 门闩但未获人类批准**：直接 `FAIL`，输出审批模板，禁止继续执行。
- **锁冲突**：直接 `FAIL`；输出 `LOCK_OWNER/AGENT_ID/SAFE_NEXT/TAKEOVER_NEXT`，且 `NEXT=SAFE_NEXT`。
- **旧命令请求**：直接 `FAIL`（如 `watch`、`do pull`、`check diff/log/review`）。

## 路由入口（Route Entry）
- Bash：`references/route-bash.md`
- PowerShell：`references/route-powershell.md`

补充参考（非执行入口）：
- `references/scripts.md`
- `references/wiki.md`

---------------------- Parafork SKILL.md截止符号 ----------------------
