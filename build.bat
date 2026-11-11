@echo off
REM ===========================================
REM  Ticket System - one-click build (Go)
REM  Requires Go 1.21+. No CGO, no Python.
REM  Version is read from VERSION file.
REM ===========================================
setlocal enabledelayedexpansion
cd /d "%~dp0"

set OUT=ticket-system.exe

REM ---- read VERSION (first line) ----
set VER=0.0.0
for /f "usebackq delims=" %%v in ("VERSION") do (
    if "!VER!"=="0.0.0" set VER=%%v
)
if "%VER%"=="" set VER=0.0.0

REM ---- commit hash ----
set COMMIT=dev
for /f "delims=" %%c in ('git rev-parse --short HEAD 2^>nul') do set COMMIT=%%c

REM ---- build time ----
for /f "delims=" %%t in ('powershell -NoProfile -Command "Get-Date -Format yyyy-MM-dd_HH:mm" 2^>nul') do set BUILT=%%t
if "%BUILT%"=="" set BUILT=unknown

echo [1/3] go mod tidy ...
go mod tidy
if errorlevel 1 goto fail

echo [2/3] go test ./... ...
go test ./...
if errorlevel 1 goto fail

echo [3/3] go build (version %VER%, commit %COMMIT%) ...
set CGO_ENABLED=0
set GOOS=windows
go build -trimpath ^
  -ldflags "-s -w -X main.Version=%VER% -X main.Commit=%COMMIT% -X main.BuildTime=%BUILT%" ^
  -o %OUT% .
if errorlevel 1 goto fail

echo.
echo Build OK: %~dp0%OUT%
for %%A in (%OUT%) do echo Size: %%~zA bytes
echo Version: %VER%  commit: %COMMIT%
echo.
echo Run it by double-click. ticket.db and ticket_system.log
echo will be created in the same directory on first launch.
goto end

:fail
echo.
echo [FAILED] see the error above.
exit /b 1

:end
pause
