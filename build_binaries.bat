@echo off
REM Windows Batch Script for Cross-Compiling YALS_SSH Application
REM This script builds for Windows x64 and Linux x64 targets

echo Starting build process for YALS_SSH...

REM Set Go environment variables
set GO111MODULE=on
set GOPROXY=https://goproxy.io,direct

REM Create output directories
mkdir bin 2>nul
mkdir bin\windows_amd64 2>nul
mkdir bin\linux_amd64 2>nul

REM Build for Windows x64
echo Building for Windows x64...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

go build -o bin\windows_amd64\yals_ssh_server.exe ./cmd/server/main.go

if %ERRORLEVEL% NEQ 0 (
    echo Build failed for Windows x64!
    exit /b %ERRORLEVEL%
)

REM Build for Linux x64
echo Building for Linux x64...
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0

go build -o bin\linux_amd64\yals_ssh_server ./cmd/server/main.go

if %ERRORLEVEL% NEQ 0 (
    echo Build failed for Linux x64!
    exit /b %ERRORLEVEL%
)

echo Build completed successfully!
echo Windows x64 binary: bin\windows_amd64\yals_ssh_server.exe
echo Linux x64 binary: bin\linux_amd64\yals_ssh_server