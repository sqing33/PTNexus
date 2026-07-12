#requires -Version 5.1
<#
.SYNOPSIS
  Windows 本地开发一键启停（对齐 scripts/dev-run.sh 的精简版）

.DESCRIPTION
  默认只起 server + webui（部分功能即可），不依赖 Docker / batch。
  - 无 updater：server 监听 5274，与 webui/vite.config.ts 的 /api 代理一致
  - 有 updater：server 5275 + updater 5274（与生产/desktop 链路一致）

.EXAMPLE
  .\scripts\dev-run.ps1
  .\scripts\dev-run.ps1 up
  .\scripts\dev-run.ps1 down
  .\scripts\dev-run.ps1 status
  .\scripts\dev-run.ps1 -WithUpdater
  .\scripts\dev-run.ps1 -NoWebui
  $env:AUTH_PASSWORD = 'admin123'; .\scripts\dev-run.ps1
#>

[CmdletBinding()]
param(
  [Parameter(Position = 0)]
  [ValidateSet('up', 'start', 'down', 'status')]
  [string]$Action = 'up',

  [int]$ServerPort = 0,
  [int]$UpdaterPort = 5274,
  [int]$WebuiPort = 5173,

  [switch]$WithUpdater,
  [switch]$NoWebui,
  [switch]$SkipDepInstall,

  [string]$AuthPassword = '',
  [string]$AuthUsername = 'admin',
  [string]$DbType = 'sqlite'
)

$ErrorActionPreference = 'Stop'

# PS5.1 + Windows 控制台默认按系统代码页（中文环境是 GB18030）输出，会把中文 banner 打成乱码。
# 脚本本身已经带 UTF-8 BOM（PS5.1 会按 UTF-8 解析源码），这里再把 host 输出编码也设成 UTF-8。
try {
  [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
  $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {}

function Write-Info([string]$Message) { Write-Host "[dev-run] $Message" }
function Write-WarnLine([string]$Message) { Write-Warning $Message }

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir '..')).Path
$ServerDir = Join-Path $RepoRoot 'server'
$WebuiDir = Join-Path $RepoRoot 'webui'
$UpdaterDir = Join-Path $RepoRoot 'updater'

$RunWebui = -not $NoWebui.IsPresent
$RunUpdater = $WithUpdater.IsPresent
$AutoInstall = -not $SkipDepInstall.IsPresent

if ($ServerPort -le 0) {
  if ($RunUpdater) { $ServerPort = 5275 } else { $ServerPort = 5274 }
}

$RuntimeDir = Join-Path $env:TEMP "ptnexus-dev-run"
$LogDir = Join-Path $RuntimeDir 'logs'
$PidDir = Join-Path $RuntimeDir 'pid'
New-Item -ItemType Directory -Force -Path $LogDir, $PidDir | Out-Null

$ServerPidFile = Join-Path $PidDir "server.$ServerPort.pid"
$UpdaterPidFile = Join-Path $PidDir "updater.$UpdaterPort.pid"
$WebuiPidFile = Join-Path $PidDir "webui.$WebuiPort.pid"

$ServerLog = Join-Path $LogDir "server.$ServerPort.log"
$UpdaterLog = Join-Path $LogDir "updater.$UpdaterPort.log"
$WebuiLog = Join-Path $LogDir "webui.$WebuiPort.log"

$UpdaterBin = Join-Path $RuntimeDir 'ptnexus-updater.exe'

# air 热重载（对齐 scripts/dev-run.sh）：优先用 server/.air.toml；Windows 下覆盖 tmp_dir / bin 到 %TEMP%
$AirConfigFile = Join-Path $ServerDir '.air.toml'
# air 在 Windows 上默认把 tmp_dir / bin 写到 /tmp/...；改用 server 下的 tmp/，跨平台一致
$AirTmp = Join-Path $ServerDir 'tmp/ptnexus-air'
$AirBin = Join-Path $ServerDir 'tmp/ptnexus-server.air.exe'
$ServerAirMode = $false  # 启动后置 true：Stop-KnownConflicts / Stop-PidFile 区分 air / go run

function Test-HttpOk {
  param(
    [Parameter(Mandatory = $true)][string]$Url,
    [int]$TimeoutSec = 2
  )
  try {
    $resp = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec $TimeoutSec
    return ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 500)
  } catch {
    return $false
  }
}

function Test-TcpOpen {
  param(
    [string]$HostName = '127.0.0.1',
    [Parameter(Mandatory = $true)][int]$Port,
    [int]$TimeoutMs = 800
  )
  $client = New-Object System.Net.Sockets.TcpClient
  try {
    $iar = $client.BeginConnect($HostName, $Port, $null, $null)
    if (-not $iar.AsyncWaitHandle.WaitOne($TimeoutMs, $false)) {
      return $false
    }
    $client.EndConnect($iar)
    return $true
  } catch {
    return $false
  } finally {
    $client.Close()
  }
}

function Wait-HttpOk {
  param(
    [Parameter(Mandatory = $true)][string]$Url,
    [int]$TimeoutSec = 20
  )
  $deadline = (Get-Date).AddSeconds($TimeoutSec)
  while ((Get-Date) -lt $deadline) {
    if (Test-HttpOk -Url $Url) { return $true }
    Start-Sleep -Milliseconds 200
  }
  return $false
}

function Get-PidFromFile([string]$PidFile) {
  if (-not (Test-Path $PidFile)) { return $null }
  $raw = (Get-Content $PidFile -ErrorAction SilentlyContinue | Select-Object -First 1)
  if (-not $raw) { return $null }
  $text = $raw.ToString().Trim()
  if ($text -notmatch '^\d+$') { return $null }
  return [int]$text
}

function Stop-PidFile {
  param(
    [Parameter(Mandatory = $true)][string]$PidFile,
    [Parameter(Mandatory = $true)][string]$Label
  )
  $procId = Get-PidFromFile $PidFile
  if (Test-Path $PidFile) {
    Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
  }
  if (-not $procId) { return }

  $proc = Get-Process -Id $procId -ErrorAction SilentlyContinue
  if (-not $proc) { return }

  Write-Info "stop: $Label pid=$procId"
  try {
    Stop-Process -Id $procId -Force -ErrorAction Stop
  } catch {
    Write-WarnLine "failed to stop $Label pid=$procId : $($_.Exception.Message)"
  }

  # go run 会再拉子进程，顺带清掉同命令行的残留
  # air 模式下，pidfile 是 air；air 启动后 fork 出 ptnexus-server.air.exe，
  # 因此子进程过滤也要命中它，否则端口不会释放
  $airChildPattern = 'ptnexus-server\.air(\.exe)?'
  Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
    Where-Object {
      $_.ParentProcessId -eq $procId -or
      ($_.CommandLine -and ($_.CommandLine -match 'cmd[/\\]server')) -or
      ($_.CommandLine -and ($_.CommandLine -match $airChildPattern)) -or
      ($_.CommandLine -and ($_.CommandLine -match 'vite'))
    } |
    ForEach-Object {
      try { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue } catch {}
    }
}

function Assert-CommandExists([string]$Name, [string]$Hint) {
  if (Get-Command $Name -ErrorAction SilentlyContinue) { return }
  throw "$Name not found. $Hint"
}

function Ensure-BackendDeps {
  Assert-CommandExists 'go' 'Install Go, then reopen the terminal.'
  if (-not $AutoInstall) { return }
  Write-Info 'deps: server go mod download'
  Push-Location $ServerDir
  try { & go mod download } finally { Pop-Location }
  if ($RunUpdater) {
    Write-Info 'deps: updater go mod download'
    Push-Location $UpdaterDir
    try { & go mod download } finally { Pop-Location }
  }
}

function Ensure-WebuiDeps {
  if (-not $RunWebui) { return }
  if (-not (Test-Path $WebuiDir)) {
    throw "webui dir missing: $WebuiDir"
  }
  Assert-CommandExists 'pnpm' 'Install pnpm (corepack enable; corepack prepare pnpm@10.0.0 --activate) or use npm i -g pnpm.'

  $viteBin = Join-Path $WebuiDir 'node_modules\.bin\vite.cmd'
  if (Test-Path $viteBin) { return }

  if (-not $AutoInstall) {
    throw "webui deps missing. Run: cd `"$WebuiDir`"; pnpm install"
  }

  Write-Info "deps: webui pnpm install ($WebuiDir)"
  Push-Location $WebuiDir
  try { & pnpm install } finally { Pop-Location }

  if (-not (Test-Path $viteBin)) {
    throw "webui deps install failed: $viteBin still missing"
  }
}

function Stop-ProcessTreeSafe([int]$ProcId, [string]$Label) {
  if ($ProcId -le 0) { return }
  $proc = Get-Process -Id $ProcId -ErrorAction SilentlyContinue
  if (-not $proc) { return }
  Write-Info "kill conflict: $Label pid=$ProcId"
  try {
    Stop-Process -Id $ProcId -Force -ErrorAction Stop
  } catch {
    Write-WarnLine "failed to stop $Label pid=$ProcId : $($_.Exception.Message)"
  }
}

function ConvertTo-WinPathLiteral([string]$Path) {
  if ([string]::IsNullOrEmpty($Path)) { return '' }
  return $Path.Replace('/', '\')
}

function Escape-RegexLiteral([string]$Text) {
  if ($null -eq $Text) { return '' }
  return [System.Text.RegularExpressions.Regex]::Escape($Text)
}

function Stop-KnownConflicts {
  # 对齐 dev-run.sh stop_known_conflicts：只清理本仓库路径下的开发进程
  $serverNorm = Escape-RegexLiteral (ConvertTo-WinPathLiteral $ServerDir)
  $webuiNorm = Escape-RegexLiteral (ConvertTo-WinPathLiteral $WebuiDir)
  $updaterNorm = Escape-RegexLiteral (ConvertTo-WinPathLiteral $UpdaterDir)
  $repoNorm = Escape-RegexLiteral (ConvertTo-WinPathLiteral $RepoRoot)
  $updaterBinNorm = Escape-RegexLiteral (ConvertTo-WinPathLiteral $UpdaterBin)

  Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | ForEach-Object {
    $cmd = [string]$_.CommandLine
    $cwd = [string]$_.ExecutablePath
    if (-not $cmd) { return }

    $cmdSlash = ConvertTo-WinPathLiteral $cmd
    $isServer =
      ($cmd -match 'go(\.exe)?\s+run\s+\./cmd/server' -or $cmd -match 'cmd\\server' -or $cmd -match 'cmd/server') -and
      ($cmdSlash -match $serverNorm -or $cmdSlash -match $repoNorm)
    $isAir =
      ($cmd -match '(^|[\\/])air(\.exe)?(\s|$)' -or $cmd -match '\.air\.toml') -and
      ($cmdSlash -match $serverNorm)
    # air 启动后的 server 子进程：bin 文件名特征 ptnexus-server.air(.exe)
    $isAirServer =
      $cmd -match 'ptnexus-server\.air(\.exe)?' -and
      ($cmdSlash -match (Escape-RegexLiteral (ConvertTo-WinPathLiteral $AirBin)))
    $isUpdater =
      ($cmdSlash -match 'ptnexus-updater\.exe' -or ($updaterBinNorm -and $cmdSlash -match $updaterBinNorm)) -or
      (($cmd -match '(^|[\\s"])\./updater(\.exe)?(\s|$)' -or $cmd -match 'go(\.exe)?\s+run\s+\.') -and ($cmdSlash -match $updaterNorm))
    $isWebui =
      $RunWebui -and (
        ($cmd -match 'vite' -or $cmd -match 'pnpm(\.cmd)?\s+run\s+dev' -or $cmd -match 'pnpm(\.cmd)?\s+dev') -and
        ($cmdSlash -match $webuiNorm -or $cmdSlash -match $repoNorm)
      )

    if ($isServer) {
      Stop-ProcessTreeSafe -ProcId ([int]$_.ProcessId) -Label 'server'
      return
    }
    if ($isAir) {
      Stop-ProcessTreeSafe -ProcId ([int]$_.ProcessId) -Label 'server(air)'
      return
    }
    if ($isAirServer) {
      Stop-ProcessTreeSafe -ProcId ([int]$_.ProcessId) -Label 'server(air-bin)'
      return
    }
    if ($isUpdater) {
      Stop-ProcessTreeSafe -ProcId ([int]$_.ProcessId) -Label 'updater'
      return
    }
    if ($isWebui) {
      Stop-ProcessTreeSafe -ProcId ([int]$_.ProcessId) -Label 'webui'
    }
  }

  # 再按监听端口兜底（与 down 一致；Windows 上 go-build 临时二进制命令行常不含仓库路径）
  $ports = @($ServerPort)
  if ($RunUpdater) { $ports += $UpdaterPort }
  if ($RunWebui) { $ports += $WebuiPort }
  foreach ($port in ($ports | Select-Object -Unique)) {
    try {
      $conns = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
      foreach ($c in $conns) {
        if (-not $c.OwningProcess) { continue }
        Stop-ProcessTreeSafe -ProcId ([int]$c.OwningProcess) -Label "listener:$port"
      }
    } catch {
      # Get-NetTCPConnection 在部分环境不可用
    }
  }

  Start-Sleep -Milliseconds 600
}

function Ensure-PortFree {
  param(
    [Parameter(Mandatory = $true)][string]$Url,
    [Parameter(Mandatory = $true)][string]$Label,
    [Parameter(Mandatory = $true)][int]$Port
  )

  $busy = (Test-HttpOk -Url $Url) -or (Test-TcpOpen -Port $Port)
  if ($busy) {
    Write-Info "port check: $Label busy on :$Port, trying to stop known conflicts"
    # 先按 pidfile / 本脚本上次记录停一轮，再扫已知开发进程
    switch ($Label) {
      'server' { Stop-PidFile -PidFile $ServerPidFile -Label 'server' }
      'updater' { Stop-PidFile -PidFile $UpdaterPidFile -Label 'updater' }
      'webui' { Stop-PidFile -PidFile $WebuiPidFile -Label 'webui' }
    }
    Stop-KnownConflicts
  }

  if (Test-HttpOk -Url $Url) {
    throw "port conflict: $Label already responding at $Url (run: .\scripts\dev-run.ps1 down)"
  }
  if (Test-TcpOpen -Port $Port) {
    throw "port conflict: $Label has TCP listener on 127.0.0.1:$Port (run: .\scripts\dev-run.ps1 down)"
  }
}

function Start-LoggedProcess {
  param(
    [Parameter(Mandatory = $true)][string]$FilePath,
    [string[]]$ArgumentList = @(),
    [Parameter(Mandatory = $true)][string]$WorkingDirectory,
    [Parameter(Mandatory = $true)][string]$LogPath,
    [Parameter(Mandatory = $true)][string]$PidFile,
    [hashtable]$EnvMap = @{}
  )

  if (Test-Path $LogPath) { Remove-Item $LogPath -Force -ErrorAction SilentlyContinue }

  # 用 cmd 重定向，兼容没有 RedirectStandard* 权限的场景
  $argLine = ($ArgumentList | ForEach-Object {
      if ($_ -match '[\s"]') { '"' + ($_ -replace '"', '\"') + '"' } else { $_ }
    }) -join ' '

  $envPrefix = ($EnvMap.GetEnumerator() | ForEach-Object {
      $k = $_.Key
      $v = [string]$_.Value
      $vEsc = $v -replace '"', '\"'
      "set `"$k=$vEsc`" && "
    }) -join ''

  $cmd = "cd /d `"$WorkingDirectory`" && $envPrefix`"$FilePath`" $argLine >> `"$LogPath`" 2>&1"
  $proc = Start-Process -FilePath 'cmd.exe' `
    -ArgumentList @('/c', $cmd) `
    -WorkingDirectory $WorkingDirectory `
    -WindowStyle Hidden `
    -PassThru

  Set-Content -Path $PidFile -Value $proc.Id -Encoding ascii
  return $proc
}

function Start-Server {
  # 决定后端运行模式：air（热重载）→ go run（兜底）
  $airCmd = $null
  if (Test-Path $AirConfigFile) {
    $airCmd = Resolve-AirCmd
  }

  if ($airCmd) {
    Write-Info "start: server :$ServerPort (hot reload: air, log: $ServerLog)"
    $ServerAirMode = $true

    # air 默认 tmp_dir/bin 写 /tmp/...；Windows 上不存在。用 CLI 覆盖到 %TEMP%
    [void](Start-LoggedProcess `
        -FilePath $airCmd `
        -ArgumentList @('-c', $AirConfigFile) `
        -WorkingDirectory $ServerDir `
        -LogPath $ServerLog `
        -PidFile $ServerPidFile `
        -EnvMap (Get-ServerEnvMap))
  }
  else {
    if (-not (Test-Path $AirConfigFile)) {
      Write-Info "start: server :$ServerPort (go run; air config missing: $AirConfigFile)"
    } else {
      Write-Info "start: server :$ServerPort (go run; air not found)"
    }
    $ServerAirMode = $false

    [void](Start-LoggedProcess `
        -FilePath 'go' `
        -ArgumentList @('run', './cmd/server') `
        -WorkingDirectory $ServerDir `
        -LogPath $ServerLog `
        -PidFile $ServerPidFile `
        -EnvMap (Get-ServerEnvMap))
  }

  $health = "http://127.0.0.1:$ServerPort/health"
  if (-not (Wait-HttpOk -Url $health -TimeoutSec 45)) {
    Write-Host '----- server log (tail) -----'
    if (Test-Path $ServerLog) { Get-Content $ServerLog -Tail 80 }
    throw "server failed to become healthy at $health"
  }
}

function Get-ServerEnvMap {
  $envMap = @{
    DEV_ENV          = 'true'
    SERVER_PORT      = "$ServerPort"
    SERVER_HOST      = '0.0.0.0'
    DB_TYPE          = $DbType
    PTNEXUS_BASE_DIR = $ServerDir
    PTNEXUS_DATA_DIR = (Join-Path $ServerDir 'data')
    PTNEXUS_LOG_DIR  = (Join-Path $ServerDir 'data\logs')
  }
  if ($AuthUsername) { $envMap['AUTH_USERNAME'] = $AuthUsername }
  if ($AuthPassword) { $envMap['AUTH_PASSWORD'] = $AuthPassword }
  # 透传调用方已有环境（例如手动 export 的 JWT）
  foreach ($key in @('AUTH_PASSWORD', 'AUTH_PASSWORD_HASH', 'JWT_SECRET', 'UPLOAD_TEST_MODE')) {
    $existing = [Environment]::GetEnvironmentVariable($key, 'Process')
    if ($existing -and -not $envMap.ContainsKey($key)) {
      $envMap[$key] = $existing
    }
  }
  return $envMap
}

function Resolve-AirCmd {
  # 对齐 scripts/dev-run.sh resolve_air_cmd：PATH → GOPATH/bin → go install
  $existing = Get-Command 'air' -ErrorAction SilentlyContinue
  if ($existing) {
    return $existing.Path
  }

  $gopath = (& go env GOPATH 2>$null)
  if ($gopath) {
    $candidate = Join-Path $gopath 'bin\air.exe'
    if (Test-Path $candidate) { return $candidate }
  }

  if (-not $AutoInstall) { return $null }

  Write-Info 'deps: air missing, running go install github.com/air-verse/air@latest'
  Push-Location $ServerDir
  try {
    & go install github.com/air-verse/air@latest
    if ($LASTEXITCODE -ne 0) {
      Write-WarnLine "go install air failed exit=$LASTEXITCODE"
      return $null
    }
  } finally {
    Pop-Location
  }

  $existing = Get-Command 'air' -ErrorAction SilentlyContinue
  if ($existing) { return $existing.Path }
  if ($gopath) {
    $candidate = Join-Path $gopath 'bin\air.exe'
    if (Test-Path $candidate) { return $candidate }
  }
  return $null
}

function Start-UpdaterProc {
  if (-not $RunUpdater) { return }

  Write-Info "build: updater -> $UpdaterBin"
  Push-Location $UpdaterDir
  try {
    & go build -o $UpdaterBin .
    if ($LASTEXITCODE -ne 0) { throw "go build updater failed exit=$LASTEXITCODE" }
  } finally {
    Pop-Location
  }

  Write-Info "start: updater :$UpdaterPort -> server :$ServerPort (log: $UpdaterLog)"
  $envMap = @{
    UPDATER_PORT = "$UpdaterPort"
    SERVER_PORT  = "$ServerPort"
    BATCH_PORT   = '5276'
  }

  [void](Start-LoggedProcess `
      -FilePath $UpdaterBin `
      -ArgumentList @() `
      -WorkingDirectory $RepoRoot `
      -LogPath $UpdaterLog `
      -PidFile $UpdaterPidFile `
      -EnvMap $envMap)

  $health = "http://127.0.0.1:$UpdaterPort/health"
  if (-not (Wait-HttpOk -Url $health -TimeoutSec 20)) {
    Write-Host '----- updater log (tail) -----'
    if (Test-Path $UpdaterLog) { Get-Content $UpdaterLog -Tail 80 }
    throw "updater failed to become healthy at $health"
  }
}

function Start-WebuiProc {
  if (-not $RunWebui) { return }
  Ensure-WebuiDeps

  Write-Info "start: webui :$WebuiPort (log: $WebuiLog)"
  # pnpm 会把 "run <script> -- <args>" 的第一个 "--" 吃掉再转给 vite；
  # 这里不要再多传一层 "--"，否则 vite 会收到字面量 "--" 并忽略后续 --host，
  # 最终只监听 localhost，导致对 127.0.0.1 的健康检查失败。
  [void](Start-LoggedProcess `
      -FilePath 'pnpm' `
      -ArgumentList @('run', 'dev', '--host', '127.0.0.1', '--port', "$WebuiPort", '--strictPort') `
      -WorkingDirectory $WebuiDir `
      -LogPath $WebuiLog `
      -PidFile $WebuiPidFile)

  $url = "http://127.0.0.1:$WebuiPort/"
  if (-not (Wait-HttpOk -Url $url -TimeoutSec 40)) {
    Write-Host '----- webui log (tail) -----'
    if (Test-Path $WebuiLog) { Get-Content $WebuiLog -Tail 80 }
    throw "webui failed to become ready at $url"
  }
}

function Show-Ready {
  Write-Host ''
  Write-Info 'ready:'
  if ($RunWebui) {
    Write-Host "  webui:  http://127.0.0.1:$WebuiPort"
  }
  if ($RunUpdater) {
    Write-Host "  updater: http://127.0.0.1:$UpdaterPort"
  }
  Write-Host "  server: http://127.0.0.1:$ServerPort"
  Write-Host "  logs:   $LogDir"
  Write-Host "  data:   $(Join-Path $ServerDir 'data')"
  if (-not $AuthPassword) {
    Write-Host '  login:  admin + 首次启动临时密码见 server 日志 (password=...)'
  } else {
    Write-Host "  login:  $AuthUsername / (你传入的 AUTH_PASSWORD)"
  }
  if (-not $RunUpdater) {
    Write-Host '  note:   未启动 updater；server 已绑定 5274 以匹配 vite /api 代理'
  }
  Write-Host ''
  Write-Info 'stop with: .\scripts\dev-run.ps1 down'
}

function Invoke-Down {
  if ($RunWebui) { Stop-PidFile -PidFile $WebuiPidFile -Label 'webui' }
  if ($RunUpdater) { Stop-PidFile -PidFile $UpdaterPidFile -Label 'updater' }
  Stop-PidFile -PidFile $ServerPidFile -Label 'server'

  # 兜底：按端口杀本机监听（仅 127.0.0.1/0.0.0.0 常见开发端口）
  foreach ($port in @($WebuiPort, $UpdaterPort, $ServerPort) | Select-Object -Unique) {
    try {
      $conns = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
      foreach ($c in $conns) {
        if ($c.OwningProcess) {
          Write-Info "stop: leftover listener port=$port pid=$($c.OwningProcess)"
          Stop-Process -Id $c.OwningProcess -Force -ErrorAction SilentlyContinue
        }
      }
    } catch {
      # Get-NetTCPConnection 在部分环境不可用，忽略
    }
  }
}

function Invoke-Status {
  Write-Info 'health:'
  if ($RunWebui) {
    if (Test-HttpOk "http://127.0.0.1:$WebuiPort/") { Write-Host '  webui: up' } else { Write-Host '  webui: down' }
  }
  if ($RunUpdater) {
    if (Test-HttpOk "http://127.0.0.1:$UpdaterPort/health") { Write-Host '  updater: up' } else { Write-Host '  updater: down' }
  }
  if (Test-HttpOk "http://127.0.0.1:$ServerPort/health") { Write-Host '  server: up' } else { Write-Host '  server: down' }
}

# --- main ---
switch ($Action) {
  'down' {
    Invoke-Down
    Write-Info 'stopped'
    exit 0
  }
  'status' {
    Invoke-Status
    exit 0
  }
  { $_ -in @('up', 'start') } {
    if (-not (Test-Path (Join-Path $ServerDir 'cmd\server\main.go'))) {
      throw "server entry missing under $ServerDir"
    }

    Ensure-PortFree -Url "http://127.0.0.1:$ServerPort/health" -Label 'server' -Port $ServerPort
    if ($RunUpdater) {
      Ensure-PortFree -Url "http://127.0.0.1:$UpdaterPort/health" -Label 'updater' -Port $UpdaterPort
    }
    if ($RunWebui) {
      Ensure-PortFree -Url "http://127.0.0.1:$WebuiPort/" -Label 'webui' -Port $WebuiPort
    }

    Ensure-BackendDeps
    Start-Server
    Start-UpdaterProc
    Start-WebuiProc
    Show-Ready
    exit 0
  }
}