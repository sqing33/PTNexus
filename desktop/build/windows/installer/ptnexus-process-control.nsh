!ifndef PTNEXUS_PROCESS_CONTROL_NSH
!define PTNEXUS_PROCESS_CONTROL_NSH

!include "FileFunc.nsh"

Var PTNEXUS_LAUNCH_PARAMS
Var PTNEXUS_INSTALL_DIR_ARG
Var PTNEXUS_MAIN_PID
Var PTNEXUS_SERVER_PID
Var PTNEXUS_UPDATER_PID

!define PTNEXUS_CLOSE_WAIT_INTERVAL_MS 200
!define PTNEXUS_CLOSE_WAIT_ATTEMPTS 15

Function PTNexus_InitInstallContext
    StrCpy $PTNEXUS_LAUNCH_PARAMS ""
    StrCpy $PTNEXUS_INSTALL_DIR_ARG ""
    StrCpy $PTNEXUS_MAIN_PID ""
    StrCpy $PTNEXUS_SERVER_PID ""
    StrCpy $PTNEXUS_UPDATER_PID ""

    ${GetParameters} $PTNEXUS_LAUNCH_PARAMS

    ClearErrors
    ${GetOptions} $PTNEXUS_LAUNCH_PARAMS "/PTNEXUS_INSTALL_DIR=" $PTNEXUS_INSTALL_DIR_ARG
    ${IfNot} ${Errors}
    ${AndIf} $PTNEXUS_INSTALL_DIR_ARG != ""
    ${AndIf} ${FileExists} "$PTNEXUS_INSTALL_DIR_ARG\${PRODUCT_EXECUTABLE}"
        StrCpy $INSTDIR "$PTNEXUS_INSTALL_DIR_ARG"
    ${EndIf}

    ClearErrors
    ${GetOptions} $PTNEXUS_LAUNCH_PARAMS "/PTNEXUS_MAIN_PID=" $PTNEXUS_MAIN_PID
    ${If} ${Errors}
        StrCpy $PTNEXUS_MAIN_PID ""
    ${EndIf}

    ClearErrors
    ${GetOptions} $PTNEXUS_LAUNCH_PARAMS "/PTNEXUS_SERVER_PID=" $PTNEXUS_SERVER_PID
    ${If} ${Errors}
        StrCpy $PTNEXUS_SERVER_PID ""
    ${EndIf}

    ClearErrors
    ${GetOptions} $PTNEXUS_LAUNCH_PARAMS "/PTNEXUS_UPDATER_PID=" $PTNEXUS_UPDATER_PID
    ${If} ${Errors}
        StrCpy $PTNEXUS_UPDATER_PID ""
    ${EndIf}
FunctionEnd

Function PTNexus_HasFastClosePIDs
    ${If} $PTNEXUS_MAIN_PID != ""
    ${OrIf} $PTNEXUS_SERVER_PID != ""
    ${OrIf} $PTNEXUS_UPDATER_PID != ""
        Push "1"
        Return
    ${EndIf}
    Push "0"
FunctionEnd

Function PTNexus_IsPidRunning
    Exch $0
    ${If} $0 == ""
        Push "0"
        Return
    ${EndIf}

    nsExec::ExecToStack /TIMEOUT=2000 '"$SYSDIR\tasklist.exe" /FI "PID eq $0" /FO CSV /NH'
    Pop $1
    Pop $2
    StrCpy $3 $2 1
    StrCmp $3 "$\"" pid_running
    Push "0"
    Return
pid_running:
    Push "1"
FunctionEnd

Function PTNexus_IsAnyFastPIDRunning
    ${If} $PTNEXUS_MAIN_PID != ""
        Push $PTNEXUS_MAIN_PID
        Call PTNexus_IsPidRunning
        Pop $0
        ${If} $0 == "1"
            Push "1"
            Return
        ${EndIf}
    ${EndIf}

    ${If} $PTNEXUS_SERVER_PID != ""
        Push $PTNEXUS_SERVER_PID
        Call PTNexus_IsPidRunning
        Pop $0
        ${If} $0 == "1"
            Push "1"
            Return
        ${EndIf}
    ${EndIf}

    ${If} $PTNEXUS_UPDATER_PID != ""
        Push $PTNEXUS_UPDATER_PID
        Call PTNexus_IsPidRunning
        Pop $0
        ${If} $0 == "1"
            Push "1"
            Return
        ${EndIf}
    ${EndIf}

    Push "0"
FunctionEnd

Function PTNexus_TaskkillPID
    Exch $0
    ${If} $0 == ""
        Push "0"
        Return
    ${EndIf}

    nsExec::ExecToStack /TIMEOUT=3000 '"$SYSDIR\taskkill.exe" /PID $0 /F'
    Pop $1
    Pop $2
    Push $1
FunctionEnd

Function PTNexus_TryFastClose
    Call PTNexus_HasFastClosePIDs
    Pop $0
    ${If} $0 == "0"
        Push "0"
        Return
    ${EndIf}

    ${If} $PTNEXUS_MAIN_PID != ""
        Push $PTNEXUS_MAIN_PID
        Call PTNexus_TaskkillPID
        Pop $0
    ${EndIf}
    ${If} $PTNEXUS_SERVER_PID != ""
        Push $PTNEXUS_SERVER_PID
        Call PTNexus_TaskkillPID
        Pop $0
    ${EndIf}
    ${If} $PTNEXUS_UPDATER_PID != ""
        Push $PTNEXUS_UPDATER_PID
        Call PTNexus_TaskkillPID
        Pop $0
    ${EndIf}

    StrCpy $0 0
fast_wait:
    Call PTNexus_IsAnyFastPIDRunning
    Pop $1
    ${If} $1 == "0"
        Push "1"
        Return
    ${EndIf}
    ${If} $0 >= ${PTNEXUS_CLOSE_WAIT_ATTEMPTS}
        Push "0"
        Return
    ${EndIf}
    IntOp $0 $0 + 1
    Sleep ${PTNEXUS_CLOSE_WAIT_INTERVAL_MS}
    Goto fast_wait
FunctionEnd

Function PTNexus_EnsureFallbackProcessControlScript
    InitPluginsDir
    StrCpy $0 "$PLUGINSDIR\ptnexus-process-control.ps1"
    IfFileExists "$0" done

    FileOpen $1 "$0" w
    FileWrite $1 "param([Parameter(Mandatory=$$true)][string]$$Mode,[Parameter(Mandatory=$$true)][string]$$InstallDir)$\r$\n"
    FileWrite $1 "$$targets = @($\r$\n"
    FileWrite $1 "  (Join-Path $$InstallDir '${PRODUCT_EXECUTABLE}'),$\r$\n"
    FileWrite $1 "  (Join-Path $$InstallDir 'server.exe'),$\r$\n"
    FileWrite $1 "  (Join-Path $$InstallDir 'updater.exe')$\r$\n"
    FileWrite $1 ") | ForEach-Object { $$_.ToLowerInvariant() }$\r$\n"
    FileWrite $1 "$\r$\n"
    FileWrite $1 "function Get-PTNexusProcesses {$\r$\n"
    FileWrite $1 "  @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {$\r$\n"
    FileWrite $1 "    $$_.ExecutablePath -and ($$targets -contains $$_.ExecutablePath.ToLowerInvariant())$\r$\n"
    FileWrite $1 "  })$\r$\n"
    FileWrite $1 "}$\r$\n"
    FileWrite $1 "$\r$\n"
    FileWrite $1 "if ($$Mode -eq 'detect') {$\r$\n"
    FileWrite $1 "  if (@(Get-PTNexusProcesses).Count -gt 0) { exit 0 }$\r$\n"
    FileWrite $1 "  exit 1$\r$\n"
    FileWrite $1 "}$\r$\n"
    FileWrite $1 "$\r$\n"
    FileWrite $1 "if ($$Mode -eq 'close') {$\r$\n"
    FileWrite $1 "  $$running = @(Get-PTNexusProcesses)$\r$\n"
    FileWrite $1 "  if ($$running.Count -eq 0) { exit 0 }$\r$\n"
    FileWrite $1 "  foreach ($$process in $$running) {$\r$\n"
    FileWrite $1 "    try { Stop-Process -Id $$process.ProcessId -Force -ErrorAction Stop } catch {}$\r$\n"
    FileWrite $1 "  }$\r$\n"
    FileWrite $1 "  for ($$i = 0; $$i -lt ${PTNEXUS_CLOSE_WAIT_ATTEMPTS}; $$i++) {$\r$\n"
    FileWrite $1 "    Start-Sleep -Milliseconds ${PTNEXUS_CLOSE_WAIT_INTERVAL_MS}$\r$\n"
    FileWrite $1 "    if (@(Get-PTNexusProcesses).Count -eq 0) { exit 0 }$\r$\n"
    FileWrite $1 "  }$\r$\n"
    FileWrite $1 "  exit 2$\r$\n"
    FileWrite $1 "}$\r$\n"
    FileWrite $1 "$\r$\n"
    FileWrite $1 "exit 3$\r$\n"
    FileClose $1

done:
    Push $0
FunctionEnd

Function PTNexus_FallbackIsRunning
    Call PTNexus_EnsureFallbackProcessControlScript
    Pop $0
    DetailPrint "PT Nexus 进程检测: 尝试 fallback 路径检测 install_dir=$INSTDIR"
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$0" -Mode detect -InstallDir "$INSTDIR"'
    Pop $1
    Pop $2
    StrCmp $1 "0" fallback_running
    StrCmp $2 "0" fallback_running
    DetailPrint "PT Nexus 进程检测: fallback 未命中或执行失败 code=$1 output=$2"
    Push "0"
    Return
fallback_running:
    DetailPrint "PT Nexus 进程检测: fallback 命中正在运行的进程"
    Push "1"
FunctionEnd

Function PTNexus_FallbackCloseProcesses
    Call PTNexus_EnsureFallbackProcessControlScript
    Pop $0
    DetailPrint "PT Nexus 进程关闭: 尝试 fallback 关闭 install_dir=$INSTDIR"
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$0" -Mode close -InstallDir "$INSTDIR"'
    Pop $1
    Pop $2
    StrCmp $1 "0" fallback_close_ok
    StrCmp $2 "0" fallback_close_ok
    DetailPrint "PT Nexus 进程关闭: fallback 关闭失败 code=$1 output=$2"
    Push "0"
    Return
fallback_close_ok:
    DetailPrint "PT Nexus 进程关闭: fallback 关闭成功"
    Push "1"
FunctionEnd

Function PTNexus_IsRunning
    Call PTNexus_HasFastClosePIDs
    Pop $0
    ${If} $0 == "1"
        Call PTNexus_IsAnyFastPIDRunning
        Pop $1
        ${If} $1 == "1"
            Push "1"
            Return
        ${EndIf}
        DetailPrint "PT Nexus 进程检测: fast pid 未命中，回退到路径检测"
    ${EndIf}

    Call PTNexus_FallbackIsRunning
    Pop $1
    Push $1
FunctionEnd

Function PTNexus_CloseProcesses
    Call PTNexus_HasFastClosePIDs
    Pop $0
    ${If} $0 == "1"
        Call PTNexus_TryFastClose
        Pop $1
        ${If} $1 == "1"
            Call PTNexus_IsRunning
            Pop $2
            ${If} $2 == "0"
                Push "1"
                Return
            ${EndIf}
        ${EndIf}
    ${EndIf}

    Call PTNexus_FallbackCloseProcesses
    Pop $1
    ${If} $1 == "1"
        Call PTNexus_IsRunning
        Pop $2
        ${If} $2 == "0"
            Push "1"
            Return
        ${EndIf}
    ${EndIf}

    Push "0"
FunctionEnd

Function PTNexus_EnsureInstallReady
retry_close:
    Call PTNexus_IsRunning
    Pop $0
    ${If} $0 == "0"
        Return
    ${EndIf}

    MessageBox MB_OKCANCEL|MB_ICONEXCLAMATION "检测到 PT Nexus 正在运行。点击“确定”后，安装器会自动关闭 PT Nexus 并继续安装。" IDOK do_close
    Abort

do_close:
    DetailPrint "检测到 PT Nexus 正在运行，正在尝试关闭..."
    Call PTNexus_CloseProcesses
    Pop $1
    ${If} $1 == "1"
        DetailPrint "PT Nexus 已退出，继续安装。"
        Return
    ${EndIf}

    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "未能自动关闭正在运行的 PT Nexus，请确认程序已退出后重试。" IDRETRY retry_close
    Abort
FunctionEnd

!endif
