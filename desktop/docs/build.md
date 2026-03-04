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

## 后续（第二部分）预期构建流程

1. 构建 `webui` 前端产物（`dist`）。
2. 同步产物到 `desktop` 可 embed 的目录。
3. 执行 `wails build -clean -nsis` 生成 Windows 安装包。

> 说明：当前阶段不改 `webui`，这里只记录后续执行路径。

## 输出产物

1. 开发模式：Wails dev 运行时应用。
2. 生产模式：`desktop/build/bin` 下可分发产物（具体以 Wails 输出为准）。
