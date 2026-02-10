!macro NSIS_HOOK_POSTINSTALL
  ; 让安装目录更直观：把 _up_\runtime\{server,batch,updater} 平铺到安装根目录
  IfFileExists "$INSTDIR\_up_\runtime\server\*.*" 0 +3
  RMDir /r "$INSTDIR\server"
  Rename "$INSTDIR\_up_\runtime\server" "$INSTDIR\server"

  IfFileExists "$INSTDIR\_up_\runtime\batch\*.*" 0 +3
  RMDir /r "$INSTDIR\batch"
  Rename "$INSTDIR\_up_\runtime\batch" "$INSTDIR\batch"

  IfFileExists "$INSTDIR\_up_\runtime\updater\*.*" 0 +3
  RMDir /r "$INSTDIR\updater"
  Rename "$INSTDIR\_up_\runtime\updater" "$INSTDIR\updater"

  ; CHANGELOG 也挪到根目录，方便查看
  IfFileExists "$INSTDIR\_up_\CHANGELOG.json" 0 +3
  Delete "$INSTDIR\CHANGELOG.json"
  Rename "$INSTDIR\_up_\CHANGELOG.json" "$INSTDIR\CHANGELOG.json"

  ; 若包含 python.zip，则安装后解压为 server\python，避免构建阶段复制海量小文件导致内存不足
  IfFileExists "$INSTDIR\server\python.zip" 0 +4
  nsExec::ExecToLog '"$SYSDIR\\WindowsPowerShell\\v1.0\\powershell.exe" -NoProfile -ExecutionPolicy Bypass -Command "Expand-Archive -Path ''$INSTDIR\\server\\python.zip'' -DestinationPath ''$INSTDIR\\server\\python'' -Force"'
  IfErrors 0 +2
  MessageBox MB_ICONSTOP "展开 python.zip 失败，请重新安装或手动解压。"
  Delete "$INSTDIR\server\python.zip"
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  ; 清理扁平布局下的文件（避免只清理 _up_ 导致残留）
  RMDir /r "$INSTDIR\server"
  RMDir /r "$INSTDIR\batch"
  RMDir /r "$INSTDIR\updater"
  Delete "$INSTDIR\CHANGELOG.json"
  Delete "$INSTDIR\server\python.zip"
!macroend
