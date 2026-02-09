# PT Nexus Desktop (Tauri)

这个目录是 PT Nexus 的桌面壳工程，负责：

1. 拉起 `server` / `batch` / `updater` 三个 sidecar。
2. 启动后自动打开 `http://127.0.0.1:5274`。
3. 退出时回收后端子进程。

## 推荐构建方式（Linux 一键）

在仓库根目录执行：

```bash
bash ./desktop/scripts/build-windows-installer-linux.sh
```

或在 `desktop/` 下执行：

```bash
bun run build:win:x64:installer:linux
```

该流程会自动完成：

- 构建 `webui/dist`
- 交叉编译 `batch.exe` / `updater.exe`
- 同步 `server` 源码到 `desktop/runtime/server`
- 下载 Windows Python Embed
- 下载并解压 Windows wheels 到 `runtime/server/python/Lib/site-packages`
- 自动准备 NSIS（无 sudo）
- 生成 Windows 单文件安装包

输出安装包：

`desktop/src-tauri/target/x86_64-pc-windows-gnu/release/bundle/nsis/PT Nexus_0.1.0_x64-setup.exe`


构建成功后，安装包通常在 **60MB+**（包含 Python 运行时与依赖）。
如果体积只有几 MB，基本可判定为缺少 runtime 资源。

## 运行目录布局

新安装包会在安装结束后自动把目录平铺为：

- `<安装目录>/server`
- `<安装目录>/batch`
- `<安装目录>/updater`

内部仍兼容 `_up_/runtime` 旧布局（若存在会自动回退）。

## 运行时环境变量配置（数据库等）

桌面版默认使用 SQLite（无需手动配置）。

如需切换 MySQL / PostgreSQL：

1. 先启动一次应用（会自动创建用户数据目录）
2. 在用户数据目录创建 `runtime.env`，可参考同目录的 `runtime.env.example`
3. 重启应用

`runtime.env` 中可用变量示例：

- `DB_TYPE=sqlite|mysql|postgresql`
- `MYSQL_HOST` / `MYSQL_PORT` / `MYSQL_USER` / `MYSQL_PASSWORD` / `MYSQL_DATABASE`
- `POSTGRES_HOST` / `POSTGRES_PORT` / `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DATABASE`

## 其他命令

### 仅编译 Windows exe（不打安装包）

```bash
cd desktop
bun run build:win:x64
```

### Windows 主机打 NSIS（官方推荐）

```bash
cd desktop
bun run build:win:x64:installer
```


调试提示：若启动白屏后退出，请查看用户数据目录下 `logs/server.stderr.log`、`logs/batch.stderr.log`、`logs/updater.stderr.log`。
