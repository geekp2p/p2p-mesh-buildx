@echo off

REM Change to the script directory so the relay can be started from anywhere
cd /d "%~dp0"

REM Relay does not need a room, but set default to match other nodes
set APP_ROOM=my-room

REM Run the relay binary from the local bin folder
"%~dp0\bin\relay.exe