@echo off
REM Windows Batch Script for Cross-Compiling YALS Application
REM This script builds:
REM - Server for Windows x64 and Linux x64
REM - Agent for Windows x64 and Linux x64

echo Starting build process for YALS...

REM Set Go environment variables
set GO111MODULE=on
set GOPROXY=https://goproxy.io,direct

REM Create output directories
mkdir bin 2>nul

REM Build Server for Windows x64
echo Building Server for Windows x64...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

go build -o bin\yals_server.exe ./cmd/server/main.go

if %ERRORLEVEL% NEQ 0 (
    echo Server build failed for Windows x64!
    exit /b %ERRORLEVEL%
)

REM Build Server for Linux x64
echo Building Server for Linux x64...
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0

go build -o bin\yals_server ./cmd/server/main.go

if %ERRORLEVEL% NEQ 0 (
    echo Server build failed for Linux x64!
    exit /b %ERRORLEVEL%
)

REM Build Agent for Windows x64
echo Building Agent for Windows x64...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

go build -o bin\yals_agent.exe ./cmd/agent/main.go

if %ERRORLEVEL% NEQ 0 (
    echo Agent build failed for Windows x64!
    exit /b %ERRORLEVEL%
)

REM Build Agent for Linux x64
echo Building Agent for Linux x64...
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0

go build -o bin\yals_agent ./cmd/agent/main.go

if %ERRORLEVEL% NEQ 0 (
    echo Agent build failed for Linux x64!
    exit /b %ERRORLEVEL%
)

echo Build completed successfully!