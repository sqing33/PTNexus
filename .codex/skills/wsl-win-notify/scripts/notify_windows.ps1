param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("decision", "permission", "blocked", "done")]
    [string]$Event,

    [string]$Title = "",

    [string]$Summary = "",
    [string]$Project = "",

    [string]$NextAction = "Open the Codex output and review the requested action.",
    [string]$Stats = "no diff",
    [string]$TitleB64 = "",
    [string]$SummaryB64 = "",
    [string]$ProjectB64 = "",
    [string]$NextActionB64 = "",
    [string]$StatsB64 = "",
    [string]$AppId = "Snore.DesktopToasts.0.7.0"
)

$ErrorActionPreference = "Stop"

function Decode-Base64Utf8 {
    param([string]$Value)
    if ([string]::IsNullOrEmpty($Value)) {
        return $null
    }
    try {
        $bytes = [System.Convert]::FromBase64String($Value)
        return [System.Text.Encoding]::UTF8.GetString($bytes)
    }
    catch {
        return $null
    }
}

function Escape-XmlText {
    param([string]$Value)
    if ([string]::IsNullOrEmpty($Value)) {
        return ""
    }
    return [System.Security.SecurityElement]::Escape($Value)
}

try {
    $decodedTitle = Decode-Base64Utf8 $TitleB64
    $decodedSummary = Decode-Base64Utf8 $SummaryB64
    $decodedProject = Decode-Base64Utf8 $ProjectB64
    $decodedNextAction = Decode-Base64Utf8 $NextActionB64
    $decodedStats = Decode-Base64Utf8 $StatsB64

    if ($null -ne $decodedTitle) { $Title = $decodedTitle }
    if ($null -ne $decodedSummary) { $Summary = $decodedSummary }
    if ($null -ne $decodedProject) { $Project = $decodedProject }
    if ($null -ne $decodedNextAction) { $NextAction = $decodedNextAction }
    if ($null -ne $decodedStats) { $Stats = $decodedStats }

    if ([string]::IsNullOrWhiteSpace($Title) -or [string]::IsNullOrWhiteSpace($Summary)) {
        throw "Title and Summary are required."
    }
    if ([string]::IsNullOrWhiteSpace($Project)) {
        $Project = "unknown-project"
    }
    if ([string]::IsNullOrWhiteSpace($AppId)) {
        throw "AppId is required."
    }

    [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
    [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] > $null

    $eventUpper = $Event.ToUpperInvariant()
    $toastTitle = Escape-XmlText "[Codex][$eventUpper] $Title"
    $toastSummary = Escape-XmlText $Summary
    $toastProject = Escape-XmlText "Project: $Project"
    $toastStats = Escape-XmlText "Changes: $Stats"
    $toastNext = Escape-XmlText "Next: $NextAction"

    $xmlPayload = @"
<toast>
  <visual>
    <binding template='ToastGeneric'>
      <text>$toastTitle</text>
      <text>$toastSummary</text>
      <text>$toastProject</text>
      <text>$toastStats</text>
      <text>$toastNext</text>
    </binding>
  </visual>
</toast>
"@

    $xmlDoc = New-Object Windows.Data.Xml.Dom.XmlDocument
    $xmlDoc.LoadXml($xmlPayload)

    $toast = [Windows.UI.Notifications.ToastNotification]::new($xmlDoc)
    $toast.Group = "CodexWSLNotify"
    $toast.Tag = $Event

    $notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($AppId)
    $notifier.Show($toast)
    exit 0
}
catch {
    Write-Error $_
    exit 1
}
