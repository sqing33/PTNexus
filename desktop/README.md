# desktop

`desktop` 是 PT Nexus 的桌面壳工程（Wails 官方骨架）。

## 当前阶段

当前仅执行第一部分：只修改 `desktop/`，不改 `server/`、`webui/`、`updater/`。

当前已完成：

1. 保留 Wails 官方骨架结构。
2. 增加桌面路由分流占位层（`internal/desktopapp`）。
3. 增加文档与脚本占位（`docs/`、`scripts/`）。

当前未执行（后续第二部分）：

1. 挂接 `server` 到 `/api/*`。
2. 挂接 `updater` 到 `/update/*`。
3. 接入 `webui` 生产构建产物并实现桌面轮询分支。

## 执行顺序

1. 先阅读 `Plan.md`。
2. 先完成第一部分（仅本目录）。
3. 等其他目录改动窗口可用后，再执行第二部分跨目录接入。

## 常用命令

开发：

```bash
bash scripts/package-desktop.sh desktop-dev
```

构建：

```bash
bash scripts/package-desktop.sh
```

统一入口脚本：

1. `scripts/package-desktop.sh`
