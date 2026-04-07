param(
    [string]$GoExe = "C:\Program Files\Go\bin\go.exe"
)

$ErrorActionPreference = "Stop"

$BaseDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ExePath = Join-Path $BaseDir "pt-nexus-box-proxy.exe"
$RuntimeLogDir = Join-Path $BaseDir "runtime\logs"
$RuntimePidDir = Join-Path $BaseDir "runtime\pid"
$BundledFiles = @(
    (Join-Path $BaseDir "bdinfo\windows\BDInfo.exe"),
    (Join-Path $BaseDir "bdinfo\windows\BDInfoDataSubstractor.exe"),
    (Join-Path $BaseDir "bdinfo\windows\lzfse.dll")
)

if (-not (Test-Path $GoExe)) {
    throw "Go executable not found: $GoExe"
}

Push-Location $BaseDir
try {
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"

    & $GoExe build -o $ExePath .

    New-Item -ItemType Directory -Force -Path $RuntimeLogDir | Out-Null
    New-Item -ItemType Directory -Force -Path $RuntimePidDir | Out-Null

    $missing = @()
    foreach ($path in $BundledFiles) {
        if (-not (Test-Path $path)) {
            $missing += $path
        }
    }

    Write-Output "Windows proxy package is ready."
    Write-Output "Executable: $ExePath"
    Write-Output "Runtime logs dir: $RuntimeLogDir"
    Write-Output "Runtime pid dir: $RuntimePidDir"

    if ($missing.Count -gt 0) {
        Write-Warning ("Missing bundled BDInfo files: " + ($missing -join ", "))
    }
}
finally {
    Pop-Location
}
