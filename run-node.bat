@echo off

REM Change to the directory containing this script so it works anywhere
cd /d "%~dp0"

REM Default chat room must match the Docker/config.yaml setup
set APP_ROOM=my-room
set ENABLE_RELAY_CLIENT=1
set ENABLE_HOLEPUNCH=0
set ENABLE_UPNP=0

REM Run the node binary from the local bin folder
"%~dp0\bin\node.exe"