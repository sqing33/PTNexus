# 定时发种功能实现方案

## 功能概述

在导航菜单中新增"定时发种"页面（与"一站多种"并列），支持创建多个定时发种任务。每个任务绑定一组种子（从一站多种种子库选取）、目标站点和固定间隔时间，系统按间隔自动向目标站点发布种子，发布记录复用现有发种日志表。

---

## 数据库设计

### 新表：`scheduled_seed_tasks`

| 列名 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `id` | INTEGER PK | auto | 自增主键 |
| `name` | VARCHAR(128) | NOT NULL | 任务名称 |
| `status` | VARCHAR(16) | `'active'` | `active` / `paused` / `completed` |
| `seeds_json` | TEXT | NOT NULL | JSON 数组 `[{torrent_id, site_name, title}]` |
| `target_sites_json` | TEXT | NOT NULL | JSON 数组 `["站点1", "站点2"]` |
| `interval_minutes` | INTEGER | NOT NULL | 发布间隔（分钟） |
| `current_seed_index` | INTEGER | `0` | 当前种子指针 |
| `current_site_index` | INTEGER | `0` | 当前目标站点指针 |
| `total_published` | INTEGER | `0` | 累计已发布数 |
| `total_skipped` | INTEGER | `0` | 累计跳过数（重复） |
| `loop_enabled` | BOOLEAN | `false` | 循环发种（种子发完后从头开始） |
| `trigger_tag` | VARCHAR(64) | NOT NULL | `"sched:{id}"`，用于关联 publish_logs |
| `last_run_at` | DATETIME | NULL | 上次执行时间 |
| `next_run_at` | DATETIME | NOT NULL | 下次执行时间 |
| `created_at` | DATETIME | auto | 创建时间 |
| `updated_at` | DATETIME | auto | 更新时间（兼作乐观锁） |

**索引：**
- `idx_sched_seed_status_next` → `(status, next_run_at)` — 调度器热路径查询
- `idx_sched_seed_trigger_tag` → `(trigger_tag)` — 关联发种日志

### 调度机制

后台 Worker 每 30 秒轮询，找到 `status='active' AND next_run_at <= now` 的任务：
1. 取 `seeds[current_seed_index]` 和 `target_sites[current_site_index]`
2. 查重：查 `publish_logs` 是否已成功发布过该种子到该站点，重复则跳过
3. 调用现有 `EnqueuePublishQueueBatch` 入队，`scene="scheduled_seeding"`，`trigger=trigger_tag`
4. 推进指针（round-robin），更新 `next_run_at = now + interval`
5. 种子耗尽时：`loop_enabled=true` 则归零，否则 `status='completed'`

---

## 后端实现（Go）

### 修改文件

**1. `server/internal/repository/schema_manager.go`** — 增加 DDL + 列规格 + 索引

在 `createTableSQLs()` 的三个 DB 分支（MySQL/PostgreSQL/SQLite）中添加 `scheduled_seed_tasks` 建表语句，在 `columnSpecs()` 和 `indexSpecs()` 中添加对应条目。

**2. `server/internal/bootstrap/app.go`** — 注册路由、组装依赖

- 创建 `ScheduledSeedRepository`
- 创建 `ScheduledSeedScheduler`，注入 repo + `EnqueuePublishQueueBatch` 引用
- 创建 `ScheduledSeedHandler`
- 注册 `/api/scheduled-seed/*` 路由组
- 启动 scheduler worker

### 新增文件

**3. `server/internal/repository/scheduled_seed_repository.go`**

Model struct（GORM 标签）+ CRUD 方法：
- `Create / GetByID / List / Update / Delete`
- `ToggleStatus(id)` — active ↔ paused 切换
- `FindDueTasks(now)` — 查询到期的活跃任务
- `ClaimAndAdvance(task, newSeedIdx, newSiteIdx, newStatus, nextRun)` — 乐观锁更新（`WHERE updated_at = ?`）
- `CheckDuplicate(torrentID, siteName, targetSite)` — 查重发种日志

**4. `server/internal/service/scheduledseed/scheduler.go`**

后台 Worker（`sync.Once` + goroutine + `time.Ticker`）：
- `Start()` — 启动后台协程
- `Stop()` — 停止信号
- `processTick()` — 每次轮询处理所有到期任务
- `processTask(task, now)` — 单任务处理：查重 → 入队 → 推进指针 → 更新状态

**5. `server/internal/http/handler/scheduled_seed_handler.go`**

7 个 HTTP 端点：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/scheduled-seed/tasks` | 分页列表（支持 status 筛选） |
| POST | `/api/scheduled-seed/tasks` | 创建任务 |
| GET | `/api/scheduled-seed/tasks/:id` | 任务详情 |
| PUT | `/api/scheduled-seed/tasks/:id` | 更新任务 |
| DELETE | `/api/scheduled-seed/tasks/:id` | 删除任务 |
| POST | `/api/scheduled-seed/tasks/:id/toggle` | 启动/暂停切换 |
| GET | `/api/scheduled-seed/seeds` | 可选种子列表（分页+搜索，查 seed_parameters 表） |

---

## 前端实现（Vue 3 + Element Plus）

### 修改文件

**6. `webui/src/router/index.ts`** — 添加路由

```typescript
{
  path: '/scheduled-seeding',
  name: 'scheduled-seeding',
  component: () => import('@/views/ScheduledSeedingView.vue')
}
```

**7. `webui/src/App.vue`** — 添加菜单项

- 桌面导航（~line 40）：`<el-menu-item index="/scheduled-seeding">定时发种</el-menu-item>`（在"一站多种"之后）
- 移动端抽屉（~line 155）：同上
- `routeTitleMap`（~line 257）：`'/scheduled-seeding': '定时发种'`

### 新增文件

**8. `webui/src/views/ScheduledSeedingView.vue`** — 主页面

布局：
- 顶部工具栏：新建任务按钮 + 状态筛选下拉
- 任务列表表格（el-table）：
  - 列：任务名称、种子数、目标站点、间隔(分钟)、状态(Tag)、已发布/已跳过、上次/下次执行、操作(启动/暂停/编辑/日志/删除)
  - 展开行：当前进度（种子 X/N, 站点 Y/M）、循环状态
- 分页组件
- 3 秒自动刷新轮询

**9. `webui/src/components/scheduled-seed/TaskFormDialog.vue`** — 创建/编辑对话框

- 任务名称输入
- 种子选择器：双栏布局（左侧可选种子列表+搜索+分页，右侧已选种子列表），从 `GET /api/scheduled-seed/seeds` 获取数据
- 目标站点多选：el-select multiple，从站点列表 API 获取
- 间隔设置：el-input-number + 分钟/小时单位切换
- 循环发种开关：el-switch

**10. `webui/src/components/scheduled-seed/PublishLogsDrawer.vue`** — 发布记录抽屉

- el-drawer 组件，点击"日志"按钮打开
- 复用现有 `GET /api/publish_logs` 接口，传入 `scene=scheduled_seeding` + `trigger=trigger_tag` 筛选
- 表格列：时间、种子标题、源站、目标站、状态、错误信息

---

## 文件清单

| # | 文件 | 操作 | 说明 |
|---|---|---|---|
| 1 | `server/internal/repository/schema_manager.go` | 修改 | 添加 scheduled_seed_tasks 建表 DDL、列规格、索引 |
| 2 | `server/internal/repository/scheduled_seed_repository.go` | 新增 | Model + CRUD + 乐观锁 + 查重 |
| 3 | `server/internal/service/scheduledseed/scheduler.go` | 新增 | 后台调度 Worker |
| 4 | `server/internal/http/handler/scheduled_seed_handler.go` | 新增 | 7 个 HTTP 端点 |
| 5 | `server/internal/bootstrap/app.go` | 修改 | 组装依赖、注册路由、启动 Worker |
| 6 | `webui/src/router/index.ts` | 修改 | 添加路由 |
| 7 | `webui/src/App.vue` | 修改 | 添加菜单项 + 标题映射 |
| 8 | `webui/src/views/ScheduledSeedingView.vue` | 新增 | 定时发种主页面 |
| 9 | `webui/src/components/scheduled-seed/TaskFormDialog.vue` | 新增 | 任务创建/编辑对话框 |
| 10 | `webui/src/components/scheduled-seed/PublishLogsDrawer.vue` | 新增 | 发布记录抽屉 |

## 实现顺序

1. **数据库层**：schema_manager.go → 建表验证
2. **仓储层**：scheduled_seed_repository.go → CRUD 测试
3. **调度器**：scheduler.go → 后台任务驱动
4. **HTTP 层**：handler + app.go 注册 → API 联调
5. **前端**：API 模块 → 对话框组件 → 主视图 → 路由 + 菜单
