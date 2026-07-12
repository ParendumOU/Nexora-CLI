# NexoraCLI installer — Windows (PowerShell 5.1+)
#
#   powershell -c "irm https://raw.githubusercontent.com/ParendumOU/Nexora-CLI/main/install.ps1 | iex"
#
# Downloads the latest release binary and puts it on your PATH as `nexora`.
# No runtime dependencies. Safe to re-run (updates in place).
#
# Overrides (set before running): $env:NEXORA_CLI_VERSION (e.g. v0.19.0), $env:NEXORA_CLI_BIN_DIR

$ErrorActionPreference = "Stop"

$Repo = "ParendumOU/Nexora-CLI"
$Api  = "https://api.github.com/repos/$Repo"

function Write-Info($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Write-Ok($msg)   { Write-Host " OK $msg" -ForegroundColor Green }
function Fail($msg)       { Write-Host "ERROR: $msg" -ForegroundColor Red; exit 1 }

[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

Write-Host ""
Write-Host "  NexoraCLI -- terminal client for Nexora" -ForegroundColor White
Write-Host ""

# -- Latest version --------------------------------------------------------------
$Tag = $env:NEXORA_CLI_VERSION
if (-not $Tag) {
    Write-Info "Looking up the latest release"
    try {
        $release = Invoke-RestMethod -Uri "$Api/releases/latest" -UseBasicParsing
        $Tag = $release.tag_name
    } catch {
        Fail "Could not determine the latest release. Set `$env:NEXORA_CLI_VERSION = 'vX.Y.Z' and re-run."
    }
}
Write-Ok "Version $Tag"

# -- Download ---------------------------------------------------------------------
$BinDir = if ($env:NEXORA_CLI_BIN_DIR) { $env:NEXORA_CLI_BIN_DIR } else { Join-Path $env:LOCALAPPDATA "Nexora\bin" }
if (-not (Test-Path $BinDir)) { New-Item -ItemType Directory -Force $BinDir | Out-Null }
$Target = Join-Path $BinDir "nexora.exe"

$Base = "https://github.com/$Repo/releases/download/$Tag"
$found = $false
foreach ($asset in @("nexora-$Tag-windows-amd64.exe", "nexora-windows-amd64.exe")) {
    Write-Info "Downloading $asset"
    try {
        Invoke-WebRequest -Uri "$Base/$asset" -OutFile $Target -UseBasicParsing
        $found = $true
        break
    } catch { }
}
if (-not $found) {
    Fail "No prebuilt Windows binary in release $Tag. Check https://github.com/$Repo/releases/latest or build from source (see README)."
}
Write-Ok "Installed $Target"

# -- PATH ----------------------------------------------------------------------------
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$BinDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$BinDir", "User")
    $env:Path = "$env:Path;$BinDir"
    Write-Ok "Added $BinDir to your user PATH (restart other terminals to pick it up)"
}

Write-Host ""
Write-Host "  NexoraCLI installed!" -ForegroundColor Green
Write-Host ""
Write-Host "  Connect to your instance:"
Write-Host "    nexora login --url https://your-instance.example.com    # email/password"
Write-Host "    nexora pair  --url https://your-instance.example.com    # code from web Settings -> Devices"
Write-Host ""
Write-Host "  Then run:  nexora"
Write-Host ""
