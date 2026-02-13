$ErrorActionPreference = "Stop"

$DesktopRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$RepoRoot = (Resolve-Path (Join-Path $DesktopRoot "..")).Path
$ChangelogPath = Join-Path $RepoRoot "CHANGELOG.json"
Set-Location $DesktopRoot

if (!(Test-Path $ChangelogPath)) {
  throw "CHANGELOG.json not found: $ChangelogPath"
}

$Changelog = Get-Content -Raw -Encoding UTF8 $ChangelogPath | ConvertFrom-Json
if ($null -eq $Changelog.history -or $Changelog.history.Count -eq 0) {
  throw "empty history in CHANGELOG.json"
}
$VersionRaw = [string]$Changelog.history[0].version
if ([string]::IsNullOrWhiteSpace($VersionRaw)) {
  throw "missing latest version in CHANGELOG.json"
}
$VersionRaw = $VersionRaw.Trim()
$VersionSafe = [regex]::Replace($VersionRaw, '[^A-Za-z0-9._-]', '')
if ([string]::IsNullOrWhiteSpace($VersionSafe)) {
  throw "invalid version parsed from CHANGELOG.json: $VersionRaw"
}

wails build -platform windows/amd64 -nsis -clean -v 2

$DefaultInstaller = Join-Path $DesktopRoot "build\\bin\\pt-nexus-amd64-installer.exe"
$VersionedInstaller = Join-Path $DesktopRoot ("build\\bin\\pt-nexus-{0}-amd64-installer.exe" -f $VersionSafe)
if (Test-Path $DefaultInstaller) {
  Move-Item -Path $DefaultInstaller -Destination $VersionedInstaller -Force
  Write-Host "Versioned installer:"
  Write-Host "  $VersionedInstaller"
}
else {
  Write-Warning "Installer not found at $DefaultInstaller"
}

Write-Host "Done. Outputs:"
Write-Host "  $DesktopRoot\\build\\bin\\pt-nexus.exe"
Write-Host "  $DesktopRoot\\build\\bin\\pt-nexus-$VersionSafe-amd64-installer.exe"
