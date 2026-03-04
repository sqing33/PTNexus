# PT Nexus 桌面化实施计划（desktop）

## 1. 计划摘要

本计划的目标是：

1. 基于已初始化的 Wails 官方骨架（`desktop/`），完成 Windows 桌面版接入方案。
2. 严格分两部分实施：
   - 第一部分：仅修改 `desktop/`（先做）。
   - 第二部分：修改其他目录（`server/`、`webui/`、`updater/`，后做，当前不执行）。
3. 保证 Docker 现有运行方式不受影响。

当前约束：你正在修改其他目录，因此现在只完善计划，不执行跨目录改动。

---

## 2. 当前状态（已确认）

1. `desktop/` 已通过 `wails init` 生成官方骨架，包含：
   - `desktop/main.go`
   - `desktop/app.go`
   - `desktop/wails.json`
   - `desktop/frontend/`
   - `desktop/build/`
2. 项目现有业务前后端位于：
   - 前端：`webui/`
   - 后端：`server/`
   - 更新能力：`updater/`（当前 `/update/*` 在此实现）
3. 当前 SSE 关键点（后续桌面需改轮询）：
   - `/api/migrate/logs/stream/:task_id`
   - `/api/migrate/bdinfo_sse/:seed_id`
   - `/api/migrate/publish_batch/stream/:batch_id`

---

## 3. 总体技术方案（定稿）

1. 桌面运行时采用 Wails `AssetServer.Handler` 分流：
   - `/api/*` -> `server` Gin Engine（进程内）
   - `/update/*` -> updater handlers（进程内）
   - 其他路径 -> 前端静态资源
2. 协议策略：
   - Docker/Web 保持 SSE，不破坏现状。
   - Windows/Desktop 切换为轮询。
3. 数据目录：Windows 默认 `%APPDATA%/PT-Nexus`。

---

## 4. 实施拆分（核心）

## 第一部分：仅 `desktop/` 内实施（先做）

说明：以下任务全部限定在 `desktop/` 目录，不修改任何其他目录文件。

### 4.1 目标

1. 让 `desktop/` 成为可承接后续接入的稳定桌面壳工程。
2. 先把目录内可独立完成的配置、脚本、结构、文档一次性完善。

### 4.2 任务清单（仅 `desktop`）

- [x] A1. 清理并标准化 `desktop` 工程结构
  - [x] 保留 Wails 官方骨架必要文件。
  - [x] 增补目录：`desktop/internal/`、`desktop/docs/`（构建入口统一在根目录 `scripts/`）。

- [x] A2. 完善桌面接入文档（仅本目录）
  - [x] 编写 `desktop/docs/integration.md`：说明未来如何挂接 `server`、`updater`、`webui`。
  - [x] 编写 `desktop/docs/build.md`：说明开发/打包命令、目录依赖、输入输出。
  - [x] 在 `desktop/README.md` 中加入执行顺序与边界说明。

- [x] A3. 预留桌面入口的分层骨架（不接入业务）
  - [x] 在 `desktop/internal/desktopapp/` 预留路由分发结构。
  - [x] 在 `desktop/main.go` 规划注释和 TODO：`/api`、`/update`、静态资源三路分发。
  - [x] 仅保留空实现或 mock handler，确保不依赖外部目录即可编译。

- [x] A4. 预置构建脚本（仅本目录）
  - [x] 新增根脚本 `scripts/package-desktop.sh`：桌面开发/前端构建/安装包统一入口。
  - [x] `desktop/wails.json` 的 `frontend:install/build/dev` 钩子全部指向统一脚本。
  - [x] 移除 `desktop/scripts/*.sh` 分散入口，避免多入口维护成本。

- [x] A5. 预置配置模板（仅本目录）
  - [x] 在 `desktop/docs/env.md` 记录未来需要的环境变量：
    - `PTNEXUS_BASE_DIR`
    - `PTNEXUS_DATA_DIR`
    - `PTNEXUS_STATIC_DIR`
    - `VITE_DESKTOP`
  - [x] 记录 Windows 默认路径策略与首次初始化行为。

- [x] A6. 第一部分验收（仅本目录）
  - [x] `desktop` 结构清晰、文档齐全。
  - [x] 本目录脚本具备可执行入口（即使为占位流程）。
  - [x] 计划清楚标注第二部分任务，不产生跨目录修改。

### 4.4 本轮补充：桌面壳指向真实前后端（仍仅改 desktop）

说明：以下是“仅为验证打包/安装流程”的接线，不修改 `webui/`、`server/`、`updater/` 的业务逻辑实现。

- [x] C1. 前端构建改为使用 `../webui`
  - [x] `desktop/wails.json` 使用脚本调用 `pnpm install/build/dev`。
  - [x] `webui/dist` 同步到 `desktop/frontend/dist` 供 embed。

- [x] C2. 后端接入 `../server`（进程内）
  - [x] `desktop/go.mod` 添加 `replace` 引用 `../server`。
  - [x] `/api/*` 转发到 `server` Gin Engine（不再使用 mock）。
  - [x] `/update/*` 通过 updater sidecar 代理（兼容现有网关职责）。

- [x] C3. Windows 图标使用 `webui/public/favicon.ico`
  - [x] 打包前复制到 `desktop/build/windows/icon.ico`。

- [x] C4. NSIS 安装器中文化 + 默认安装目录
  - [x] 安装器语言改为中文。
  - [x] 默认安装目录改为 `D:\\Program Files (x86)\\...`。

- [x] C5. 优化 `desktop/.gitignore`
  - [x] 忽略构建产物目录、安装器临时目录、sidecar 二进制目录。
  - [x] 忽略前端生成文件（`dist`、`wailsjs`、`package-lock`、md5）。

- [x] C6. 提供一条命令完成打包
  - [x] 新增 `scripts/package-desktop.sh`。
  - [x] 支持执行单命令：`bash scripts/package-desktop.sh`。

### 4.3 第一部分交付物

1. `desktop/Plan.md`（本文件）
2. `desktop/docs/*.md`（集成、构建、环境说明）
3. `scripts/package-desktop.sh`（统一脚本入口）
4. `desktop/internal/*`（桌面分层骨架占位）

---

## 第二部分：其他目录实施（后做，当前不执行）

说明：以下内容是后续实施计划，当前阶段只保留在计划中，不动代码。

### 5.1 后端目录改造（`server/`）

- [ ] B1. 桌面轮询新增接口：`GET /api/migrate/logs/poll/:task_id?cursor=<int>`
- [ ] B2. 在服务层引入日志增量缓冲（cursor + completed）。
- [ ] B3. 保持 SSE 端点继续可用（Web/Docker 不变）。

### 5.2 更新模块改造（`updater/`）

- [ ] B4. 抽取 updater 路由注册逻辑（可嵌入桌面进程）。
- [ ] B5. 保持 `/update/*` 协议不变。

### 5.3 前端目录改造（`webui/`）

- [ ] B6. 加入 `isDesktopRuntime()` 分支。
- [ ] B7. `LogProgress.vue`：Desktop 改轮询，Web 保持 SSE。
- [ ] B8. `CrossSeedPanel.vue`：Desktop 改轮询，Web 保持 SSE。
- [ ] B9. 桌面首版禁用 batch 入口（按既定范围）。

### 5.4 桌面与业务接入（回到 `desktop/`）

- [ ] B10. 真正接入 `server` Gin Engine 到 Wails Handler。
- [ ] B11. 真正接入 updater handlers 到 Wails Handler。
- [ ] B12. 前端构建产物改为使用 `webui/dist`（替代默认模板前端）。

### 5.5 第二部分验收

- [ ] B13. Windows 端完整流程：登录、迁移、日志进度、更新检查。
- [ ] B14. Docker 端回归：SSE 行为和现有部署不变。

---

## 6. 接口与兼容性约束

### 6.1 保持不变

1. `/api/*` 既有接口语义。
2. `/update/*` 既有接口语义。
3. Docker/Web 的 SSE 行为。

### 6.2 新增

1. `GET /api/migrate/logs/poll/:task_id?cursor=<int>`（供桌面轮询）。

建议返回：

```json
{
  "success": true,
  "task_id": "fetch_xxx",
  "events": [],
  "next_cursor": 0,
  "completed": false
}
```

---

## 7. 风险与缓解

1. 风险：桌面与 Web 两套传输路径漂移。
   - 缓解：统一后端事件源，仅传输层差异化。
2. 风险：轮询请求频率过高带来空转开销。
   - 缓解：1s 起步 + 退避到 2~5s + cursor 增量。
3. 风险：桌面工程与 Docker 构建耦合。
   - 缓解：桌面脚本和入口保持独立，不改 Docker 启动链。

---

## 8. 执行策略（本轮）

1. 当前只执行第一部分（`desktop/` 内）。
2. 第二部分仅保留计划，不改任何其他目录文件。
3. 等你完成其他目录在途修改后，再进入第二部分实施。
