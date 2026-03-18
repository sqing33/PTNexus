# build.md

## 目标

记录 `desktop` 的开发与打包流程，以及后续与 `webui` 的构建协作关系。

## 当前（第一部分）构建方式

开发：

```bash
bash scripts/package-desktop.sh desktop-dev
```

打包：

```bash
bash scripts/package-desktop.sh
```

增量缓存会默认启用，重复执行会按阶段跳过未变化的前端、sidecar 和安装包打包步骤。
如需强制全量重建：

```bash
PTNEXUS_PACKAGE_FORCE=1 bash scripts/package-desktop.sh
```

## 后续（第二部分）预期构建流程

1. 构建 `webui` 前端产物（`dist`）。
2. 同步产物到 `desktop` 可 embed 的目录。
3. 复用已同步到 `desktop/frontend/dist` 与 `desktop/build/windows/sidecar` 的产物。
4. 执行 `wails build -clean -nsis -s -skipbindings` 生成 Windows 安装包。
5. 若 update 安装包输入未变化则跳过，否则执行单独的 NSIS update 安装包构建。

> 说明：当前阶段不改 `webui`，这里只记录后续执行路径。

## 输出产物

1. 开发模式：Wails dev 运行时应用。
2. 生产模式：`desktop/build/bin` 下可分发产物（具体以 Wails 输出为准）。
