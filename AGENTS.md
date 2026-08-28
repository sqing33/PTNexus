# 仓库指南

## 项目结构与模块组织

- `server/`：Go API 与核心逻辑（入口在 `server/cmd/server`，其余按目录职责分层）。
- `webui/`：Vue 3 + TypeScript 前端；页面在 `webui/src/views/`，通用组件在 `webui/src/components/`，Pinia Store 在 `webui/src/stores/`。
- `batch/`、`updater/`、`proxy/`：Go 服务；构建后的二进制分别位于 `batch/batch`、`updater/updater`、`proxy/pt-nexus-box-proxy`。
- `server/configs/`：站点 YAML 映射；`server/data/`：运行时数据；`wiki/`：文档。

- 修改 Go 后端（`server/`）时遵循 `server-standards` skill 中的规范。

## 构建、测试与开发命令

前端（Node 20+/pnpm）：

```bash
cd webui
pnpm install
pnpm run dev        # 本地 UI 在 5173
pnpm run build      # 输出到 webui/dist
```

后端（Go）：

```bash
cd server
go run ./cmd/server
```

Go 服务（版本见各自 `go.mod`）：

```bash
./batch/build.sh
./updater/build.sh
./proxy/build.sh
```

全栈：`./start-services.sh`（需要先构建好上述二进制）。

## 编码风格与命名约定

- Vue/TS：2 空格缩进；组件使用 `PascalCase.vue`，页面以 `*View.vue` 结尾。
- Go：遵循标准 `gofmt` 格式化。
- Lint/format：`pnpm run lint`（oxlint + eslint）与 `pnpm run format`（prettier）。

## 测试指南

- 后端基础检查：`cd server && go test ./...`。
- 前端检查：`pnpm run type-check` 与 `pnpm run lint`。
- 当前没有完整单测体系；涉及解析或上传逻辑时建议补充有针对性的测试。

## 配置与数据

- 本地配置使用 `server/.env` 与 `server/data/config.json`；将 `DB_TYPE` 设置为 `sqlite`、`mysql` 或 `postgresql`。
- `server/data/` 视为运行时状态；除非明确要更新夹具/样例数据，否则避免提交本地 DB 或缓存变更。

## Skills 同步约定

- `AGENTS.md` 是主说明文件，`CLAUDE.md` 必须是指向 `AGENTS.md` 的软链接。
- skills 路径允许使用 `.codex/skills` 或 `.claude/skills`，工具应自动发现并解析对应 skill。
- 需要修复 skills 软链接时，执行：`bash scripts/link-path.sh`。

## 通知约定（强制）

- 任何会话只要发生代码写入/修改，必须使用 `wsl-win-notify` skill 的通知能力。
- 至少在以下节点触发 Windows 原生通知：需要人工选择前、遇到阻塞错误时、任务完成并可交付时。
- 若用户明确要求“修改后提醒我 / 人工介入时通知 / 完成通知 / notify me when done”，必须全程按该 skill 执行通知。

## 提交与 PR 指南

- ✅ 分支 / 修改策略：**直接在当前分支的主工作区修改**，不创建 worktree、不创建临时会话分支、不使用 `worktree-lite` skill。
  - 任务需要对仓库代码进行写入/修改（新增/改动/删除受 Git 跟踪的文件）时，直接在当前检出分支上改，改动留在工作区等待用户处理。
  - 修改完成后用 `git status` / `git diff` 输出变更摘要供用户审查，无需执行 review / merge-options 等流程。
- ⚠️ Git 安全约束：千万不要触碰任何 Git 回滚/提交/改写历史/批量还原工作区的操作（例如 `git reset` / `git restore` / `git checkout -- .` / `git revert` / `git commit` / `git merge` / `git rebase` / `git cherry-pick` 等），除非用户在当前对话中明确要求。
- ✅ 允许使用只读方式查看之前代码的修改内容与历史（例如 `git status` / `git diff` / `git log` / `git show` / `git blame`）。
- Git 历史提交信息过去常用极简数字（例如 `7`）；没有强制规范，建议使用简短且能说明范围的描述。
- 仅当用户明确要求“提交”时：若改动了 `webui/`、`batch/`、`updater/` 或 `proxy/`，需要一并提交构建产物（`webui/dist/`、`batch/batch`、`updater/updater`、`proxy/pt-nexus-box-proxy`）；`pre-commit` hook 可自动化。
- 仅当用户明确要求“提交”且 `CHANGELOG.json` 有变更时：运行 `python sync_changelog.py` 同步更新 `readme.md` 与 `wiki/docs/index.md`（只读查看不受影响）。
- PR 需说明变更范围、配置影响；如涉及 UI，附截图。
