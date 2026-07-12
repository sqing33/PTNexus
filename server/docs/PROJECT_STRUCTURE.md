# server 项目结构与功能说明（当前版本）

> 更新时间：2026-02-15  
> 说明：本文档按当前仓库真实代码结构整理，重点反映迁移链路已重构为 `acquire -> processing -> publish` 三阶段模型。

## 1. 项目概览

`server` 是 PT Nexus 的 Go 后端实现，采用分层结构：

- HTTP 层：`internal/http/handler` + `internal/http/middleware`
- 业务层：`internal/service`
- 数据层：`internal/repository`
- 启动组装：`internal/bootstrap/app.go`

迁移核心链路已经从旧的扁平式 `migrate/*` 逻辑，收敛为：

1. `acquire/`：抓取和提取（公共提取器 + 站点特殊提取器）
2. `processing/`：修复、校验、补全、标准化、入库
3. `publish/`：从上下文/数据库发布到目标站点、下载器处理

---

## 2. 根目录结构

| 路径 | 说明 |
|---|---|
| `server/go.mod` / `server/go.sum` | Go 模块与依赖声明 |
| `server/.env` | 本地环境变量 |
| `server/sites_data.json` | 站点基础元数据（同步入库来源之一） |
| `server/cmd/server/main.go` | 程序入口（加载环境、初始化日志、启动 Gin） |
| `server/configs/*.yaml` | 站点配置与映射规则（含 `global_mappings.yaml`） |
| `server/bdinfo/` | BDInfo 工具文件（Linux/Windows） |
| `server/data/` | 运行时配置、日志、数据库、临时文件 |
| `server/docs/` | 文档目录（含注释日志规范与本文件） |
| `server/internal/` | 核心业务代码目录 |

---

## 3. internal 顶层模块

### 3.1 `internal/bootstrap`

| 文件 | 说明 |
|---|---|
| `internal/bootstrap/app.go` | 应用装配入口：初始化配置、数据库、Service、Handler、路由和中间件 |

`NewApp()` 中将 `migrationflow.NewMigrateService(...)` 作为迁移能力入口注入到 Handler。

### 3.2 `internal/config`

| 文件 | 说明 |
|---|---|
| `internal/config/manager.go` | 读取/写入 `config.json`，处理默认值与认证配置 |
| `internal/config/runtime_paths.go` | 运行目录解析（base/data/static/logs 等） |
| `internal/config/database_config.go` | 数据库配置结构与读取辅助 |
| `internal/config/network_proxy.go` | 应用级网络代理配置结构、解析与校验 |

### 3.3 `internal/platform/logx`

| 文件 | 说明 |
|---|---|
| `internal/platform/logx/logx.go` | 统一中文日志输出、级别控制、滚动日志 |
| `internal/platform/netproxy/netproxy.go` | 应用级 HTTP 代理管理器，支持运行时热更新与 transport 接管 |

### 3.4 `internal/repository`

| 文件 | 说明 |
|---|---|
| `internal/repository/db.go` | DB 初始化与连接（sqlite/mysql/postgresql） |
| `internal/repository/schema_manager.go` | 建表/补字段/索引/结构维护 |
| `internal/repository/sites_sync.go` | 从 `sites_data.json` 同步站点元数据 |
| `internal/repository/migrate_repository.go` | 迁移相关查询与写入 |
| `internal/repository/torrent_repository.go` | 种子基础查询、路径列表等 |
| `internal/repository/torrent_data_repository.go` | 种子详情查询、缓存站点查询 |
| `internal/repository/torrent_sync_repository.go` | 下载器同步写库相关 |
| `internal/repository/stats_repository.go` | 统计维度查询 |
| `internal/repository/site_repository.go` | 站点管理、Cookie 更新、状态 |
| `internal/repository/cross_seed_repository.go` | Cross-seed 记录读写 |
| `internal/repository/local_query_repository.go` | 本地扫描/重复分析相关读写 |
| `internal/repository/helpers.go` | Repository 公共辅助 |
| `internal/repository/value_helpers.go` | 类型转换辅助 |

### 3.5 `internal/http`

#### `internal/http/middleware`

| 文件 | 说明 |
|---|---|
| `internal/http/middleware/auth.go` | JWT 与内部令牌鉴权 |
| `internal/http/middleware/request_context.go` | 请求上下文注入（请求 ID、用户 ID） |
| `internal/http/middleware/request_logger.go` | 请求日志 |

#### `internal/http/handler` 顶层处理器

| 文件 | 说明 |
|---|---|
| `internal/http/handler/auth_handler.go` | 登录、认证状态、修改密码 |
| `internal/http/handler/settings_handler.go` | 设置子模块总入口（转发到 `handler/settings`） |
| `internal/http/handler/config_handler.go` | 源优先级/标签映射/批量抓取筛选配置 |
| `internal/http/handler/sites_handler.go` | 站点列表、更新、删除、Cookie |
| `internal/http/handler/torrents_handler.go` | 路径列表等基础种子接口 |
| `internal/http/handler/torrents_data_handler.go` | 数据列表、刷新、IYUU |
| `internal/http/handler/stats_handler.go` | 统计与速度图表 |
| `internal/http/handler/cross_seed_handler.go` | Cross-seed 查询、删除等 |
| `internal/http/handler/local_query_handler.go` | 本地扫描、缓存、重复分析 |
| `internal/http/handler/go_proxy_handler.go` | Go 代理增强接口（批量转种增强等） |
| `internal/http/handler/torrent_transfer_handler.go` | 种子转移流程接口 |
| `internal/http/handler/logs_handler.go` | 日志导出 |
| `internal/http/handler/sites_cookie_extension.go` | 浏览器插件 Cookie 同步目标与批量写入接口 |

#### `internal/http/handler/migrate`

| 文件 | 说明 |
|---|---|
| `internal/http/handler/migrate/types.go` | 迁移 Handler 结构体与构造函数 |
| `internal/http/handler/migrate/helpers.go` | 迁移 Handler 解析/SSE 输出辅助 |
| `internal/http/handler/migrate/db_seed_publish.go` | DB 查询、抓取入库、发布接口 |
| `internal/http/handler/migrate/downloader_media.go` | 下载器、标题解析、媒体校验接口 |
| `internal/http/handler/migrate/batch_fetch_logs.go` | 批量抓取与日志流接口 |
| `internal/http/handler/migrate/publish_batch_stream.go` | 发布批任务 SSE 流 |
| `internal/http/handler/migrate/bdinfo.go` | BDInfo 状态/回调/任务管理接口 |
| `internal/http/handler/migrate/task_monitor.go` | 全局任务监控聚合接口 |

#### `internal/http/handler/settings`

| 文件 | 说明 |
|---|---|
| `internal/http/handler/settings/types.go` | 设置 Handler 结构体与构造 |
| `internal/http/handler/settings/helpers.go` | 设置 Handler 公共转换函数 |
| `internal/http/handler/settings/config_cookiecloud.go` | 基础设置与 CookieCloud 同步 |
| `internal/http/handler/settings/downloader_connection.go` | 下载器连通性、版本、流量信息 |
| `internal/http/handler/settings/cross_seed_migration.go` | Cross-seed 设置与迁移历史 |
| `internal/http/handler/settings/ui_upload_iyuu.go` | UI 设置、上传、IYUU 设置 |
| `internal/http/handler/settings/background_images.go` | 本地背景图列表/上传/下载 URL/删除/静态访问 |

---

## 4. internal/service 总体

### 4.1 顶层 Service 入口文件

| 文件 | 说明 |
|---|---|
| `internal/service/auth_service.go` | 认证服务 |
| `internal/service/settings_service.go` | 设置服务入口（组合 `settings/*`） |
| `internal/service/stats_service.go` | 统计服务入口（组合 `stats/*`） |
| `internal/service/torrent_data_service.go` | 种子数据服务入口（组合 `torrentdata/*`） |
| `internal/service/torrent_transfer_service.go` | 种子转移服务入口（组合 `torrenttransfer/*`） |
| `internal/service/cross_seed_service.go` | Cross-seed 服务入口 |
| `internal/service/local_query_service.go` | 本地查询服务入口 |
| `internal/service/go_proxy_service.go` | Go 代理服务，组合 migrate |
| `internal/service/go_proxy_batch_enhance.go` | 批量转种增强入口（强制开启视频体积过滤） |
| `internal/service/tracker_service.go` | 下载器流量采集服务入口 |
| `internal/service/log_export_service.go` | 日志导出服务入口 |
| `internal/service/iyuu_task_service.go` | IYUU 批任务服务入口 |
| `internal/service/common_helpers.go` | 顶层公共类型转换辅助 |

### 4.2 迁移编排层 `internal/service/migrationflow`

该目录只负责编排，不承载底层解析/修复细节。

| 文件 | 说明 |
|---|---|
| `internal/service/migrationflow/types.go` | `MigrateService` 定义与状态对象装配 |
| `internal/service/migrationflow/helpers.go` | 编排辅助（如反向映射读取） |
| `internal/service/migrationflow/log_stream.go` | 抓取日志流状态封装 |
| `internal/service/migrationflow/actions.go` | 下载、一步迁移、预览更新、聚合查询 |
| `internal/service/migrationflow/seed_title.go` | DB 种子标题查询辅助 |
| `internal/service/migrationflow/torrent_video_size.go` | torrent 视频体积统计封装（供批量增强调用） |
| `internal/service/migrationflow/fetch_store.go` | 抓取并入库总入口（调度 acquire + processing） |
| `internal/service/migrationflow/db_seed_query.go` | DB 种子参数查询入口 |
| `internal/service/migrationflow/db_seed_update.go` | DB 种子参数手动更新入口 |
| `internal/service/migrationflow/screenshot_review.go` | 截图人工确认状态更新入口 |
| `internal/service/migrationflow/batch_fetch.go` | 批量抓取任务管理 |
| `internal/service/migrationflow/downloader.go` | 发布后加种/下载器信息入口 |
| `internal/service/migrationflow/media.go` | 标题解析、媒体校验、MediaInfo 刷新入口 |
| `internal/service/migrationflow/mediainfo_refresh.go` | MediaInfo 异步刷新调度 |
| `internal/service/migrationflow/tagging_helpers.go` | 标签重算与持久化辅助 |
| `internal/service/migrationflow/publish_batch.go` | 单发/批发发布编排与事件流 |
| `internal/service/migrationflow/task_monitor.go` | 聚合批量抓取/发布/BDInfo/发布队列任务快照 |
| `internal/service/migrationflow/bdinfo_status.go` | BDInfo 状态/列表查询入口 |
| `internal/service/migrationflow/bdinfo_callbacks.go` | BDInfo 回调接入入口 |
| `internal/service/migrationflow/bdinfo_task_ops.go` | BDInfo 任务启动/重启/清理 |

---

## 5. 迁移三阶段目录（重点）

### 5.1 `internal/service/acquire`（获取与提取）

### `acquire/extract`

实现“公共提取器 + 多站点特殊提取器”，输出统一结构，供后续处理流水线复用。

| 文件 | 说明 |
|---|---|
| `internal/service/acquire/extract/types.go` | 提取输入输出结构体、统一参数构建 |
| `internal/service/acquire/extract/public.go` | 公共提取器封装 |
| `internal/service/acquire/extract/site_adapters.go` | 站点特殊提取器适配器注册 |
| `internal/service/acquire/extract/engine.go` | 提取器引擎：按 site code/nickname 选择特殊提取器 |
| `internal/service/acquire/extract/runtime_entry.go` | 提取运行入口、ReviewData 转换、默认引擎构建 |

### `acquire/extract/sites`

每个特殊站点独立一个文件，命名与 Python 版“单站点特殊提取器”思路一致。

| 文件 | 说明 |
|---|---|
| `internal/service/acquire/extract/sites/types.go` | 站点提取运行时输入输出结构 |
| `internal/service/acquire/extract/sites/ssd.go` | SSD 专用提取规则 |
| `internal/service/acquire/extract/sites/audiences.go` | audiences 专用提取规则 |
| `internal/service/acquire/extract/sites/hhanclub.go` | hhanclub 专用提取规则 |
| `internal/service/acquire/extract/sites/keepfrds.go` | keepfrds 专用提取规则 |
| `internal/service/acquire/extract/sites/chdbits.go` | chdbits 专用提取规则 |
| `internal/service/acquire/extract/sites/hdsky.go` | hdsky 专用提取规则 |
| `internal/service/acquire/extract/sites/pterclub.go` | pterclub 专用提取规则 |
| `internal/service/acquire/extract/sites/hddolby.go` | hddolby 专用提取规则 |

### `acquire/fetch`

| 文件 | 说明 |
|---|---|
| `internal/service/acquire/fetch/source_site.go` | 源站点解析与校验 |
| `internal/service/acquire/fetch/torrent_fetch.go` | 详情页下载、候选下载链接解析、torrent 元数据解析 |
| `internal/service/acquire/fetch/torrent_video_size.go` | torrent 视频体积统计（批量转种增强强制过滤） |
| `internal/service/acquire/fetch/fetch_acquire_flow.go` | 抓取阶段总流程（Prepare + Download + Parse） |
| `internal/service/acquire/fetch/fetched_context.go` | 抓取结果上下文补全（关联当前种子信息） |
| `internal/service/acquire/fetch/download_only.go` | 仅下载种子接口逻辑 |
| `internal/service/acquire/fetch/downloaded_torrent.go` | 已下载种子路径定位 |
| `internal/service/acquire/fetch/publish_torrent_path.go` | 发布使用的 torrent 路径解析 |
| `internal/service/acquire/fetch/site_lookup.go` | 通过详情页反查站点 |
| `internal/service/acquire/fetch/log_stream_state.go` | 抓取日志流状态管理 |
| `internal/service/acquire/fetch/batch_fetch_state.go` | 批量抓取任务状态管理 |
| `internal/service/acquire/fetch/batch_fetch_runner.go` | 批量抓取执行器 |
| `internal/service/acquire/fetch/aggregated_torrents.go` | 聚合种子分页查询 |

### `acquire/extract`（解析核心与辅助）

| 文件 | 说明 |
|---|---|
| `internal/service/acquire/extract/review_extract.go` | 从详情页 HTML 提取审核展示字段 |
| `internal/service/acquire/extract/extractor_site_common_helpers.go` | 提取公共站点辅助函数 |
| `internal/service/acquire/extract/extractor_ssd_helpers.go` | SSD 提取辅助函数 |
| `internal/service/acquire/extract/shared_helpers.go` | 公共工具 |
| `internal/service/acquire/extract/exports.go` | 对外导出入口与适配 |

### 5.2 `internal/service/processing`（修复、校验、入库）

### `processing/shared`

| 文件 | 说明 |
|---|---|
| `internal/service/processing/shared/conversion.go` | 字符串/数字/布尔统一转换工具 |
| `internal/service/processing/shared/screenshot_review_status.go` | 截图人工确认状态常量与归一化工具 |

### `processing/repair`

负责“抓取后修复”，包含海报、简介、截图、媒体信息校验等。

| 文件 | 说明 |
|---|---|
| `internal/service/processing/repair/types.go` | 修复输入输出结构 |
| `internal/service/processing/repair/fetch_flow.go` | 并发修复主流程与任务编排 |
| `internal/service/processing/repair/movie_info.go` | 豆瓣/IMDb/TMDb/PTGen 媒体信息抓取与组装 |
| `internal/service/processing/repair/intro_completeness.go` | 简介关键字段完整性检测（片名/产地/简介） |
| `internal/service/processing/repair/media_validate.go` | 媒体文本校验逻辑 |
| `internal/service/processing/repair/media_validate_entry.go` | 媒体校验外部入口 |
| `internal/service/processing/repair/screenshot.go` | 正式截图生成、上传与路径解析 |
| `internal/service/processing/repair/screenshot_preview.go` | 低清候选截图生成与正式截图时间点收敛 |
| `internal/service/processing/repair/images.go` | 图片链接提取与 BBCode 转换 |
| `internal/service/processing/repair/pixhost.go` | Pixhost 上传与链接标准化 |
| `internal/service/processing/repair/http.go` | 修复环节网络请求工具 |
| `internal/service/processing/repair/utils.go` | 文本压缩等辅助函数 |

### `processing/media`

负责 MediaInfo/BDInfo 内容识别、标准化、ISO 本地访问和刷新。

| 文件 | 说明 |
|---|---|
| `internal/service/processing/media/iso_session.go` | 本地媒体访问会话、候选路径解析与 ISO 会话收口 |
| `internal/service/processing/media/iso_session_linux.go` | Linux 本地 / 原生 Docker ISO 自动挂载实现 |
| `internal/service/processing/media/iso_session_windows.go` | Windows 原生 ISO 自动挂载实现（PowerShell） |
| `internal/service/processing/media/parser_pythonish.go` | Python 风格兼容的媒体信息解析 |
| `internal/service/processing/media/validate.go` | 媒体格式判定（MediaInfo/BDInfo） |
| `internal/service/processing/media/bluray_detect.go` | 蓝光原盘目录识别 |
| `internal/service/processing/media/overrides.go` | HDR/音轨信息覆盖规则 |
| `internal/service/processing/media/target.go` | 媒体目标文件定位与提取命令执行 |
| `internal/service/processing/media/refresh_flow.go` | MediaInfo 异步刷新流程 |
| `internal/service/processing/media/timeutil.go` | 时间展示工具 |

### `processing/title`

| 文件 | 说明 |
|---|---|
| `internal/service/processing/title/simple_components.go` | 标题组件基础解析（标题、分辨率、编码、音频等） |
| `internal/service/processing/title/components.go` | 标题组件构建与存储映射 |
| `internal/service/processing/title/parse_flow.go` | 标题解析流程 |
| `internal/service/processing/title/parse_entry.go` | 标题解析入口 |
| `internal/service/processing/title/preview.go` | 标题预览重建 |

### `processing/tagging`

| 文件 | 说明 |
|---|---|
| `internal/service/processing/tagging/completion.go` | 原始标签提取（标题/副标题/媒体文本） |
| `internal/service/processing/tagging/mapping.go` | 标签映射到标准标签 |
| `internal/service/processing/tagging/completion_checker.go` | 完整季/全集判定 |
| `internal/service/processing/tagging/recompute.go` | 标签重算逻辑 |
| `internal/service/processing/tagging/recompute_persist.go` | 标签重算后回写数据库 |

### `processing/persist`

抓取产物标准化后写入 `seed_parameters`，并提供 DB 查询、手动更新、预览更新。

| 文件 | 说明 |
|---|---|
| `internal/service/processing/persist/fetch_store_entry.go` | 抓取入库主入口 |
| `internal/service/processing/persist/fetch_store_process.go` | 抓取结果处理与草稿构建 |
| `internal/service/processing/persist/fetch_persist_pipeline.go` | 入库流水线（标准化 + 持久化） |
| `internal/service/processing/persist/fetch_finalize.go` | 抓取后收尾 |
| `internal/service/processing/persist/fetch_finalize_flow.go` | 收尾流程编排 |
| `internal/service/processing/persist/fetch_finalize_logging.go` | 收尾日志输出 |
| `internal/service/processing/persist/fetch_repair_finalize.go` | 修复后收口处理 |
| `internal/service/processing/persist/fetch_post_persist.go` | 写库后补充动作 |
| `internal/service/processing/persist/fetch_store_outcome.go` | API 返回体与状态码收敛 |
| `internal/service/processing/persist/db_seed_query_entry.go` | DB 查询 API 入口 |
| `internal/service/processing/persist/db_seed_lookup_flow.go` | DB 查询流程 |
| `internal/service/processing/persist/db_seed_query.go` | DB 查询标准化输出 |
| `internal/service/processing/persist/manual_update.go` | 手动编辑参数构建 |
| `internal/service/processing/persist/manual_update_apply.go` | 手动更新入库应用 |
| `internal/service/processing/persist/preview_update.go` | 预览数据更新 |
| `internal/service/processing/persist/publish_params.go` | 发布参数构建、`seedID` 编解码 |
| `internal/service/processing/persist/seed_draft.go` | 抓取草稿结构与组装逻辑 |
| `internal/service/processing/persist/seed_component_rewrite.go` | 根据媒体信息重写标题组件 |
| `internal/service/processing/persist/seed_row_normalize.go` | DB 行标准化 |
| `internal/service/processing/persist/datetime.go` | 时间字段标准化 |

### `processing/bdflow`

负责 BDInfo 全链路：任务启动、执行、状态缓存、回调入库、状态查询。

| 文件 | 说明 |
|---|---|
| `internal/service/processing/bdflow/bdinfo_state.go` | BDInfo 任务内存状态管理 |
| `internal/service/processing/bdflow/bdinfo_task.go` | 任务结构定义 |
| `internal/service/processing/bdflow/bdinfo_start.go` | 启动前参数准备、任务创建入口 |
| `internal/service/processing/bdflow/start_task.go` | 启动任务编排入口 |
| `internal/service/processing/bdflow/bdinfo_launch.go` | 启动并注册任务 |
| `internal/service/processing/bdflow/bdinfo_runtime.go` | 运行态执行与状态联动 |
| `internal/service/processing/bdflow/task_runtime.go` | 任务运行执行器 |
| `internal/service/processing/bdflow/bdinfo_flow.go` | BDInfo 执行主流程 |
| `internal/service/processing/bdflow/bdinfo_tool.go` | BDInfo 二进制调用与目录识别 |
| `internal/service/processing/bdflow/resolve_extract.go` | 路径解析与提取辅助 |
| `internal/service/processing/bdflow/bdinfo_query.go` | BDInfo 状态与记录查询 |
| `internal/service/processing/bdflow/bdinfo_progress_enrich.go` | 响应附加实时进度 |
| `internal/service/processing/bdflow/bdinfo_callbacks.go` | 回调 payload 解析 |
| `internal/service/processing/bdflow/bdinfo_callback_entry.go` | 回调 API 入口处理 |
| `internal/service/processing/bdflow/bdinfo_callback.go` | 回调结果持久化 |
| `internal/service/processing/bdflow/value_helpers.go` | BDInfo 局部类型转换辅助 |

### 5.3 `internal/service/publish`（发布）

### `publish/workflow`

| 文件 | 说明 |
|---|---|
| `internal/service/publish/workflow/publish_entry.go` | 单目标发布执行入口 |
| `internal/service/publish/workflow/publish_payload_entry.go` | 从 payload 直接发布入口 |
| `internal/service/publish/workflow/publish_with_context.go` | 基于上下文发布执行 |
| `internal/service/publish/workflow/publish_target.go` | 发布分派入口（按站点选择 public/special 发布器执行上传） |
| `internal/service/publish/workflow/context_state.go` | 上下文状态存储 |
| `internal/service/publish/workflow/context_build.go` | 上下文对象构建 |
| `internal/service/publish/workflow/context_register.go` | 抓取/DB 结果注册上下文 |
| `internal/service/publish/workflow/preview_update.go` | 发布预览更新 |
| `internal/service/publish/workflow/response_build.go` | 抓取成功响应构建 |
| `internal/service/publish/workflow/migrate_torrent.go` | 一步迁移编排 |
| `internal/service/publish/workflow/batch_state.go` | 批量发布任务状态、订阅、取消 |
| `internal/service/publish/workflow/batch_runner.go` | 批量发布执行器 |
| `internal/service/publish/workflow/batch_execute.go` | 批量发布管理入口 |
| `internal/service/publish/workflow/batch_payload_execute.go` | 基于 payload 的批量发布入口 |

### `publish/publisher`

发布器实现：保持“公共发布器 + 站点特殊发布器”结构，站点差异逻辑收敛到 `sites/` 单文件内，避免散落在 workflow/uploader 中。

| 文件 | 说明 |
|---|---|
| `internal/service/publish/publisher/types.go` | 发布器输入输出结构体 |
| `internal/service/publish/publisher/anonymous.go` | 匿名发布开关读取与公共表单匿名字段注入 |
| `internal/service/publish/publisher/public.go` | 公共发布器（表单 takeupload.php/upload.php） |
| `internal/service/publish/publisher/engine/engine.go` | 发布器路由（按站点选择 public/special 发布器） |
| `internal/service/publish/publisher/sites/helpers.go` | 特殊发布器公共辅助（字符串/布尔转换等） |
| `internal/service/publish/publisher/sites/public_site.go` | 公共表单发布器站点模板底座（步骤覆写：描述/额外字段/表单调整） |
| `internal/service/publish/publisher/sites/cbg.go` | CBG 动画/动漫分类分流发布器 |
| `internal/service/publish/publisher/sites/baozi.go` | Baozi DIY 原盘媒介修正发布器 |
| `internal/service/publish/publisher/sites/pterclub.go` | PTerClub 独立 checkbox 标签发布器 |
| `internal/service/publish/publisher/sites/hddolby.go` | HDDolby TMDb/MediaInfo/截图独立字段发布器 |
| `internal/service/publish/publisher/sites/hdkyl.go` | HDKyl 年份字段映射发布器 |
| `internal/service/publish/publisher/sites/luckpt.go` | LuckPT 英语标签过滤发布器 |
| `internal/service/publish/publisher/sites/ptskit.go` | PTSKit 白名单标签发布器 |
| `internal/service/publish/publisher/sites/crabpt.go` | CrabPT 普通区发布器（固定普通区表单模式） |
| `internal/service/publish/publisher/sites/ptlgs.go` | PTLGS 字段分离发布器（封面/截图独立字段） |
| `internal/service/publish/publisher/sites/hdfans.go` | HDFans 标签/媒介覆盖发布器 |
| `internal/service/publish/publisher/sites/haidan.go` | 海胆特殊发布器（截图独立字段） |
| `internal/service/publish/publisher/sites/zhuque.go` | 朱雀（TNode/API）特殊发布器 |
| `internal/service/publish/publisher/sites/rousi.go` | 肉丝（API v1）特殊发布器 |

### `publish/mapping`

| 文件 | 说明 |
|---|---|
| `internal/service/publish/mapping/site_mapping.go` | 目标站点发布映射配置加载 |
| `internal/service/publish/mapping/basic_fields.go` | 基础字段映射 |

### `publish/checker`

发布后页面校验与权限判断（例如：已存在种子时是否允许自动编辑）。

| 文件 | 说明 |
|---|---|
| `internal/service/publish/checker/existing_edit_permission.go` | 已存在种子自动编辑权限校验入口（按站点页面特征分派） |
| `internal/service/publish/checker/nexusphp_edit_permission.go` | NexusPHP 详情页权限校验（发布者=当前登录用户且存在编辑入口） |
| `internal/service/publish/checker/zhuque_edit_permission.go` | 朱雀（TNode）详情页权限校验（对比发布者与 user-info-side 当前用户） |
| `internal/service/publish/checker/name_normalize.go` | HTML 文本提取与用户名标准化 |

### `publish/uploader`

| 文件 | 说明 |
|---|---|
| `internal/service/publish/uploader/upload_http.go` | 上传 HTTP 请求封装 |
| `internal/service/publish/uploader/helpers.go` | 发布描述文本与链接提取 |
| `internal/service/publish/uploader/published_url.go` | 发布 URL 标准化与直链构建 |
| `internal/service/publish/uploader/path.go` | 种子路径识别 |
| `internal/service/publish/uploader/params_dump.go` | DEV 环境发布参数落盘（写入 `data/tmp/torrents`） |

### `publish/guard`

发布前预检查（例如：下载器做种数限制）。

| 文件 | 说明 |
|---|---|
| `internal/service/publish/guard/seeding_limit.go` | 发布前下载器限制检查（支持远端 guard 与本地降级） |

### `publish/downloader`

| 文件 | 说明 |
|---|---|
| `internal/service/publish/downloader/query.go` | 下载器信息查询 |
| `internal/service/publish/downloader/add.go` | 发布后加种 |

---

## 6. 其他 service 子模块

| 目录 | 说明 |
|---|---|
| `internal/service/crossseed` | Cross-seed 查询与删除等 |
| `internal/service/localquery` | 本地路径扫描、缓存、重复分析 |
| `internal/service/settings` | 设置读写、UI 设置、本地背景图、迁移配置、下载器信息 |

本地背景图落盘目录：`{DataDir}/backgrounds/`；内置默认图：`internal/service/settings/default_backgrounds/`（embed，空目录时种子写入）。
| `internal/service/stats` | 图表、速度、站点/分组统计 |
| `internal/service/torrentdata` | 种子数据检索、刷新、IYUU 查询任务 |
| `internal/service/torrenttransfer` | 转移准备、执行、暂停、导出、补种 |
| `internal/service/tracker` | 下载器流量采集与周期写库 |
| `internal/service/downloaderclient` | 统一下载器客户端 |
| `internal/service/reversemapping` | 反向映射（标准字段回目标站字段） |
| `internal/service/logexport` | 日志聚合导出服务 |

### `internal/service/downloaderclient`

| 文件 | 说明 |
|---|---|
| `internal/service/downloaderclient/client.go` | 下载器通用客户端（qB/Transmission）与通用工具函数 |
| `internal/service/downloaderclient/torrents_fetch.go` | 下载器种子列表拉取与归一化 |
| `internal/service/downloaderclient/proxy_media.go` | 盒子代理：远程提取 MediaInfo（用于本机无法访问媒体文件） |
| `internal/service/downloaderclient/proxy_bdinfo.go` | 盒子代理：提交 BDInfo 任务（异步） |
| `internal/service/downloaderclient/proxy_bdinfo_progress.go` | 盒子代理：轮询 BDInfo 任务进度与结果 |

---

## 7. 当前目录中“保留/空位”说明

| 路径 | 现状 |
|---|---|
| `internal/auth` | 当前未放置独立文件，保留目录位 |
| `internal/http/router` | 当前路由由 `bootstrap/app.go` 直接注册，目录暂为空位 |

---

## 8. 迁移请求实际调用路径（简化）

以 `/api/migrate/fetch_and_store` 为例：

1. `internal/http/handler/migrate/db_seed_publish.go` 接收请求
2. 调用 `internal/service/migrationflow/fetch_store.go`
3. 编排调用 `acquire/fetch` 与 `acquire/extract`
4. 进入 `processing/repair + processing/persist` 完成修复和写库
5. 需要时联动 `processing/media`、`processing/bdflow` 异步流程
6. 返回统一结果并通过 `log_stream_state` 进行日志流推送

---

## 9. 结构设计原则（当前版本）

1. 抓取提取与发布彻底解耦，避免单目录堆积所有逻辑。
2. 提取层统一返回结构，特殊站点仅覆盖提取规则，不影响后续流水线。
3. `migrationflow` 只做编排与状态管理，不重复实现底层算法。
4. 处理链路按功能域拆分（repair/media/title/tagging/persist/bdflow），便于逐段维护。
5. 发布链路按职责拆分（mapping/uploader/downloader/workflow），便于扩展多站点上传策略。
