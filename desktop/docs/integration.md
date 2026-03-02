# integration.md

## 目标

说明 `desktop` 后续如何接入项目现有业务模块，同时保持 Docker 路径不受影响。

## 接入边界

第一部分（当前）：

1. 仅在 `desktop` 建立占位路由分层。
2. 不直接 import 其他目录业务代码。

第二部分（后续）：

1. 接入 `server/internal/bootstrap.NewApp()`。
2. 接入 updater 的 handler 注册函数。
3. 把前端来源从 `desktop/frontend` 逐步切换到 `webui` 构建产物。

## 目标请求分流

1. `/api/*` -> `server` Gin Engine（进程内）。
2. `/update/*` -> updater handlers（进程内）。
3. 其他路径 -> Wails 静态资源。

## 轮询与 SSE 策略

1. Docker/Web 继续使用 SSE。
2. Windows/Desktop 使用轮询分支。
3. 新增 `logs/poll` 后由前端在 Desktop 模式调用。

