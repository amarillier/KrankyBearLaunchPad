@echo off
REM Windows batch wrapper for compile-windows.ps1
REM Fixes line endings (LF to CRLF) before execution if possible

cd /d "%~dp0"

REM Fix line endings in PowerShell scripts (convert LF to CRLF)
REM Use try/catch to continue even if file is locked
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "try{$f='%~dp0compile-windows.ps1';if(Test-Path $f){$c=[System.IO.File]::ReadAllText($f);$c=$c -replace \"`r`n\",\"`n\";$c=$c -replace \"`n\",\"`r`n\";[System.IO.File]::WriteAllText($f,$c,[System.Text.Encoding]::UTF8)}}catch{}"
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "try{$f='%~dp0prepare-deps.ps1';if(Test-Path $f){$c=[System.IO.File]::ReadAllText($f);$c=$c -replace \"`r`n\",\"`n\";$c=$c -replace \"`n\",\"`r`n\";[System.IO.File]::WriteAllText($f,$c,[System.Text.Encoding]::UTF8)}}catch{}"

REM Execute the PowerShell script
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0compile-windows.ps1" %*

REM "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
