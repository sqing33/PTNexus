# PT Nexus 在线更新：产物更新（Artifact Update）规范

本规范用于替代“git 拉源码 + 覆盖文件”的在线更新方式，面向**已编译二进制**部署形态（含容器）。

## 1. updater 行为约定

updater 保持 `/update/check`、`/update/pull`、`/update/install` 协议不变，在线更新主链路为：

- `/update/check`：基于 Release 资产中的 `UPDATE_MANIFEST.json` 计算版本、强更与禁更状态。
- `/update/pull`：按平台下载构建产物（artifact），校验 SHA256，解压到 staging。
- `/update/install`：切换到新版本目录并重启服务，健康检查失败时自动回滚。

说明：

- 线上 updater 默认只信任 GitHub/Gitee Release 里的 `UPDATE_MANIFEST.json`；仓库根目录同名文件仅用于仓库内同步版本历史与下载地址占位。
- 在线更新链路不依赖 git clone/pull。
- updater 会并行探测 GitHub/Gitee 的 Release 元数据地址，自动选择当前更新模式可用的源。

## 2. UPDATE_MANIFEST.json 格式

当前实现使用如下结构：

```json
{
  "schema": 2,
  "latest": {
    "version": "v3.6.4",
    "date": "2026.02.21",
    "force_update": false,
    "disable_update": false,
    "note": "可选说明",
    "artifacts": [
      {
        "os": "linux",
        "arch": "amd64",
        "url": "https://example.com/releases/v3.6.4/ptnexus-runtime-linux-amd64.tar.gz",
        "mirror_urls": [
          "https://mirror.example.com/releases/v3.6.4/ptnexus-runtime-linux-amd64.tar.gz"
        ],
        "sha256": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
        "size": 12345678,
        "format": "tar.gz"
      }
    ]
  },
  "history": [
    {
      "version": "v3.6.4",
      "date": "2026.02.21",
      "force_update": false,
      "disable_update": false,
      "note": "可选说明",
      "changes": [
        "变更项"
      ]
    }
  ]
}
```

字段约束：

- `schema`、`latest.version`、`latest.artifacts`、`history` 必填。
- `history[0].version` 必须与 `latest.version` 一致。
- `artifacts[*].url` 与 `artifacts[*].mirror_urls` 至少要有一个可用下载地址。
- `artifacts[*].sha256` 必填（除非显式开启跳过校验）。
- `format` 支持 `tar.gz` 与 `zip`（默认按文件名推断）。

## 3. 产物压缩包内容约定

压缩包必须包含顶层目录：

- `server/`：运行时目录内容（至少含 `server` 二进制，可包含 `dist/`、`configs/`、`sites_data.json`）。

强制约束：

- **不得**打入运行时数据目录（如 `server/data/`）。
- 产物必须是 `tar.gz` 或 `zip`。

## 4. 安装与回滚模型（Phase 1）

以 `PTNEXUS_BASE_DIR=/app/server`、`UPDATE_DIR=/app/data/updates` 为例：

- 下载与解压阶段：
  - `downloads/<version>/...`
  - `staging/<version>/server`
- 安装阶段：
  - 版本目录：`releases/<version>/server`
  - 当前指针：`current -> releases/<version>/server`
  - `PTNEXUS_BASE_DIR` 作为稳定入口，指向 `current`

安装流程：

1. 停止 server 进程（优先 `supervisorctl`）。
2. 将 staging 产物移动到 `releases/<version>/server`。
3. 原子切换 `current` 指针到新版本。
4. 启动 server 并检查 `http://127.0.0.1:<SERVER_PORT>/health`。
5. 失败则回切 `current` 到旧版本并拉起服务。

## 5. 常用环境变量

- `UPDATE_USE_PROXY`：是否使用系统代理访问更新源。
- `UPDATE_SKIP_VERIFY`：是否允许跳过 SHA256 校验（默认 `false`）。
- `UPDATE_DOWNLOAD_TIMEOUT`：下载超时，默认 `20m`。
- `UPDATE_DIR`：更新工作目录，默认 `/app/data/updates`。
- `PTNEXUS_BASE_DIR`：运行时基目录，默认 `/app/server`。
- `SUPERVISOR_CONF`：supervisor 配置路径，默认 `/app/supervisord.conf`。
- `SUPERVISOR_SERVER_NAME`：受控服务名，默认 `server`。

## 6. 发布流程建议

1. 只维护 `CHANGELOG.json`：更新 `history[0]` 与顶层 `artifact_sources`。
2. 构建前端：`cd webui && pnpm run build`。
3. 构建 server 二进制（按目标架构）。
4. 执行 `scripts/build-update-artifacts.sh` 自动生成产物与包含完整 `history` 的 `UPDATE_MANIFEST.json`。
5. 上传 `dist/updates/<version>/` 下产物到 Release/对象存储。
6. 将最终 `UPDATE_MANIFEST.json` 发布到 GitHub/Gitee 对应路径。
