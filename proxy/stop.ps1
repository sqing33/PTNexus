$ErrorActionPreference = "Stop"

$BaseDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$PidFile = Join-Path $BaseDir "runtime\\pid\\pt-nexus-box-proxy.pid"

if (-not (Test-Path $PidFile)) {
    Write-Output "PID file not found: $PidFile"
    exit 1
}

$Pid = (Get-Content $PidFile -ErrorAction SilentlyContinue | Select-Object -First 1).Trim()
if (-not $Pid) {
    Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
    Write-Output "PID file is empty and has been removed."
    exit 1
}

$Process = Get-Process -Id $Pid -ErrorAction SilentlyContinue
if (-not $Process) {
    Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
    Write-Output "Process is no longer running; PID file has been removed."
    exit 1
}

Stop-Process -Id $Pid -Force
Remove-Item $PidFile -Force -ErrorAction SilentlyContinue

Write-Output "PT Nexus Proxy stopped. PID: $Pid"
