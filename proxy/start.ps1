param(
    [string]$Port = "9090"
)

$ErrorActionPreference = "Stop"

$BaseDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RuntimeDir = Join-Path $BaseDir "runtime"
$LogDir = Join-Path $RuntimeDir "logs"
$PidDir = Join-Path $RuntimeDir "pid"
$StdoutLog = Join-Path $LogDir "pt-nexus-box-proxy.stdout.log"
$StderrLog = Join-Path $LogDir "pt-nexus-box-proxy.stderr.log"
$PidFile = Join-Path $PidDir "pt-nexus-box-proxy.pid"
$ExePath = Join-Path $BaseDir "pt-nexus-box-proxy.exe"
$BundledBDInfo = Join-Path $BaseDir "bdinfo\\windows\\BDInfo.exe"
$BundledSubstractor = Join-Path $BaseDir "bdinfo\\windows\\BDInfoDataSubstractor.exe"

New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
New-Item -ItemType Directory -Force -Path $PidDir | Out-Null

if (-not (Test-Path $ExePath)) {
    Write-Error "Executable not found: $ExePath"
}

if (Test-Path $PidFile) {
    $ExistingPid = (Get-Content $PidFile -ErrorAction SilentlyContinue | Select-Object -First 1).Trim()
    if ($ExistingPid) {
        $ExistingProcess = Get-Process -Id $ExistingPid -ErrorAction SilentlyContinue
        if ($ExistingProcess) {
            Write-Output "Proxy is already running. PID: $ExistingPid"
            exit 1
        }
    }
    Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
}

$env:PTNEXUS_BASE_DIR = $BaseDir
$env:PTNEXUS_DATA_DIR = $RuntimeDir

$missingDeps = @()
foreach ($tool in @("mediainfo", "ffmpeg", "ffprobe", "mpv")) {
    switch ($tool) {
        "mediainfo" { $envKey = "PTNEXUS_MEDIAINFO_PATH" }
        "ffmpeg" { $envKey = "PTNEXUS_FFMPEG_PATH" }
        "ffprobe" { $envKey = "PTNEXUS_FFPROBE_PATH" }
        "mpv" { $envKey = "PTNEXUS_MPV_PATH" }
    }

    $configuredPath = [Environment]::GetEnvironmentVariable($envKey)
    if ($configuredPath) {
        if (-not (Test-Path $configuredPath)) {
            $missingDeps += "$tool ($envKey points to a missing file)"
        }
        continue
    }

    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        $missingDeps += $tool
    }
}

if (-not (Test-Path $BundledBDInfo)) {
    $missingDeps += "BDInfo.exe (expected at bdinfo\\windows\\BDInfo.exe)"
}
if (-not (Test-Path $BundledSubstractor)) {
    $missingDeps += "BDInfoDataSubstractor.exe (expected at bdinfo\\windows\\BDInfoDataSubstractor.exe)"
}

if ($missingDeps.Count -gt 0) {
    Write-Warning ("Missing optional runtime dependencies: " + ($missingDeps -join ", "))
}

$process = Start-Process -FilePath $ExePath `
    -ArgumentList @($Port) `
    -WorkingDirectory $BaseDir `
    -RedirectStandardOutput $StdoutLog `
    -RedirectStandardError $StderrLog `
    -PassThru `
    -WindowStyle Hidden

Set-Content -Path $PidFile -Value $process.Id -Encoding ascii

Write-Output "PT Nexus Proxy started."
Write-Output "PID: $($process.Id)"
Write-Output "Port: $Port"
Write-Output "Stdout log: $StdoutLog"
Write-Output "Stderr log: $StderrLog"
Write-Output "PID file: $PidFile"
