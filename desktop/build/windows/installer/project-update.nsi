Unicode true

####
## Windows patch/update installer.
## Can be built directly after `wails build --target windows/amd64` without requiring
## a full `--nsis` packaging pass.
####
!include "wails_update_tools.nsh"
!include "MUI.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "StrFunc.nsh"

${Using:StrFunc} StrTok
!include "ptnexus-process-control.nsh"

VIProductVersion "${INFO_FILEVERSION}"
VIFileVersion    "${INFO_FILEVERSION}"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Update Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!define MUI_PAGE_CUSTOMFUNCTION_LEAVE ValidateInstallDir
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_LANGUAGE "SimpChinese"

Name "${INFO_PRODUCTNAME} 更新安装包"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-update.exe"
InstallDir "D:\Program Files (x86)\pt-nexus"
ShowInstDetails show

Function TrySetInstallDirFromPath
    Exch $0

    ${If} $0 == ""
        Push ""
        Return
    ${EndIf}

    ${If} ${FileExists} "$0\${PRODUCT_EXECUTABLE}"
        StrCpy $INSTDIR "$0"
        Push "1"
        Return
    ${EndIf}

    Push ""
FunctionEnd

!macro ProbeInstallLocation ROOT VIEW
    SetRegView ${VIEW}
    ReadRegStr $0 ${ROOT} "${UNINST_KEY}" "InstallLocation"
    Push "$0"
    Call TrySetInstallDirFromPath
    Pop $1
    ${If} $1 == "1"
        Return
    ${EndIf}
!macroend

!macro ProbeUninstallString ROOT VIEW
    SetRegView ${VIEW}
    ReadRegStr $0 ${ROOT} "${UNINST_KEY}" "UninstallString"
    ${If} $0 != ""
        ${StrTok} $1 $0 '"' 2 0
        ${If} $1 == ""
            ${StrTok} $1 $0 " " 1 0
        ${EndIf}
        ${If} $1 != ""
            ${GetParent} $1 $2
            Push "$2"
            Call TrySetInstallDirFromPath
            Pop $3
            ${If} $3 == "1"
                Return
            ${EndIf}
        ${EndIf}
    ${EndIf}
!macroend

Function ResolveExistingInstallDir
    !insertmacro ProbeInstallLocation HKLM 64
    !insertmacro ProbeInstallLocation HKCU 64
    !insertmacro ProbeInstallLocation HKLM 32
    !insertmacro ProbeInstallLocation HKCU 32

    !insertmacro ProbeUninstallString HKLM 64
    !insertmacro ProbeUninstallString HKCU 64
    !insertmacro ProbeUninstallString HKLM 32
    !insertmacro ProbeUninstallString HKCU 32
FunctionEnd

Function ValidateInstallDir
    ${IfNot} ${FileExists} "$INSTDIR\${PRODUCT_EXECUTABLE}"
        MessageBox MB_OK|MB_ICONSTOP "所选目录未检测到 PT Nexus，请选择已安装目录。"
        Abort
    ${EndIf}
    Call PTNexus_EnsureInstallReady
FunctionEnd

Function .onInit
    !insertmacro wails.checkArchitecture
    Call ResolveExistingInstallDir
    Call PTNexus_InitInstallContext
FunctionEnd

Section
    !insertmacro wails.setShellContext

    SetOutPath $INSTDIR
    !insertmacro wails.files
    File /oname=updater.exe "..\sidecar\updater.exe"
    File /oname=server.exe "..\sidecar\server.exe"
    File /oname=sites_data.json "..\sidecar\sites_data.json"
    File /oname=CHANGELOG.json "..\sidecar\CHANGELOG.json"

    SetOutPath "$INSTDIR\configs"
    File /r "..\sidecar\configs\*.*"

    SetOutPath $INSTDIR
    SetRegView 64
    WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
SectionEnd
