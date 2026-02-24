---
name: commit-message-formatter
description: 统一 PTNexus 提交标题为“动作：修改内容”格式，并基于当前改动生成可直接使用的 commit message。Use when users ask to write/optimize/format commit messages, summarize branch changes before commit, or enforce repository commit-title conventions.
---

# PTNexus Commit Message Formatter

## 目标

- 输出可直接用于 `git commit -m` 的单行标题。
- 保持仓库主流风格：`动作：修改内容`（全角冒号 `：`）。
- 在信息不足时先补充上下文，再给出 1-3 条候选标题。

## 执行流程

1. 检查最近提交风格：

   ```bash
   git log -n 20 --pretty=format:'%s'
   ```

2. 在需要更稳的动作词优先级时，统计更长窗口：

   ```bash
   git log -n 120 --pretty=format:'%s' | awk 'match($0,/^([^：:[:space:]]+)[：:]/,m){print m[1]}' | sort | uniq -c | sort -nr
   ```

3. 读取本次改动范围（优先暂存区）：

   ```bash
   git diff --name-only --cached
   git diff --name-only
   ```

4. 根据变更目标选择动作词，并输出 1 条推荐标题 + 最多 2 条备选标题。

## 格式规范

- 使用模板：`动作：修改内容`。
- 优先使用全角冒号 `：`，避免半角 `:`。
- 动作词优先级：`修复` > `新增` > `优化` > `修改` > `合并`。
- 修改内容聚焦“对象 + 结果/原因”，避免空泛描述。
- 单条标题建议控制在 12-45 字。
- 若一次提交包含两个并列变更，可写成：`修复：... 优化：...`。

## 动作词映射

- 处理报错、逻辑错误、兼容问题：使用 `修复`。
- 增加新功能、站点、工具或入口：使用 `新增`。
- 性能提升、流程提速、体验改善：使用 `优化`。
- 参数调整、行为重构、结构性改动：使用 `修改`。
- 纯合并或整合型提交：使用 `合并`。

## 负面示例（避免）

- `1`
- `修复运行错误`
- `go版本前后端修复 (vibe-kanban xxxx)`（除非用户明确要求保留该后缀）

## 输出格式

按以下顺序输出：

1. `推荐：动作：修改内容`
2. `备选1：动作：修改内容`（可选）
3. `备选2：动作：修改内容`（可选）
4. `理由：一句话说明动作词与范围匹配原因`

## 示例

- `修复：SSD 标签提取误判“禁转”标签问题`
- `新增：Windows 客户端 BDInfo 提取工具`
- `优化：一站多种页面种子数据获取速度`
- `修改：下载进度计算逻辑并补充部分做种状态`

## 额外约束

- 用户明确要求英文提交时，改为 `action: summary`，但保持同样语义精度。
- 用户明确要求保留外部系统后缀（如 `vibe-kanban`）时，再附加到标题末尾。
- 不执行 `git commit`，仅提供格式化后的提交文案。
