Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0

. "$PSScriptRoot/_lib.ps1"

$script:InvocationPwd = (Get-Location).Path
$script:EntryPath = Join-Path $PSScriptRoot 'parafork.ps1'
$script:EntryCmd = ParaforkPsFileCmd $script:EntryPath @()
$script:ConfigPath = Join-Path (Split-Path -Parent $PSScriptRoot) 'settings/config.toml'
$script:CliArgs = @($args)

$script:LastInitRoot = $null
$script:GuardWorktreeId = $null
$script:GuardWorktreeRoot = $null
$script:GuardBaseRoot = $null
$script:GuardSymbolPath = $null

function CfgStr {
  param([string]$Section, [string]$Key, [string]$Default)
  ParaforkTomlGetStr $script:ConfigPath $Section $Key $Default
}

function CfgBool {
  param([string]$Section, [string]$Key, [string]$Default)
  ParaforkTomlGetBool $script:ConfigPath $Section $Key $Default
}

function BaseBranch { CfgStr 'base' 'branch' 'autodetect' }
function WorkdirRoot { CfgStr 'workdir' 'root' '.parafork' }
function WorkdirRule { CfgStr 'workdir' 'rule' '{YYMMDD}-{HEX4}' }
function AutoplanEnabled { CfgBool 'custom' 'autoplan' 'false' }
function AutoformatEnabled { CfgBool 'custom' 'autoformat' 'true' }
function SquashEnabled { CfgBool 'control' 'squash' 'true' }

function UsageMain {
@"
Parafork proposed (minimal)
Usage: $script:EntryCmd [cmd] [args...]
Commands:
  help [debug|--debug]
  init [--new|--reuse] [--yes] [--i-am-maintainer]
  do <exec|commit>
  check [status|merge]
  merge [--yes] [--i-am-maintainer]
Default: no args => init --new + do exec
"@
}

function UsageInit { "Usage: $script:EntryCmd init [--new|--reuse] [--yes] [--i-am-maintainer]" }
function UsageCheck { "Usage: $script:EntryCmd check [status|merge] [--strict]" }
function UsageDo { "Usage: $script:EntryCmd do <exec|commit>" }
function UsageDoExec { "Usage: $script:EntryCmd do exec [--strict]" }
function UsageDoCommit { "Usage: $script:EntryCmd do commit --message `"<msg>`" [--no-check]" }
function UsageMerge {
@"
Usage: $script:EntryCmd merge [--yes] [--i-am-maintainer]
CLI gate: --yes --i-am-maintainer
"@
}

function WorktreeContainer {
  param([string]$BaseRoot)
  Join-Path $BaseRoot (WorkdirRoot)
}

function ListWorktreesNewestFirst {
  param([string]$BaseRoot)

  $container = WorktreeContainer $BaseRoot
  if (-not (Test-Path -LiteralPath $container -PathType Container)) {
    return @()
  }

  $roots = @()
  $children = Get-ChildItem -LiteralPath $container -Directory -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending
  foreach ($child in $children) {
    $candidate = $child.FullName
    if (Test-Path -LiteralPath (Join-Path $candidate '.worktree-symbol') -PathType Leaf) {
      $roots += $candidate
    }
  }
  return $roots
}

function ExpandWorktreeRule {
  param([string]$Rule)
  $yymmdd = (Get-Date).ToUniversalTime().ToString('yyMMdd')
  $hex4 = ([System.Guid]::NewGuid().ToString('N').Substring(0, 4)).ToUpperInvariant()
  return $Rule.Replace('{YYMMDD}', $yymmdd).Replace('{HEX4}', $hex4)
}

function ResolveInitBaseBranch {
  param([string]$BaseRoot)

  $configured = BaseBranch
  $current = (& git -C $BaseRoot rev-parse --abbrev-ref HEAD 2>$null | Select-Object -First 1)
  if ($null -ne $current) { $current = $current.Trim() }

  if ([string]::IsNullOrEmpty($current)) {
    ParaforkDie 'cannot detect current branch from base repo'
  }

  if ([string]::IsNullOrEmpty($configured) -or $configured -eq 'autodetect') {
    if ($current -eq 'HEAD') {
      ParaforkDie 'autodetect requires a branch checkout (detached HEAD is not supported)'
    }
    return $current
  }

  if ($configured -ne $current) {
    if ($current -eq 'HEAD') {
      ParaforkWarn ("config base.branch={0} differs from detached HEAD; keep configured branch" -f $configured)
      return $configured
    }

    $interactive = $false
    try {
      $interactive = -not [Console]::IsInputRedirected
    } catch {
      $interactive = $false
    }

    if (-not $interactive) {
      ParaforkWarn ("config base.branch={0} differs from current branch={1}; non-interactive mode defaults to current branch" -f $configured, $current)
      return $current
    }

    try {
      $answer = Read-Host ("WARN: config base.branch={0} differs from current branch={1}. Use current branch for init --new? [Y/n]" -f $configured, $current)
      if (-not [string]::IsNullOrWhiteSpace($answer) -and $answer.Trim() -notmatch '^(?i:y|yes)$') {
        return $configured
      }
      return $current
    } catch {
      ParaforkWarn ("config base.branch={0} differs from current branch={1}; failed to read prompt, default to current branch" -f $configured, $current)
      return $current
    }
  }

  return $configured
}

function AppendUniqueLine {
  param([string]$Path, [string]$Line)

  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    ParaforkWriteTextUtf8NoBom $Path ""
  }

  foreach ($l in [System.IO.File]::ReadAllLines($Path)) {
    if ($l -eq $Line) {
      return
    }
  }

  ParaforkAppendTextUtf8NoBom $Path ($Line + "`n")
}

function GuardConflictState {
  param([string]$RepoRoot, [string]$WorktreeId, [string]$PwdNow)

  $gitDir = ParaforkGitPathAbs $RepoRoot '.'
  if ([string]::IsNullOrEmpty($gitDir)) {
    return $true
  }

  $isConflict =
    (Test-Path -LiteralPath (Join-Path $gitDir 'MERGE_HEAD') -PathType Leaf) -or
    (Test-Path -LiteralPath (Join-Path $gitDir 'CHERRY_PICK_HEAD') -PathType Leaf) -or
    (Test-Path -LiteralPath (Join-Path $gitDir 'rebase-apply') -PathType Container) -or
    (Test-Path -LiteralPath (Join-Path $gitDir 'rebase-merge') -PathType Container)

  if ($isConflict) {
    Write-Output 'REFUSED: repository in conflict state (merge/rebase/cherry-pick)'
    ParaforkPrintOutputBlock $WorktreeId $PwdNow 'FAIL' 'diagnose conflicts and request human approval before continuing'
    return $false
  }

  return $true
}

function GuardWorktree {
  $pwdNow = (Get-Location).Path
  $script:GuardWorktreeId = $null
  $script:GuardWorktreeRoot = $null
  $script:GuardBaseRoot = $null
  $script:GuardSymbolPath = $null

  $symbolPath = ParaforkSymbolFindUpwards $pwdNow
  if (-not $symbolPath) {
    $baseRoot = ParaforkGitToplevel
    if ($baseRoot) {
      ParaforkPrintOutputBlock 'UNKNOWN' $pwdNow 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('help', '--debug'))
    } else {
      ParaforkPrintOutputBlock 'UNKNOWN' $pwdNow 'FAIL' ('cd <BASE_ROOT>; ' + (ParaforkPsFileCmd $script:EntryPath @('init', '--new')))
    }
    return $false
  }

  $pfWt = ParaforkSymbolGet $symbolPath 'PARAFORK_WORKTREE'
  if ($pfWt -ne '1') {
    ParaforkPrintOutputBlock 'UNKNOWN' $pwdNow 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('help', '--debug'))
    return $false
  }

  $worktreeId = ParaforkSymbolGet $symbolPath 'WORKTREE_ID'
  if ([string]::IsNullOrEmpty($worktreeId)) { $worktreeId = 'UNKNOWN' }

  $worktreeRoot = ParaforkSymbolGet $symbolPath 'WORKTREE_ROOT'
  if ([string]::IsNullOrEmpty($worktreeRoot)) {
    ParaforkPrintOutputBlock $worktreeId $pwdNow 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('help', '--debug'))
    return $false
  }

  $used = ParaforkSymbolGet $symbolPath 'WORKTREE_USED'
  if ($used -ne '1') {
    Write-Output 'REFUSED: worktree not entered (WORKTREE_USED!=1)'
    ParaforkPrintOutputBlock $worktreeId $pwdNow 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('init', '--reuse', '--yes', '--i-am-maintainer'))
    return $false
  }

  $lock = ParaforkSymbolGet $symbolPath 'WORKTREE_LOCK'
  $owner = ParaforkSymbolGet $symbolPath 'WORKTREE_LOCK_OWNER'
  $agentId = ParaforkAgentId

  if ($lock -ne '1' -or [string]::IsNullOrEmpty($owner)) {
    ParaforkWriteWorktreeLock $symbolPath
    $owner = $agentId
  }

  if ($owner -ne $agentId) {
    $safeNext = ParaforkPsFileCmd $script:EntryPath @('init', '--new')
    $takeoverNext = 'cd ' + (ParaforkQuotePs $worktreeRoot) + '; ' + (ParaforkPsFileCmd $script:EntryPath @('init', '--reuse', '--yes', '--i-am-maintainer'))

    Write-Output 'REFUSED: worktree locked by another agent'
    ParaforkPrintKv 'LOCK_OWNER' $owner
    ParaforkPrintKv 'AGENT_ID' $agentId
    ParaforkPrintKv 'SAFE_NEXT' $safeNext
    ParaforkPrintKv 'TAKEOVER_NEXT' $takeoverNext
    ParaforkPrintOutputBlock $worktreeId $pwdNow 'FAIL' $safeNext
    return $false
  }

  $script:GuardWorktreeId = $worktreeId
  $script:GuardWorktreeRoot = $worktreeRoot
  $script:GuardBaseRoot = ParaforkSymbolGet $symbolPath 'BASE_ROOT'
  $script:GuardSymbolPath = $symbolPath
  return $true
}

function CheckFiles {
  param([string]$Phase, [bool]$Strict)

  $pwdNow = (Get-Location).Path
  $symbolPath = Join-Path $pwdNow '.worktree-symbol'
  $worktreeRoot = ParaforkSymbolGet $symbolPath 'WORKTREE_ROOT'
  if ([string]::IsNullOrEmpty($worktreeRoot)) { $worktreeRoot = $pwdNow }

  $autoplan = AutoplanEnabled
  $autoformat = AutoformatEnabled
  if ($Strict) {
    $autoplan = 'true'
    $autoformat = 'true'
  }

  $planFile = Join-Path $worktreeRoot 'paradoc/Plan.md'
  $execFile = Join-Path $worktreeRoot 'paradoc/Exec.md'
  $mergeFile = Join-Path $worktreeRoot 'paradoc/Merge.md'
  $logFile = Join-Path $worktreeRoot 'paradoc/Log.txt'

  $errors = [System.Collections.Generic.List[string]]::new()
  if (-not (Test-Path -LiteralPath $execFile -PathType Leaf)) { $errors.Add("missing file: $execFile") }
  if (-not (Test-Path -LiteralPath $mergeFile -PathType Leaf)) { $errors.Add("missing file: $mergeFile") }
  if (-not (Test-Path -LiteralPath $logFile -PathType Leaf)) { $errors.Add("missing file: $logFile") }
  if ($autoplan -eq 'true' -and -not (Test-Path -LiteralPath $planFile -PathType Leaf)) { $errors.Add("missing file: $planFile") }

  if ($autoplan -eq 'true' -and $autoformat -eq 'true' -and (Test-Path -LiteralPath $planFile -PathType Leaf)) {
    $planText = Get-Content -Raw -LiteralPath $planFile
    if ($planText -notmatch [regex]::Escape('## Milestones')) { $errors.Add('Plan.md missing heading: ## Milestones') }
    if ($planText -notmatch [regex]::Escape('## Tasks')) { $errors.Add('Plan.md missing heading: ## Tasks') }
    if ($planText -notmatch '^- \[.\] ') { $errors.Add('Plan.md has no checkboxes') }
  }

  if ($autoformat -eq 'true' -and (Test-Path -LiteralPath $mergeFile -PathType Leaf)) {
    $mergeText = Get-Content -Raw -LiteralPath $mergeFile
    if ($mergeText -notmatch 'Acceptance|Repro') {
      $errors.Add('Merge.md missing Acceptance/Repro section keywords')
    }
  }

  if ($Phase -eq 'merge' -or $Strict) {
    foreach ($f in @($execFile, $mergeFile)) {
      if (Test-Path -LiteralPath $f -PathType Leaf) {
        $txt = Get-Content -Raw -LiteralPath $f
        if ($txt -match 'PARAFORK_TBD|TODO_TBD') {
          $errors.Add("placeholder remains: $f")
        }
      }
    }
    if ($autoplan -eq 'true' -and (Test-Path -LiteralPath $planFile -PathType Leaf)) {
      $planTxt = Get-Content -Raw -LiteralPath $planFile
      if ($planTxt -match 'PARAFORK_TBD|TODO_TBD') {
        $errors.Add("placeholder remains: $planFile")
      }
    }
  }

  if ($Phase -eq 'merge') {
    $trackedParadoc = & git ls-files -- 'paradoc/' 2>$null
    if ($trackedParadoc) { $errors.Add('git pollution: tracked files under paradoc/') }

    $trackedSymbol = & git ls-files -- '.worktree-symbol' 2>$null
    if ($trackedSymbol) { $errors.Add('git pollution: .worktree-symbol is tracked') }

    $staged = & git diff --cached --name-only -- 2>$null
    if ($staged) {
      $pollution = $staged | Where-Object { $_ -match '^(paradoc/|\.worktree-symbol$)' }
      if ($pollution) { $errors.Add('git pollution: staged includes paradoc/ or .worktree-symbol') }
    }
  }

  if ($errors.Count -gt 0) {
    Write-Output 'CHECK_RESULT=FAIL'
    foreach ($e in $errors) { Write-Output ('FAIL: ' + $e) }
    return $false
  }

  Write-Output 'CHECK_RESULT=PASS'
  return $true
}

function PrintStatus {
  $pwdNow = (Get-Location).Path
  $symbolPath = Join-Path $pwdNow '.worktree-symbol'

  $current = (& git rev-parse --abbrev-ref HEAD 2>$null | Select-Object -First 1).Trim()
  $baseBranch = ParaforkSymbolGet $symbolPath 'BASE_BRANCH'
  $wtBranch = ParaforkSymbolGet $symbolPath 'WORKTREE_BRANCH'
  $trackedDirty = (& git status --porcelain --untracked-files=no 2>$null | Measure-Object).Count
  $untracked = ((& git status --porcelain 2>$null) | Where-Object { $_ -match '^\?\?' } | Measure-Object).Count

  ParaforkPrintKv 'CURRENT_BRANCH' $current
  ParaforkPrintKv 'BASE_BRANCH' $baseBranch
  ParaforkPrintKv 'WORKTREE_BRANCH' $wtBranch
  ParaforkPrintKv 'TRACKED_DIRTY' $trackedDirty
  ParaforkPrintKv 'UNTRACKED_COUNT' $untracked
}

function PrintReview {
  $pwdNow = (Get-Location).Path
  $symbolPath = Join-Path $pwdNow '.worktree-symbol'
  $baseBranch = ParaforkSymbolGet $symbolPath 'BASE_BRANCH'
  $wtBranch = ParaforkSymbolGet $symbolPath 'WORKTREE_BRANCH'

  Write-Output '### Review material'
  Write-Output ("#### Commits ({0}..{1})" -f $baseBranch, $wtBranch)
  & git log --oneline "$baseBranch..$wtBranch" 2>$null | ForEach-Object { $_ }
  Write-Output ''
  Write-Output ("#### Files ({0}...{1})" -f $baseBranch, $wtBranch)
  & git diff --name-status "$baseBranch...$wtBranch" 2>$null | ForEach-Object { $_ }
}

function CmdHelpDebug {
  $pwdNow = (Get-Location).Path
  $symbolPath = ParaforkSymbolFindUpwards $pwdNow

  if ($symbolPath) {
    $worktreeId = ParaforkSymbolGet $symbolPath 'WORKTREE_ID'
    if ([string]::IsNullOrEmpty($worktreeId)) { $worktreeId = 'UNKNOWN' }
    $worktreeRoot = ParaforkSymbolGet $symbolPath 'WORKTREE_ROOT'

    if ($worktreeRoot) {
      $body = {
        ParaforkPrintKv 'SYMBOL_PATH' $symbolPath
        ParaforkPrintOutputBlock $worktreeId $script:InvocationPwd 'PASS' (ParaforkPsFileCmd $script:EntryPath @('do', 'exec'))
      }
      ParaforkInvokeLogged $worktreeRoot 'parafork help --debug' @('--debug') $body
      return 0
    }

    ParaforkPrintKv 'SYMBOL_PATH' $symbolPath
    ParaforkPrintOutputBlock $worktreeId $script:InvocationPwd 'PASS' (ParaforkPsFileCmd $script:EntryPath @('do', 'exec'))
    return 0
  }

  $baseRoot = ParaforkGitToplevel
  if (-not $baseRoot) {
    ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('help'))
    ParaforkDie 'not in a git repo and no .worktree-symbol found'
  }

  $container = WorktreeContainer $baseRoot
  if (-not (Test-Path -LiteralPath $container -PathType Container)) {
    ParaforkPrintKv 'BASE_ROOT' $baseRoot
    ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'PASS' (ParaforkPsFileCmd $script:EntryPath @('init', '--new'))
    Write-Output ''
    Write-Output "No worktree container found at: $container"
    return 0
  }

  $roots = ListWorktreesNewestFirst $baseRoot
  if (-not $roots -or $roots.Count -eq 0) {
    ParaforkPrintKv 'BASE_ROOT' $baseRoot
    ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'PASS' (ParaforkPsFileCmd $script:EntryPath @('init', '--new'))
    Write-Output ''
    Write-Output "No worktrees found under: $container"
    return 0
  }

  Write-Output 'Found worktrees (newest first):'
  foreach ($d in $roots) {
    $wid = ParaforkSymbolGet (Join-Path $d '.worktree-symbol') 'WORKTREE_ID'
    if ([string]::IsNullOrEmpty($wid)) { $wid = 'UNKNOWN' }
    Write-Output ("- {0}  {1}" -f $wid, $d)
  }

  $chosen = $roots[0]
  $chosenId = ParaforkSymbolGet (Join-Path $chosen '.worktree-symbol') 'WORKTREE_ID'
  if ([string]::IsNullOrEmpty($chosenId)) { $chosenId = 'UNKNOWN' }

  $safeNext = ParaforkPsFileCmd $script:EntryPath @('init', '--new')
  $takeoverNext = 'cd ' + (ParaforkQuotePs $chosen) + '; ' + (ParaforkPsFileCmd $script:EntryPath @('init', '--reuse', '--yes', '--i-am-maintainer'))

  Write-Output ''
  ParaforkPrintKv 'BASE_ROOT' $baseRoot
  ParaforkPrintKv 'SAFE_NEXT' $safeNext
  ParaforkPrintKv 'TAKEOVER_NEXT' $takeoverNext
  ParaforkPrintOutputBlock $chosenId $script:InvocationPwd 'PASS' $safeNext
  return 0
}

function CmdHelp {
  param([string[]]$CmdArgs = @())

  $topic = if ($CmdArgs.Count -gt 0) { $CmdArgs[0] } else { '' }
  switch ($topic) {
    '' {
      ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'PASS' (ParaforkPsFileCmd $script:EntryPath @())
      Write-Output (UsageMain)
      return 0
    }
    'debug' {
      if ($CmdArgs.Count -gt 1) { ParaforkDie ("unknown arg for help debug: {0}" -f $CmdArgs[1]) }
      return (CmdHelpDebug)
    }
    '--debug' {
      if ($CmdArgs.Count -gt 1) { ParaforkDie ("unknown arg for help debug: {0}" -f $CmdArgs[1]) }
      return (CmdHelpDebug)
    }
    default {
      ParaforkDie ("unknown help topic: {0}" -f $topic)
    }
  }
}

function InitNewWorktree {
  param([string]$BaseRoot)

  if (-not (Test-Path -LiteralPath $script:ConfigPath -PathType Leaf)) {
    ParaforkDie "missing config: $script:ConfigPath"
  }

  $branch = ResolveInitBaseBranch $BaseRoot
  $root = WorkdirRoot
  $rule = WorkdirRule
  $autoplan = AutoplanEnabled

  $container = Join-Path $BaseRoot $root
  if (-not (Test-Path -LiteralPath $container)) {
    $null = New-Item -ItemType Directory -Path $container -Force
  }

  $worktreeId = ExpandWorktreeRule $rule
  $worktreeRoot = Join-Path $container $worktreeId
  $worktreeBranch = "parafork/$worktreeId"

  if (Test-Path -LiteralPath $worktreeRoot) {
    ParaforkDie "worktree already exists: $worktreeRoot"
  }

  & git -C $BaseRoot worktree add -b $worktreeBranch $worktreeRoot $branch
  if ($LASTEXITCODE -ne 0) {
    ParaforkDie 'git worktree add failed'
  }

  $symbolPath = Join-Path $worktreeRoot '.worktree-symbol'
  $symbolText = @(
    'PARAFORK_WORKTREE=1'
    ("WORKTREE_ID={0}" -f $worktreeId)
    ("BASE_ROOT={0}" -f $BaseRoot)
    ("WORKTREE_ROOT={0}" -f $worktreeRoot)
    ("WORKTREE_BRANCH={0}" -f $worktreeBranch)
    ("BASE_BRANCH={0}" -f $branch)
    'WORKTREE_USED=1'
    'WORKTREE_LOCK=1'
    ("WORKTREE_LOCK_OWNER={0}" -f (ParaforkAgentId))
    ("WORKTREE_LOCK_AT={0}" -f (ParaforkNowUtc))
    ("CREATED_AT={0}" -f (ParaforkNowUtc))
    ''
  ) -join "`n"
  ParaforkWriteTextUtf8NoBom $symbolPath $symbolText

  $baseExclude = ParaforkGitPathAbs $BaseRoot 'info/exclude'
  $wtExclude = ParaforkGitPathAbs $worktreeRoot 'info/exclude'
  AppendUniqueLine $baseExclude ("/{0}/" -f $root)
  AppendUniqueLine $wtExclude '/.worktree-symbol'
  AppendUniqueLine $wtExclude '/paradoc/'

  $paradoc = Join-Path $worktreeRoot 'paradoc'
  if (-not (Test-Path -LiteralPath $paradoc)) {
    $null = New-Item -ItemType Directory -Path $paradoc -Force
  }

  $assetsRoot = Join-Path (Split-Path -Parent $PSScriptRoot) 'assets'
  $execTpl = Join-Path $assetsRoot 'Exec.md'
  $mergeTpl = Join-Path $assetsRoot 'Merge.md'
  $planTpl = Join-Path $assetsRoot 'Plan.md'

  if (Test-Path -LiteralPath $execTpl) { Copy-Item -LiteralPath $execTpl -Destination (Join-Path $paradoc 'Exec.md') -Force }
  if (Test-Path -LiteralPath $mergeTpl) { Copy-Item -LiteralPath $mergeTpl -Destination (Join-Path $paradoc 'Merge.md') -Force }
  if ($autoplan -eq 'true' -and (Test-Path -LiteralPath $planTpl)) { Copy-Item -LiteralPath $planTpl -Destination (Join-Path $paradoc 'Plan.md') -Force }

  $logFile = Join-Path $paradoc 'Log.txt'
  if (-not (Test-Path -LiteralPath $logFile)) { ParaforkWriteTextUtf8NoBom $logFile '' }

  $script:LastInitRoot = $worktreeRoot

  $body = {
    Write-Output 'MODE=new'
    ParaforkPrintKv 'WORKTREE_ROOT' $worktreeRoot
    ParaforkPrintKv 'WORKTREE_BRANCH' $worktreeBranch
    $next = 'cd ' + (ParaforkQuotePs $worktreeRoot) + '; ' + (ParaforkPsFileCmd $script:EntryPath @('do', 'exec'))
    ParaforkPrintOutputBlock $worktreeId $script:InvocationPwd 'PASS' $next
  }

  ParaforkInvokeLogged $worktreeRoot 'parafork init --new' @('--new') $body
  return 0
}

function CmdInit {
  param([string[]]$CmdArgs = @())

  $mode = 'auto'
  $yes = $false
  $iam = $false

  for ($i = 0; $i -lt $CmdArgs.Count; ) {
    $a = $CmdArgs[$i]

    switch ($a) {
      '--new' {
        if ($mode -ne 'auto' -and $mode -ne 'new') { ParaforkDie '--new and --reuse are mutually exclusive' }
        $mode = 'new'; $i++; continue
      }
      '--reuse' {
        if ($mode -ne 'auto' -and $mode -ne 'reuse') { ParaforkDie '--new and --reuse are mutually exclusive' }
        $mode = 'reuse'; $i++; continue
      }
      '--yes' { $yes = $true; $i++; continue }
      '--i-am-maintainer' { $iam = $true; $i++; continue }
      '-h' { Write-Output (UsageInit); return 0 }
      '--help' { Write-Output (UsageInit); return 0 }
      default { ParaforkDie ("unknown arg: {0}" -f $a) }
    }
  }

  $pwdNow = (Get-Location).Path
  $symbolPath = ParaforkSymbolFindUpwards $pwdNow
  $inWorktree = $false
  $symWtId = $null
  $symWtRoot = $null
  $symBase = $null

  if ($symbolPath) {
    $pfWt = ParaforkSymbolGet $symbolPath 'PARAFORK_WORKTREE'
    if ($pfWt -ne '1') {
      ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('help', '--debug'))
      ParaforkDie "found .worktree-symbol but not parafork worktree: $symbolPath"
    }
    $inWorktree = $true
    $symWtId = ParaforkSymbolGet $symbolPath 'WORKTREE_ID'
    $symWtRoot = ParaforkSymbolGet $symbolPath 'WORKTREE_ROOT'
    $symBase = ParaforkSymbolGet $symbolPath 'BASE_ROOT'
  }

  if ($mode -eq 'auto' -and $inWorktree) {
    if ([string]::IsNullOrEmpty($symWtId)) { $symWtId = 'UNKNOWN' }
    Write-Output 'REFUSED: init called from inside a worktree without --reuse or --new'
    ParaforkPrintKv 'SYMBOL_PATH' $symbolPath
    ParaforkPrintKv 'WORKTREE_ID' $symWtId
    ParaforkPrintKv 'WORKTREE_ROOT' $symWtRoot
    ParaforkPrintKv 'BASE_ROOT' $symBase
    Write-Output ''
    Write-Output 'Choose one:'
    Write-Output ("- Reuse current worktree: {0}" -f (ParaforkPsFileCmd $script:EntryPath @('init', '--reuse', '--yes', '--i-am-maintainer')))
    Write-Output ("- Create new worktree:    {0}" -f (ParaforkPsFileCmd $script:EntryPath @('init', '--new')))
    ParaforkPrintOutputBlock $symWtId $script:InvocationPwd 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('init', '--new'))
    return 1
  }

  if ($mode -eq 'reuse' -and -not $inWorktree) {
    ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('help', '--debug'))
    ParaforkDie '--reuse requires being inside existing parafork worktree'
  }

  if ($mode -eq 'auto') { $mode = 'new' }

  if ($mode -eq 'reuse') {
    if (-not $yes -or -not $iam) {
      Write-Output 'REFUSED: missing CLI gate'
      $wid = if ([string]::IsNullOrEmpty($symWtId)) { 'UNKNOWN' } else { $symWtId }
      ParaforkPrintOutputBlock $wid $script:InvocationPwd 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('init', '--reuse', '--yes', '--i-am-maintainer'))
      return 1
    }

    if ([string]::IsNullOrEmpty($symWtRoot)) {
      ParaforkDie 'missing WORKTREE_ROOT in .worktree-symbol'
    }

    $ok = ParaforkSymbolSet $symbolPath 'WORKTREE_USED' '1'
    if (-not $ok) { ParaforkDie "failed to update .worktree-symbol: $symbolPath" }
    ParaforkWriteWorktreeLock $symbolPath

    $body = {
      Write-Output 'MODE=reuse'
      ParaforkPrintKv 'WORKTREE_USED' '1'
      $wid = if ([string]::IsNullOrEmpty($symWtId)) { 'UNKNOWN' } else { $symWtId }
      $next = 'cd ' + (ParaforkQuotePs $symWtRoot) + '; ' + (ParaforkPsFileCmd $script:EntryPath @('do', 'exec'))
      ParaforkPrintOutputBlock $wid $script:InvocationPwd 'PASS' $next
    }

    ParaforkInvokeLogged $symWtRoot 'parafork init --reuse' @('--reuse', '--yes', '--i-am-maintainer') $body
    return 0
  }

  $baseRoot = if ($inWorktree) { $symBase } else { ParaforkGitToplevel }
  if (-not $baseRoot) {
    ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'FAIL' ('cd <BASE_ROOT>; ' + (ParaforkPsFileCmd $script:EntryPath @('init', '--new')))
    ParaforkDie 'not in a git repo'
  }

  return (InitNewWorktree $baseRoot)
}

function CmdCheckStatus {
  if (-not (GuardWorktree)) { return 1 }
  $null = Set-Location -LiteralPath $script:GuardWorktreeRoot
  if (-not (GuardConflictState $script:GuardWorktreeRoot $script:GuardWorktreeId (Get-Location).Path)) { return 1 }

  $body = {
    PrintStatus
    ParaforkPrintOutputBlock $script:GuardWorktreeId (Get-Location).Path 'PASS' (ParaforkPsFileCmd $script:EntryPath @('do', 'exec'))
  }

  ParaforkInvokeLogged $script:GuardWorktreeRoot 'parafork check status' @('status') $body
  return 0
}

function CmdCheckMerge {
  param([bool]$Strict)

  if (-not (GuardWorktree)) { return 1 }
  $null = Set-Location -LiteralPath $script:GuardWorktreeRoot
  if (-not (GuardConflictState $script:GuardWorktreeRoot $script:GuardWorktreeId (Get-Location).Path)) { return 1 }

  $argv = @('merge')
  if ($Strict) { $argv += '--strict' }

  $body = {
    $pwdNow = (Get-Location).Path
    PrintStatus
    PrintReview
    if (-not (CheckFiles -Phase 'merge' -Strict:$Strict)) {
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' ('fix issues then rerun: ' + (ParaforkPsFileCmd $script:EntryPath @('check', 'merge')))
      throw 'check failed'
    }
    ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'PASS' (ParaforkPsFileCmd $script:EntryPath @('merge', '--yes', '--i-am-maintainer'))
  }

  try {
    ParaforkInvokeLogged $script:GuardWorktreeRoot 'parafork check merge' $argv $body
    return 0
  } catch {
    return 1
  }
}

function CmdCheck {
  param([string[]]$CmdArgs = @())

  $strict = $false
  $topic = 'status'
  $topicSet = $false
  $rest = @()

  for ($i = 0; $i -lt $CmdArgs.Count; ) {
    $a = $CmdArgs[$i]
    switch ($a) {
      '--strict' { $strict = $true; $i++; continue }
      '-h' { Write-Output (UsageCheck); return 0 }
      '--help' { Write-Output (UsageCheck); return 0 }
      default {
        if (-not $topicSet) {
          $topic = $a
          $topicSet = $true
        } else {
          $rest += $a
        }
        $i++
      }
    }
  }

  if ($strict -and $topic -ne 'merge') {
    ParaforkDie '--strict is only valid for check merge'
  }

  switch ($topic) {
    'status' {
      if ($rest.Count -gt 0) { ParaforkDie ("unknown arg: {0}" -f $rest[0]) }
      return (CmdCheckStatus)
    }
    'merge' {
      if ($rest.Count -gt 0) { ParaforkDie ("unknown arg: {0}" -f $rest[0]) }
      return (CmdCheckMerge -Strict:$strict)
    }
    default { ParaforkDie ("unknown topic: {0}" -f $topic) }
  }
}

function CmdDoExec {
  param([string[]]$CmdArgs = @())

  $strict = $false
  for ($i = 0; $i -lt $CmdArgs.Count; ) {
    $a = $CmdArgs[$i]
    switch ($a) {
      '--strict' { $strict = $true; $i++; continue }
      '-h' { Write-Output (UsageDoExec); return 0 }
      '--help' { Write-Output (UsageDoExec); return 0 }
      default { ParaforkDie ("unknown arg: {0}" -f $a) }
    }
  }

  if (-not (GuardWorktree)) { return 1 }
  $null = Set-Location -LiteralPath $script:GuardWorktreeRoot
  if (-not (GuardConflictState $script:GuardWorktreeRoot $script:GuardWorktreeId (Get-Location).Path)) { return 1 }

  $argv = @('exec')
  if ($strict) { $argv += '--strict' }

  $body = {
    $pwdNow = (Get-Location).Path
    PrintStatus
    if (-not (CheckFiles -Phase 'exec' -Strict:$strict)) {
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' ('fix issues and rerun: ' + (ParaforkPsFileCmd $script:EntryPath @('do', 'exec')))
      throw 'check failed'
    }

    $changes = (& git status --porcelain 2>$null | Measure-Object).Count
    if ($changes -ne 0) {
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'PASS' (ParaforkPsFileCmd $script:EntryPath @('do', 'commit', '--message', '<msg>'))
    } else {
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'PASS' ('edit files (rerun: ' + (ParaforkPsFileCmd $script:EntryPath @('do', 'exec')) + ')')
    }
  }

  try {
    ParaforkInvokeLogged $script:GuardWorktreeRoot 'parafork do exec' $argv $body
    return 0
  } catch {
    return 1
  }
}

function CmdDoCommit {
  param([string[]]$CmdArgs = @())

  $message = $null
  $noCheck = $false

  for ($i = 0; $i -lt $CmdArgs.Count; ) {
    $a = $CmdArgs[$i]
    switch ($a) {
      '--message' {
        if ($i + 1 -ge $CmdArgs.Count) { ParaforkDie 'missing value for --message' }
        $message = $CmdArgs[$i + 1]
        $i += 2
        continue
      }
      '--no-check' { $noCheck = $true; $i++; continue }
      '-h' { Write-Output (UsageDoCommit); return 0 }
      '--help' { Write-Output (UsageDoCommit); return 0 }
      default { ParaforkDie ("unknown arg: {0}" -f $a) }
    }
  }

  if ([string]::IsNullOrEmpty($message)) { ParaforkDie 'missing --message' }

  if (-not (GuardWorktree)) { return 1 }
  $null = Set-Location -LiteralPath $script:GuardWorktreeRoot
  if (-not (GuardConflictState $script:GuardWorktreeRoot $script:GuardWorktreeId (Get-Location).Path)) { return 1 }

  $commitCmd = ParaforkPsFileCmd $script:EntryPath @('do', 'commit', '--message', $message)

  $body = {
    $pwdNow = (Get-Location).Path

    if (-not $noCheck) {
      if (-not (CheckFiles -Phase 'exec' -Strict:$false)) {
        Write-Output 'REFUSED: check failed'
        ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' ('fix issues then retry: ' + $commitCmd)
        throw 'check failed'
      }
    }

    & git add -A -- .
    if ($LASTEXITCODE -ne 0) { ParaforkDie 'git add failed' }

    $staged = & git diff --cached --name-only -- 2>$null
    if ($staged) {
      $pollution = $staged | Where-Object { $_ -match '^(paradoc/|\.worktree-symbol$)' }
      if ($pollution) {
        Write-Output 'REFUSED: git pollution staged'
        ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' ('unstage pollution and retry: git reset -q; ' + $commitCmd)
        throw 'pollution staged'
      }
    }

    & git diff --cached --quiet --
    if ($LASTEXITCODE -eq 0) {
      Write-Output 'REFUSED: nothing staged'
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' ('edit files then retry: ' + $commitCmd)
      throw 'nothing staged'
    }

    & git commit -m $message
    if ($LASTEXITCODE -ne 0) { ParaforkDie 'git commit failed' }

    $head = (& git rev-parse --short HEAD 2>$null | Select-Object -First 1).Trim()
    ParaforkPrintKv 'COMMIT' $head
    ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'PASS' (ParaforkPsFileCmd $script:EntryPath @('do', 'exec'))
  }

  try {
    ParaforkInvokeLogged $script:GuardWorktreeRoot 'parafork do commit' @('--message', $message) $body
    return 0
  } catch {
    return 1
  }
}

function CmdDo {
  param([string[]]$CmdArgs = @())

  if (-not $CmdArgs -or $CmdArgs.Count -eq 0) {
    Write-Output (UsageDo)
    return 0
  }

  $action = $CmdArgs[0]
  if ($action -eq '-h' -or $action -eq '--help') {
    Write-Output (UsageDo)
    return 0
  }

  $rest = @()
  if ($CmdArgs.Count -gt 1) { $rest = @($CmdArgs[1..($CmdArgs.Count - 1)]) }

  switch ($action) {
    'exec' { return (CmdDoExec -CmdArgs $rest) }
    'commit' { return (CmdDoCommit -CmdArgs $rest) }
    default { ParaforkDie ("unknown action: {0}" -f $action) }
  }
}

function CmdMerge {
  param([string[]]$CmdArgs = @())

  $yes = $false
  $iam = $false

  for ($i = 0; $i -lt $CmdArgs.Count; ) {
    $a = $CmdArgs[$i]
    switch ($a) {
      '--yes' { $yes = $true; $i++; continue }
      '--i-am-maintainer' { $iam = $true; $i++; continue }
      '-h' { Write-Output (UsageMerge); return 0 }
      '--help' { Write-Output (UsageMerge); return 0 }
      default { ParaforkDie ("unknown arg: {0}" -f $a) }
    }
  }

  if (-not (GuardWorktree)) { return 1 }
  $null = Set-Location -LiteralPath $script:GuardWorktreeRoot
  if (-not (GuardConflictState $script:GuardWorktreeRoot $script:GuardWorktreeId (Get-Location).Path)) { return 1 }

  $body = {
    $pwdNow = (Get-Location).Path
    $symbolPath = Join-Path $pwdNow '.worktree-symbol'
    $baseRoot = ParaforkSymbolGet $symbolPath 'BASE_ROOT'
    $baseBranch = ParaforkSymbolGet $symbolPath 'BASE_BRANCH'
    $wtBranch = ParaforkSymbolGet $symbolPath 'WORKTREE_BRANCH'

    PrintStatus
    PrintReview

    if (-not (CheckFiles -Phase 'merge' -Strict:$false)) {
      Write-Output 'REFUSED: check merge failed'
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' ('fix issues then rerun: ' + (ParaforkPsFileCmd $script:EntryPath @('check', 'merge')))
      throw 'check failed'
    }

    if (-not $yes -or -not $iam) {
      Write-Output 'REFUSED: missing CLI gate'
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' 'rerun with --yes --i-am-maintainer'
      throw 'missing cli gate'
    }

    $current = (& git rev-parse --abbrev-ref HEAD 2>$null | Select-Object -First 1).Trim()
    if ($wtBranch -and $current -ne $wtBranch) {
      Write-Output 'REFUSED: wrong worktree branch'
      ParaforkPrintKv 'EXPECTED_WORKTREE_BRANCH' $wtBranch
      ParaforkPrintKv 'CURRENT_BRANCH' $current
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' 'checkout correct branch and retry'
      throw 'wrong worktree branch'
    }

    $baseDirty = (& git -C $baseRoot status --porcelain --untracked-files=no 2>$null | Measure-Object).Count
    if ($baseDirty -ne 0) {
      Write-Output 'REFUSED: base repo not clean (tracked)'
      ParaforkPrintKv 'BASE_TRACKED_DIRTY' $baseDirty
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' 'clean base repo tracked changes then retry'
      throw 'base dirty'
    }

    $baseCurrent = (& git -C $baseRoot rev-parse --abbrev-ref HEAD 2>$null | Select-Object -First 1).Trim()
    if ($baseCurrent -ne $baseBranch) {
      Write-Output 'REFUSED: base branch mismatch'
      ParaforkPrintKv 'BASE_BRANCH' $baseBranch
      ParaforkPrintKv 'BASE_CURRENT_BRANCH' $baseCurrent
      ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' ('cd ' + (ParaforkQuotePs $baseRoot) + '; git checkout ' + (ParaforkQuotePs $baseBranch))
      throw 'base mismatch'
    }

    Write-Output ("PREVIEW_COMMITS={0}..{1}" -f $baseBranch, $wtBranch)
    & git -C $baseRoot log --oneline "$baseBranch..$wtBranch" 2>$null | ForEach-Object { $_ }
    Write-Output ("PREVIEW_FILES={0}...{1}" -f $baseBranch, $wtBranch)
    & git -C $baseRoot diff --name-status "$baseBranch...$wtBranch" 2>$null | ForEach-Object { $_ }

    $squash = SquashEnabled
    ParaforkPrintKv 'SQUASH' $squash
    $message = "parafork: merge $($script:GuardWorktreeId)"

    if ($squash -eq 'true') {
      & git -C $baseRoot merge --squash $wtBranch
      if ($LASTEXITCODE -ne 0) {
        Write-Output 'REFUSED: squash merge stopped (likely conflicts)'
        ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' ('resolve conflicts in ' + (ParaforkQuotePs $baseRoot) + ' then commit')
        throw 'squash merge failed'
      }
      & git -C $baseRoot commit -m $message
      if ($LASTEXITCODE -ne 0) { ParaforkDie 'git commit failed on base' }
    } else {
      & git -C $baseRoot merge --no-ff $wtBranch -m $message
      if ($LASTEXITCODE -ne 0) {
        Write-Output 'REFUSED: merge stopped (likely conflicts)'
        ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'FAIL' ('resolve then git -C ' + (ParaforkQuotePs $baseRoot) + ' merge --continue')
        throw 'merge failed'
      }
    }

    $merged = (& git -C $baseRoot rev-parse --short HEAD 2>$null | Select-Object -First 1).Trim()
    ParaforkPrintKv 'MERGED_COMMIT' $merged
    ParaforkPrintOutputBlock $script:GuardWorktreeId $pwdNow 'PASS' 'run acceptance steps in paradoc/Merge.md'
  }

  try {
    ParaforkInvokeLogged $script:GuardWorktreeRoot 'parafork merge' @('merge') $body
    return 0
  } catch {
    return 1
  }
}

function CmdDefault {
  $pwdNow = (Get-Location).Path
  $symbolPath = ParaforkSymbolFindUpwards $pwdNow
  $inWorktree = $false

  if ($symbolPath) {
    if ((ParaforkSymbolGet $symbolPath 'PARAFORK_WORKTREE') -eq '1') {
      $inWorktree = $true
    }
  }

  $baseRoot = if ($inWorktree) { ParaforkSymbolGet $symbolPath 'BASE_ROOT' } else { ParaforkGitToplevel }
  if (-not $baseRoot) {
    ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('help'))
    ParaforkDie 'not in a git repo and no .worktree-symbol found'
  }

  $null = Set-Location -LiteralPath $baseRoot
  $code = CmdInit -CmdArgs @('--new')
  if ($code -ne 0) { return $code }

  if (-not $script:LastInitRoot) {
    ParaforkDie 'failed to resolve new worktree root'
  }

  $null = Set-Location -LiteralPath $script:LastInitRoot
  return (CmdDo -CmdArgs @('exec'))
}

function InvokeMain {
  param([string[]]$CliArgs = @())

  if (-not $CliArgs -or $CliArgs.Count -eq 0) {
    return (CmdDefault)
  }

  $cmd = $CliArgs[0]
  if ($cmd -eq '-h' -or $cmd -eq '--help') { $cmd = 'help' }
  $rest = @()
  if ($CliArgs.Count -gt 1) { $rest = @($CliArgs[1..($CliArgs.Count - 1)]) }

  switch ($cmd) {
    'help' { return (CmdHelp -CmdArgs $rest) }
    'init' { return (CmdInit -CmdArgs $rest) }
    'do' { return (CmdDo -CmdArgs $rest) }
    'check' { return (CmdCheck -CmdArgs $rest) }
    'merge' { return (CmdMerge -CmdArgs $rest) }
    default {
      Write-Output ("ERROR: unknown command: {0}" -f $cmd)
      ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('help'))
      Write-Output (UsageMain)
      return 1
    }
  }
}

try {
  $result = @(InvokeMain -CliArgs $script:CliArgs)
  $exitCode = 0

  if ($result.Count -gt 0 -and $result[-1] -is [int]) {
    $exitCode = [int]$result[-1]
    if ($result.Count -gt 1) {
      $result[0..($result.Count - 2)] | Write-Output
    }
  } else {
    $result | Write-Output
  }

  $global:LASTEXITCODE = [int]$exitCode
  exit $global:LASTEXITCODE
} catch {
  if (-not $global:PARAFORK_OUTPUT_BLOCK_PRINTED) {
    ParaforkPrintOutputBlock 'UNKNOWN' $script:InvocationPwd 'FAIL' (ParaforkPsFileCmd $script:EntryPath @('help', '--debug'))
  }
  $msg = $_.Exception.Message
  if ([string]::IsNullOrEmpty($msg)) { $msg = $_.ToString() }
  Write-Output ("ERROR: {0}" -f $msg)
  $global:LASTEXITCODE = 1
  exit 1
}
