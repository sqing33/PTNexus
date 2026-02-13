# env.md

## 桌面运行相关环境变量（预置模板）

以下变量在后续第二部分接入时使用，当前第一部分先记录规范。

1. `PTNEXUS_BASE_DIR`
   - 含义：服务基础目录。
   - Windows 建议：`%APPDATA%/PT-Nexus`
2. `PTNEXUS_DATA_DIR`
   - 含义：运行时数据目录。
   - Windows 建议：`%APPDATA%/PT-Nexus/data`
3. `PTNEXUS_STATIC_DIR`
   - 含义：静态资源目录（如需要）。
4. `VITE_DESKTOP`
   - 含义：前端运行模式标记。
   - 桌面构建建议值：`1`

## Windows 默认路径策略

1. 首次启动检查 `%APPDATA%/PT-Nexus`。
2. 若目录不存在则创建：
   - `data/`
   - `data/tmp/`
   - 必要配置文件默认模板

## 备注

当前第一部分不改外部目录，仅保留变量规范文档，后续第二部分再落地实际注入逻辑。

