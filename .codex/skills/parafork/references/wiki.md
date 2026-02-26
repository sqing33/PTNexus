# Parafork 维护说明（实现向）

> 本文档用于维护实现细节；执行策略以 `parafork/SKILL.md` 为唯一准则。
> 如文档冲突：`SKILL.md` 优先。

## 1. 定位
- Parafork 是脚本执行型 skill，不承担 Spec/复利沉淀职责。
- 目标：在保持硬门闩的同时降低 agent 决策负担。

## 2. 目录结构

```text
parafork/
  SKILL.md
  bash-scripts/
    parafork.sh
    _lib.sh
  powershell-scripts/
    parafork.ps1
    _lib.ps1
  references/
    route-bash.md
    route-powershell.md
    scripts.md
    wiki.md
  settings/
    config.toml
  assets/
    Plan.md
    Exec.md
    Merge.md
```

## 3. 运行边界
- **base-allowed**：`help`、`init --new`。
- **worktree-required**：`init --reuse`、`do`、`check`、`merge`。
- 状态变更操作必须在 worktree 内进行（初始化创建除外）。

## 4. 数据文件与并发门禁
- `.worktree-symbol` 是 KEY=VALUE 数据文件，按首个 `=` 解析。
- 关键字段：
  - `PARAFORK_WORKTREE=1`
  - `WORKTREE_ID/WORKTREE_ROOT/BASE_ROOT`
  - `WORKTREE_USED`
  - `WORKTREE_LOCK/WORKTREE_LOCK_OWNER/WORKTREE_LOCK_AT`
- 并发冲突时默认安全路径：`SAFE_NEXT=init --new`。

## 5. 输出协议
每个关键分支应输出：

```text
WORKTREE_ID=<...|UNKNOWN>
PWD=<abs path>
STATUS=PASS|FAIL
NEXT=<copy/paste command>
```

可附加字段（如 `LOCK_OWNER/AGENT_ID/SAFE_NEXT/TAKEOVER_NEXT`），但不可替代 4 个基础字段。

## 6. 命令实现映射
- Bash：`parafork/bash-scripts/parafork.sh`
- PowerShell：`parafork/powershell-scripts/parafork.ps1`
- 目标：同输入同语义（尤其 `STATUS/NEXT` 与 gate 触发点）。

## 7. 配置约定
`parafork/settings/config.toml`：

```toml
[base]
branch = "autodetect"

[workdir]
root = ".parafork"
rule = "{YYMMDD}-{HEX4}"

[custom]
autoplan = false
autoformat = true

[control]
squash = true
```

- `base.branch = "autodetect"` 时，`init --new` 默认使用当前分支。
- 当 `base.branch` 显式设置且与当前分支不一致时，脚本会询问是否改用当前分支（非交互模式默认改用当前分支）。

## 8. 验证入口
- 发布包仅包含运行必需文件，不内置测试脚本/测试报告。
- 本地开发验证入口（非发布）：
  - 冒烟：`bash dev/qa/regression-smoke.sh`
  - 全量：按 `dev/qa/regression-checklist.md` 逐项执行。
