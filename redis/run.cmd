@echo off
rem QuickBite on Redis (Go Edition)
setlocal enabledelayedexpansion
cd /d "%~dp0"

if "%1"=="" goto app
if "%1"=="app" goto app
if "%1"=="check" goto check
if "%1"=="worker" goto worker
goto usage

:app
go run . app
goto :eof

:check
shift
if "%0"=="" (
    go run . check all
) else (
    go run . check %0
)
goto :eof

:worker
shift
go run . worker %*
goto :eof

:usage
echo usage: run.cmd [check [A^|B^|C^|D^|E] ^| worker ^<name^> [--crash]]
exit /b 1
