# NexoraCLI build script (Windows / PowerShell).
# Compiles inside the golang:1.23 container — no host Go toolchain required.
#
# Usage:
#   .\build.ps1                 # build host binary -> bin\nexora.exe
#   .\build.ps1 -All            # cross-compile all targets -> dist\
#   .\build.ps1 -Version 0.3.0  # stamp a version
#
# Requires Docker Desktop running.

param(
    [switch]$All,
    [string]$Version = "0.2.1",
    [string]$GoImage = "golang:1.23"
)

$ErrorActionPreference = "Stop"
$repo = $PSScriptRoot
$ldflags = "-s -w -X main.version=$Version"

# MSYS_NO_PATHCONV stops Git-Bash-style path mangling; -w //app is the in-container workdir.
$env:MSYS_NO_PATHCONV = "1"

function Invoke-Go([string]$script) {
    docker run --rm -v "${repo}:/app" -w //app $GoImage sh -c $script
    if ($LASTEXITCODE -ne 0) { throw "docker build failed (exit $LASTEXITCODE)" }
}

Write-Host "==> go mod tidy" -ForegroundColor Cyan
Invoke-Go "go mod tidy"

if ($All) {
    Write-Host "==> cross-compiling all targets (v$Version)" -ForegroundColor Cyan
    Invoke-Go @"
GOOS=linux   GOARCH=amd64 go build -ldflags '$ldflags' -o dist/nexora-linux-amd64 . && \
GOOS=darwin  GOARCH=arm64 go build -ldflags '$ldflags' -o dist/nexora-darwin-arm64 . && \
GOOS=windows GOARCH=amd64 go build -ldflags '$ldflags' -o dist/nexora-windows-amd64.exe .
"@
    Write-Host "==> built into dist\" -ForegroundColor Green
    Get-ChildItem "$repo\dist" | Format-Table Name, Length
} else {
    Write-Host "==> building host binary (v$Version)" -ForegroundColor Cyan
    Invoke-Go "go build -ldflags '$ldflags' -o bin/nexora.exe ."
    Write-Host "==> built bin\nexora.exe" -ForegroundColor Green
}
