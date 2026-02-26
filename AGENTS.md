# 仓库指南

## 项目结构与模块组织

- `server/`：Flask API 与核心逻辑；路由在 `server/api/`，服务在 `server/core/`，辅助工具在 `server/utils/`。
- `webui/`：Vue 3 + TypeScript 前端；页面在 `webui/src/views/`，通用组件在 `webui/src/components/`，Pinia Store 在 `webui/src/stores/`。
- `batch/`、`updater/`、`proxy/`：Go 服务；构建后的二进制分别位于 `batch/batch`、`updater/updater`、`proxy/pt-nexus-box-proxy`。
- `server/configs/`：站点 YAML 映射；`server/data/`：运行时数据；`wiki/`：文档。

## 多版本说明（Python 原版 / Go 版本）

- 本项目同时维护两套实现，互相独立：
  - **Python 原版**：后端 `server/` + 前端 `webui/`。
  - **Go 版本**：后端 `server-go/` + 前端 `webui-go/`。
- 开发与修改必须按版本分隔进行：
  - 不要在 `server-go/` 中直接调用/依赖 `server/`（Python）里已有的函数或实现。
  - 若 Go 版本需要与 Python 原版相同的功能，必须在 `server-go/` 内实现等价逻辑（允许保持相同的接口语义/协议，但实现代码必须在 Go 侧独立存在）。
- 修改 Go 后端（`server-go/`）时遵循 `.codex/skills/server-go-standards/SKILL.md` 中的规范。

## 构建、测试与开发命令

前端（Node 20+/bun）：

```bash
cd webui
bun install
bun run dev        # 本地 UI 在 5173
bun run build      # 输出到 webui/dist
```

后端（Python 3.12/uv）：

```bash
cd server
uv sync
. .venv/bin/activate
uv pip install -r requirements.txt
uv run app.py
```

Go 服务（版本见各自 `go.mod`）：

```bash
./batch/build.sh
./updater/build.sh
./proxy/build.sh
```

全栈：`./start-services.sh`（需要先构建好上述二进制）。

## 编码风格与命名约定

- Python：4 空格缩进，模块与函数使用 snake_case；import 分组保持整洁。
- Vue/TS：2 空格缩进；组件使用 `PascalCase.vue`，页面以 `*View.vue` 结尾。
- Go：遵循标准 `gofmt` 格式化。
- Lint/format：`bun run lint`（oxlint + eslint）与 `bun run format`（prettier）。

## 测试指南

- 后端冒烟测试：`cd server && uv run python test_functionality.py`。
- 前端检查：`bun run type-check` 与 `bun run lint`。
- 当前没有完整单测体系；涉及解析或上传逻辑时建议补充有针对性的测试。

## 配置与数据

- 本地配置使用 `server/.env` 与 `server/data/config.json`；将 `DB_TYPE` 设置为 `sqlite`、`mysql` 或 `postgresql`。
- `server/data/` 视为运行时状态；除非明确要更新夹具/样例数据，否则避免提交本地 DB 或缓存变更。

## 提交与 PR 指南

- ✅ 分支 / Worktree 策略（用于每次会话的代码修改，使用 `worktree-lite` skill）：
  - 只要任务需要对仓库代码进行**写入/修改**（新增/改动/删除受 Git 跟踪的文件），默认将 base repo 视为只读；状态变更操作在独立 worktree 内执行。
  - 写入前创建新 worktree（Bash 路由）：`bash ".codex/skills/worktree-lite/scripts/worktree-lite.sh" init`。
    - worktree 默认创建在 `.worktree-lite/<YYMMDD>-<HEX4>/`，对应分支 `worktree-lite/<YYMMDD>-<HEX4>`。
  - 不要手动用 `git switch -c ...` 创建会话分支；由 `worktree-lite init` 统一创建分支与 worktree。
  - 修改完成后先执行 `review` 输出变更摘要，先让用户审查。
  - 审查后必须显式询问用户是否合并；仅在用户明确同意后执行 `merge`。
  - 合并前执行 `propose-message` 生成 `动作：修改内容` 格式的推荐/备选提交标题，并先让用户确认最终文案。
  - 若当前工作区不干净（已有未提交改动），先停下来问用户要不要：继续在当前状态修改 / 先手动清理后再开 worktree（不要自动执行 stash/reset/restore）。
- ⚠️ Git 安全约束：千万不要触碰任何 Git 回滚/提交/改写历史/批量还原工作区的操作（例如 `git reset` / `git restore` / `git checkout -- .` / `git revert` / `git commit` / `git merge` / `git rebase` / `git cherry-pick` 等），除非用户在当前对话中明确要求。
- ✅ 允许使用只读方式查看之前代码的修改内容与历史（例如 `git status` / `git diff` / `git log` / `git show` / `git blame`）。
- Git 历史提交信息过去常用极简数字（例如 `7`）；没有强制规范，建议使用简短且能说明范围的描述。
- 仅当用户明确要求“提交”时：若改动了 `webui/`、`batch/`、`updater/` 或 `proxy/`，需要一并提交构建产物（`webui/dist/`、`batch/batch`、`updater/updater`、`proxy/pt-nexus-box-proxy`）；`pre-commit` hook 可自动化。
- 仅当用户明确要求“提交”且 `CHANGELOG.json` 有变更时：运行 `python sync_changelog.py` 同步更新 `readme.md` 与 `wiki/docs/index.md`（只读查看不受影响）。
- PR 需说明变更范围、配置影响；如涉及 UI，附截图。
