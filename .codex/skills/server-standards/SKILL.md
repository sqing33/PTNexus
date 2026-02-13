---
name: server-go-standards
description: server-go 修改强制工作流与规范（先读结构文档定位；新文件按分层落目录；业务日志用 logx；导出函数/Handler/Service/Repo 写中文注释；交付自检清单）。
---

# PT Nexus Go 后端注释、日志与目录分层规范（server-go-standards）

## 1. 目标

本 Skill 用于所有涉及 `server-go/` 的问题定位与代码修改，确保：

- 每个函数都能快速看懂用途与边界。
- 运行日志在控制台与文件中都可追踪，便于定位与复现。
- 目录分层与文件拆分有统一约束，避免逻辑回退到“单目录堆叠”。
- 查找问题前先理解项目结构（先结构，后定位）。

## 2. 触发范围（何时必须启用）

满足任一条件即视为 `server-go` 任务，强制按本 Skill 执行：

1. 需求中出现 `server-go/` 路径或明确提到 Go 后端
2. 对话过程中探索/打开/计划修改到任意 `server-go/**` 文件

## 3. 强制工作流（先结构，后定位）

1. 先打开并检索 `server-go/docs/PROJECT_STRUCTURE.md`，用业务关键词定位到对应分层与目录
2. 明确“应该在哪一层改”（handler / migrationflow / acquire / processing / publish / repository）
3. 再开始在代码中搜索与打开文件（避免全仓库盲搜导致跨层实现）

## 4. 目录分层与依赖方向（硬性约束）

### 4.1 分层职责边界（硬性约束）

- `internal/http/handler`：只做参数解析、调用 Service、返回 HTTP 响应；禁止承载核心业务算法。
- `internal/service/migrationflow`：只做编排、状态管理、入口收敛；禁止堆放底层抓取解析/修复实现细节。
- `internal/service/acquire`：只负责“获取与提取”（站点抓取、提取器路由、详情页解析）。
- `internal/service/processing`：只负责“修复、校验、标准化、入库”。
- `internal/service/publish`：只负责“发布、映射、上传、下载器操作”。
- `internal/repository`：只负责数据库读写；禁止反向依赖 HTTP Handler。

### 4.2 依赖方向与禁止项

依赖方向必须保持单向：

`handler -> migrationflow -> acquire/processing/publish -> repository`

- 禁止 `repository -> service` 反向依赖
- 禁止 `processing` 直接依赖 `handler`

## 5. 新文件落点与提取器约束（硬性约束）

### 5.1 新文件落点（硬性约束）

新增/移动文件必须落在下列目录之一：

| 分层 | 目录 |
|---|---|
| HTTP Handler | `server-go/internal/http/handler/**` |
| 编排入口（只做编排） | `server-go/internal/service/migrationflow/**` |
| 获取与提取 | `server-go/internal/service/acquire/**` |
| 修复/校验/标准化/入库 | `server-go/internal/service/processing/**` |
| 发布/映射/上传/下载器 | `server-go/internal/service/publish/**` |
| 数据读写 | `server-go/internal/repository/**` |

### 5.2 提取器目录约束（硬性约束）

必须保持“公共提取器 + 站点特殊提取器”模式：

- 公共提取器：`server-go/internal/service/acquire/extractors/public.go`（及同目录下公共引擎文件）
- 站点特殊提取器：`server-go/internal/service/acquire/extractors/sites/`

站点特殊提取器必须满足：

- 一站点一个文件
- 文件名使用小写站点 code：`{site}.go`（示例：`ssd.go`、`hdsky.go`）
- 返回统一结构，保证后续 `processing` 可无差别处理

新增特殊站点时，必须同时更新：

- `server-go/internal/service/acquire/extractors/site_adapters.go`（注册/适配）
- `server-go/docs/PROJECT_STRUCTURE.md`（结构文档同步）

“抖音”默认站点 code 为 `douyin`，因此默认文件名为 `douyin.go`；若仓库实际站点 code 不同，以实际为准。

### 5.3 文件拆分规则（硬性约束）

- 单文件只能承载一个主职责；发现“抓取 + 修复 + 发布”混杂时必须拆分。
- 入口文件与实现细节分离：
  - 入口建议：`*_entry.go` / `*_flow.go`
  - 状态管理建议：`*_state.go`
  - 类型定义建议：`types.go`
- 禁止新增“过渡桥接层”长期保留：
  - 迁移期间允许短期桥接
  - 调用方切换完成后必须清理桥接代码，避免双路径并存
- 禁止把多个站点特殊逻辑写回同一个“通用大文件”。

### 5.4 命名与可读性规则（建议约束）

- 文件名优先表达业务语义，避免 `misc.go` / `tmp.go` / `new_utils.go` 这类模糊命名。
- 包内工具函数应就近放置，避免建立跨业务“大杂烩 helpers”。
- 同一流程模块下文件命名风格保持一致（如 `fetch_*`、`bdinfo_*`）。
- 当函数数量持续增长时，优先按“流程阶段”拆文件，而不是按“作者/时间”拆文件。

### 5.5 变更落地要求（硬性约束）

- 涉及目录结构或职责边界变动时，必须同步更新：`server-go/docs/PROJECT_STRUCTURE.md`。
- 新模块/新分层引入前必须自检：分层职责、跨层反向依赖、桥接/兼容层是否应清理。

## 6. 函数注释规范（全中文）

### 6.1 必写范围

- 所有导出函数（大写开头）
- 所有 HTTP Handler 方法
- 所有 Service 核心流程方法
- 所有 Repository 数据写入/查询方法
- 所有异步任务入口与关键工具函数

### 6.2 注释模板

每个函数注释建议至少覆盖以下 4 点（可按函数复杂度裁剪）：

1. 功能：函数负责什么业务
2. 参数/返回：关键输入与输出含义
3. 失败场景：何时返回错误
4. 副作用：是否写 DB/文件/发请求/启动 goroutine

### 6.3 禁止项

- 禁止写“重复代码本身”的无效注释。
- 禁止写无法验证的“将来可能”描述。
- 禁止中英文混杂导致术语不一致（业务语义优先中文）。

## 7. 日志打印规范（全中文）

### 7.1 统一日志入口（必须）

必须使用 `server-go/internal/platform/logx` 提供的接口：

- `logx.Debugf(module, format, args...)`
- `logx.Infof(module, format, args...)`
- `logx.Warnf(module, format, args...)`
- `logx.Errorf(module, format, args...)`

`module` 必须使用固定中文模块名（如：媒体校验-PTGen、BDInfo、访问日志），便于检索。

禁止直接使用 `log.Printf` / `fmt.Printf` 打业务日志。

### 7.2 示例（完整示例）

```go
import "github.com/pt-nexus/server-go/internal/platform/logx"

const module = "媒体校验-PTGen"

logx.Infof(module, "开始请求 PTGen requestID=%s flowID=%s candidates=%d", requestID, flowID, len(candidates))
logx.Warnf(module, "PTGen 请求失败 requestID=%s flowID=%s err=%v", requestID, flowID, err)
logx.Errorf(module, "流程终止 requestID=%s flowID=%s reason=%s", requestID, flowID, reason)
```

### 7.3 统一等级

- Debug：仅调试细节，默认不建议线上开启。
- Info：流程关键节点、成功状态。
- Warn：可恢复异常、回退路径。
- Error：失败或中断。

### 7.4 日志字段建议

- 请求 ID（`X-Request-ID`）
- 用户标识（登录用户或内部调用）
- 功能模块（如：日志导出、BDInfo、访问日志）
- 关键业务参数（避免敏感信息）

### 7.5 敏感信息约束

- 不打印密码明文。
- 不打印完整 Token/Cookie。
- 不打印用户隐私数据原文。

## 8. 交付自检清单

### 8.1 必须项（每次交付必须逐条确认）

1. 已先查阅 `server-go/docs/PROJECT_STRUCTURE.md` 并据此定位分层/目录
2. 新增/修改文件落在正确目录，且依赖方向未被破坏
3. 新增导出函数/Handler/Service/Repository/异步入口/关键工具函数已按 4 点模板补全中文注释
4. 未出现注释禁止项（无效注释/将来可能/中英混杂）
5. 业务日志仅使用 `logx`，模块名为固定中文，等级使用合理
6. 日志未输出敏感信息（密码/完整 Token/Cookie/隐私原文）
7. 若新增站点特殊提取器：已更新 `server-go/internal/service/acquire/extractors/site_adapters.go` 与 `server-go/docs/PROJECT_STRUCTURE.md`
8. **测试验证**：修改完成后必须先跑测试再交付；至少执行一次 `cd server-go && go test ./...`（如有明确影响范围，可补充更聚焦的包级测试命令，但不可跳过全量测试）
9. **测试文件清理（自动执行）**：为保持 `server-go/` 代码库整洁，功能修改验证通过后（`go test ./...` 通过），必须自动清理 `server-go/` 下所有 `*_test.go` 测试文件。

   - 预览将被删除的文件：
     - `find server-go -type f -name '*_test.go' -print`
   - 一键删除（建议在仓库根目录执行）：
     - `find server-go -type f -name '*_test.go' -print -delete`

### 8.2 建议项（尽量做到）

- 复杂流程至少打印“开始 / 关键分支 / 结束”三类日志。
- 失败时日志包含错误原因与上下文（任务 ID、路径、目标站点等）。
- 同一流程日志文案使用固定关键词，便于全文检索。
- 大功能上线前先给出目录设计草图（输入、处理、输出三段）再落代码。
- 对迁移链路新增功能优先复用三阶段目录，不回退到扁平化堆叠。
