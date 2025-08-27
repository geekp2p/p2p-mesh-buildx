@echo off
setlocal
set ROOT=%~dp0
if not exist "%ROOT%bin" mkdir "%ROOT%bin"
cd /d "%ROOT%relay" && go mod download && go build -o "%ROOT%bin\relay.exe" .
cd /d "%ROOT%node"  && go mod download && go build -o "%ROOT%bin\node.exe" .
echo Built to "%ROOT%bin\relay.exe" and "%ROOT%bin\node.exe"