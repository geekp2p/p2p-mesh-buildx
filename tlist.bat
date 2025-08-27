@echo off
chcp 437>nul
setlocal enabledelayedexpansion

REM — รายชื่อไฟล์ .py และ .html ที่ต้องการดู
set "INCLUDE_FILES=android.py app.py engine.py runner.py index.html"

REM — แพทเทิร์นไฟล์ key อื่นๆ
set "OTHER_PATTERNS=.env docker* *.go *.mod *.sum *.md Dockerfile* *.sh requirements.txt emu.log droidflow.log docker-compose.yml environment.yml supervisord.conf *.conf"

echo.
echo ===== Displaying key files in current directory =====
echo.

REM — แสดงไฟล์ .py / .html จากลิสต์ที่กำหนด
for %%F in (%INCLUDE_FILES%) do (
  if exist "%%~fF" (
    echo ------------------------------------------------------------
    echo File: %%~fF
    echo ------------------------------------------------------------
    type "%%~fF"
    echo.
  )
)

REM — แสดงไฟล์ key อื่นๆ ตามแพทเทิร์น
for %%G in (%OTHER_PATTERNS%) do (
  for %%F in (%%G) do (
    if exist "%%~fF" (
      echo ------------------------------------------------------------
      echo File: %%~fF
      echo ------------------------------------------------------------
      type "%%~fF"
      echo.
    )
  )
)

endlocal
pause
