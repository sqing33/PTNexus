param(
  [ValidateSet("x64")]
  [string]$Arch = "x64",

  [switch]$SkipWeb,
  [switch]$SkipGo,
  [switch]$SkipPython,
  [switch]$SkipTauri
)

$ErrorActionPreference = "Stop"

$Desktop = Split-Path -Parent $PSScriptRoot
$Root = Split-Path -Parent $Desktop
$RuntimeRoot = Join-Path $Desktop "runtime"
$RuntimeServer = Join-Path $RuntimeRoot "server"
$RuntimeBatch = Join-Path $RuntimeRoot "batch"
$RuntimeUpdater = Join-Path $RuntimeRoot "updater"
$RuntimeData = Join-Path $RuntimeRoot "data"

function Ensure-Dir([string]$Path) {
  if (-not (Test-Path $Path)) {
    New-Item -Path $Path -ItemType Directory | Out-Null
  }
}

Write-Host "[1/7] 准备 runtime 目录..."
if (Test-Path $RuntimeRoot) {
  Remove-Item -Recurse -Force $RuntimeRoot
}
Ensure-Dir $RuntimeServer
Ensure-Dir $RuntimeBatch
Ensure-Dir $RuntimeUpdater
Ensure-Dir $RuntimeData
Ensure-Dir (Join-Path $RuntimeData "tmp")

if (-not $SkipWeb) {
  Write-Host "[2/7] 构建 webui..."
  Push-Location (Join-Path $Root "webui")
  bun run build
  Pop-Location

  Copy-Item -Recurse -Force (Join-Path $Root "webui/dist") (Join-Path $RuntimeServer "dist")
}

Write-Host "[3/7] 复制 server 资源..."
Copy-Item -Force (Join-Path $Root "server/sites_data.json") (Join-Path $RuntimeServer "sites_data.json")
Copy-Item -Recurse -Force (Join-Path $Root "server/configs") (Join-Path $RuntimeServer "configs")
if (Test-Path (Join-Path $Root "server/core/bdinfo")) {
  Copy-Item -Recurse -Force (Join-Path $Root "server/core/bdinfo") (Join-Path $RuntimeServer "bdinfo")
}

if (-not $SkipGo) {
  Write-Host "[4/7] 构建 Go 二进制..."

  Push-Location (Join-Path $Root "batch")
  $env:CGO_ENABLED = "0"
  $env:GOOS = "windows"
  $env:GOARCH = "amd64"
  go build -ldflags="-s -w" -o (Join-Path $RuntimeBatch "batch.exe") batch.go
  Pop-Location

  Push-Location (Join-Path $Root "updater")
  $env:CGO_ENABLED = "0"
  $env:GOOS = "windows"
  $env:GOARCH = "amd64"
  go build -ldflags="-s -w" -o (Join-Path $RuntimeUpdater "updater.exe") updater.go
  Pop-Location

  Remove-Item Env:GOOS -ErrorAction SilentlyContinue
  Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
}

if (-not $SkipPython) {
  Write-Host "[5/7] 使用 PyInstaller 打包 server..."
  Push-Location (Join-Path $Root "server")

  if (-not (Test-Path ".venv/Scripts/python.exe")) {
    throw "未检测到 server/.venv，请先准备 Python 虚拟环境。"
  }

  .\.venv\Scripts\python.exe -m pip install pyinstaller
  .\.venv\Scripts\pyinstaller.exe `
    --noconfirm `
    --clean `
    --onedir `
    --name server `
    --distpath (Join-Path $RuntimeServer "_dist") `
    --workpath (Join-Path $Desktop ".pyi-work") `
    --specpath (Join-Path $Desktop ".pyi-spec") `
    app.py

  Copy-Item -Recurse -Force (Join-Path $RuntimeServer "_dist/server/*") $RuntimeServer
  Remove-Item -Recurse -Force (Join-Path $RuntimeServer "_dist")

  Pop-Location
}

Write-Host "[6/7] 拷贝版本文件..."
Copy-Item -Force (Join-Path $Root "CHANGELOG.json") (Join-Path $Desktop "CHANGELOG.json")

Write-Host "[6.5/7] 安装 desktop 依赖..."
Push-Location $Desktop
npm install
Pop-Location

if (-not $SkipTauri) {
  Write-Host "[7/7] 构建 Tauri NSIS 安装包..."
  Push-Location $Desktop
  npm run build:win:x64:installer
  Pop-Location
}

Write-Host "完成。若未跳过 Tauri，安装包在 desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis。"
