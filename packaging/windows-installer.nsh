; Match the per-user startup registration in desktop_windows.go.
; The SQLite data directory is outside the installation and is preserved.
!macro customUnInstall
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "ReceiptStudio"
!macroend
