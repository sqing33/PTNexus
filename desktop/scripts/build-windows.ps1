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
$ToolsDir = Join-Path $RuntimeServer "tools"

$MediaCacheDir = Join-Path $Desktop ".tools/win-installer-cache"
$FfmpegArchive = "ffmpeg-n8.0.1-56-g3201cd40a4-win64-gpl-8.0.zip"
$FfmpegUrl = "https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-02-10-13-08/$FfmpegArchive"
$FfmpegSha256 = "01aebfcdbbcf83d512c1edc960eb62a6ce5f56df2e35fb3643582f78ace5f39f"
$MpvArchive = "mpv-0.41.0-x86_64.7z"
$MpvUrl = "https://downloads.sourceforge.net/project/mpv-player-windows/release/$MpvArchive"
$MpvSha256 = "ef86fde0959d789d77a3ad7c3c2dca51c6999695363f493a6154f2c518634c0f"
$SevenZipExe = Join-Path $MediaCacheDir "7zr.exe"
$SevenZipUrl = "https://www.7-zip.org/a/7zr.exe"
$SevenZipSha256 = "27cbe3d5804ad09e90bbcaa916da0d5c3b0be9462d0e0fb6cb54be5ed9030875"

function Ensure-Dir([string]$Path) {
  if (-not (Test-Path $Path)) {
    New-Item -Path $Path -ItemType Directory | Out-Null
  }
}

function Get-SHA256([string]$Path) {
  return (Get-FileHash -Algorithm SHA256 -Path $Path).Hash.ToLowerInvariant()
}

function Ensure-Download([string]$Url, [string]$OutFile) {
  if (-not (Test-Path $OutFile)) {
    Invoke-WebRequest -Uri $Url -OutFile $OutFile
  }
}

function Prepare-WindowsMediaTools {
  Write-Host "[3.5/7] 准备 Windows 媒体工具..."

  Ensure-Dir $MediaCacheDir
  Ensure-Dir $ToolsDir

  $ffmpegZip = Join-Path $MediaCacheDir $FfmpegArchive
  $mpv7z = Join-Path $MediaCacheDir $MpvArchive

  Ensure-Download $FfmpegUrl $ffmpegZip
  if ((Get-SHA256 $ffmpegZip) -ne $FfmpegSha256) {
    throw "FFmpeg SHA256 校验失败：$ffmpegZip"
  }

  Ensure-Download $MpvUrl $mpv7z
  if ((Get-SHA256 $mpv7z) -ne $MpvSha256) {
    throw "MPV SHA256 校验失败：$mpv7z"
  }

  Ensure-Download $SevenZipUrl $SevenZipExe
  if ((Get-SHA256 $SevenZipExe) -ne $SevenZipSha256) {
    throw "7zr.exe SHA256 校验失败：$SevenZipExe"
  }

  if (Test-Path $ToolsDir) {
    Remove-Item -Recurse -Force $ToolsDir
  }
  Ensure-Dir $ToolsDir

  $extractRoot = Join-Path $MediaCacheDir "extracted-media"
  if (Test-Path $extractRoot) {
    Remove-Item -Recurse -Force $extractRoot
  }
  Ensure-Dir $extractRoot

  $ffmpegExtract = Join-Path $extractRoot "ffmpeg"
  Ensure-Dir $ffmpegExtract
  Expand-Archive -Path $ffmpegZip -DestinationPath $ffmpegExtract -Force

  $ffmpegTop = Get-ChildItem -Path $ffmpegExtract -Directory | Select-Object -First 1
  if (-not $ffmpegTop) {
    throw "FFmpeg 解压结构异常"
  }

  $ffmpegBinSrc = Join-Path $ffmpegTop.FullName "bin"
  if (-not (Test-Path (Join-Path $ffmpegBinSrc "ffmpeg.exe")) -or -not (Test-Path (Join-Path $ffmpegBinSrc "ffprobe.exe"))) {
    throw "FFmpeg bin 目录缺少 ffmpeg.exe 或 ffprobe.exe"
  }

  $ffmpegBinDst = Join-Path $ToolsDir "ffmpeg/bin"
  Ensure-Dir (Join-Path $ToolsDir "ffmpeg")
  Copy-Item -Recurse -Force $ffmpegBinSrc $ffmpegBinDst

  $mpvExtract = Join-Path $extractRoot "mpv"
  Ensure-Dir $mpvExtract
  & $SevenZipExe x $mpv7z "-o$mpvExtract" -y | Out-Null

  $mpvExeSrc = Get-ChildItem -Path $mpvExtract -Filter "mpv.exe" -Recurse | Select-Object -First 1
  if (-not $mpvExeSrc) {
    throw "MPV 解压后未找到 mpv.exe"
  }

  $mpvDstDir = Join-Path $ToolsDir "mpv"
  Ensure-Dir $mpvDstDir
  Copy-Item -Force $mpvExeSrc.FullName (Join-Path $mpvDstDir "mpv.exe")
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

$RuntimeEnvTemplate = Join-Path $Desktop "templates/runtime.env.example"
if (Test-Path $RuntimeEnvTemplate) {
  Copy-Item -Force $RuntimeEnvTemplate (Join-Path $RuntimeData "runtime.env.example")
}

$DbConfigWizard = Join-Path $Desktop "src-tauri/nsis/db-config.ps1"
if (Test-Path $DbConfigWizard) {
  Copy-Item -Force $DbConfigWizard (Join-Path $RuntimeData "db-config.ps1")
}

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

$RuntimeBdinfoRoot = Join-Path $RuntimeServer "core/bdinfo"
if (Test-Path (Join-Path $Root "server/core/bdinfo")) {
  Ensure-Dir (Join-Path $RuntimeServer "core")
  Copy-Item -Recurse -Force (Join-Path $Root "server/core/bdinfo") (Join-Path $RuntimeServer "core")
}

# 仅保留 Windows BDInfo 工具，避免把 Linux 大文件打进 Windows 安装包
if (Test-Path (Join-Path $RuntimeBdinfoRoot "linux")) {
  Remove-Item -Recurse -Force (Join-Path $RuntimeBdinfoRoot "linux")
}

Prepare-WindowsMediaTools

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

  .\.venv\Scripts\pyinstaller.exe `
    --noconfirm `
    --clean `
    --onedir `
    --name background_runner `
    --distpath (Join-Path $RuntimeServer "_dist") `
    --workpath (Join-Path $Desktop ".pyi-work") `
    --specpath (Join-Path $Desktop ".pyi-spec") `
    background_runner.py

  Copy-Item -Recurse -Force (Join-Path $RuntimeServer "_dist/server/*") $RuntimeServer
  Copy-Item -Recurse -Force (Join-Path $RuntimeServer "_dist/background_runner/*") $RuntimeServer
  Remove-Item -Recurse -Force (Join-Path $RuntimeServer "_dist")

  # 防止 PyInstaller 产物再次带入 Linux 版 BDInfo 大文件
  if (Test-Path (Join-Path $RuntimeBdinfoRoot "linux")) {
    Remove-Item -Recurse -Force (Join-Path $RuntimeBdinfoRoot "linux")
  }

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
