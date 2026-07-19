# NexoraCLI installer — Windows (PowerShell 5.1+)
#
#   powershell -c "irm https://raw.githubusercontent.com/ParendumOU/Nexora-CLI/main/install.ps1 | iex"
#
# Zero-touch join (what an admin's invite one-liner does):
#   & ([scriptblock]::Create((irm https://.../install.ps1))) -Join <INVITE_TOKEN> -Url https://your-instance
# Same via env: $env:NEXORA_JOIN_TOKEN, $env:NEXORA_URL.
#
# Overrides: $env:NEXORA_CLI_VERSION (e.g. v0.19.0), $env:NEXORA_CLI_BIN_DIR
param(
    [string]$Join = $env:NEXORA_JOIN_TOKEN,
    [string]$Url  = $env:NEXORA_URL
)

$ErrorActionPreference = "Stop"

$Repo = "ParendumOU/Nexora-CLI"
$Api  = "https://api.github.com/repos/$Repo"

function Write-Info($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Write-Ok($msg)   { Write-Host "OK  $msg" -ForegroundColor Green }
function Fail($msg)       { Write-Host "ERROR: $msg" -ForegroundColor Red; exit 1 }

[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

# -- Latest version --------------------------------------------------------------
$Tag = $env:NEXORA_CLI_VERSION
if (-not $Tag) {
    try {
        $release = Invoke-RestMethod -Uri "$Api/releases/latest" -UseBasicParsing
        $Tag = $release.tag_name
    } catch {
        Fail "Could not determine the latest release. Set `$env:NEXORA_CLI_VERSION = 'vX.Y.Z' and re-run."
    }
}

# -- Download --------------------------------------------------------------------
$BinDir = if ($env:NEXORA_CLI_BIN_DIR) { $env:NEXORA_CLI_BIN_DIR } else { Join-Path $env:LOCALAPPDATA "Nexora\bin" }
if (-not (Test-Path $BinDir)) { New-Item -ItemType Directory -Force $BinDir | Out-Null }
$Target = Join-Path $BinDir "nexora.exe"

$Base = "https://github.com/$Repo/releases/download/$Tag"
$found = $false
foreach ($asset in @("nexora-$Tag-windows-amd64.exe", "nexora-windows-amd64.exe")) {
    try {
        Invoke-WebRequest -Uri "$Base/$asset" -OutFile $Target -UseBasicParsing
        $found = $true
        break
    } catch { }
}
if (-not $found) {
    Fail "No prebuilt Windows binary in release $Tag. Check https://github.com/$Repo/releases/latest or build from source (see README)."
}
Write-Ok "Installed nexora $Tag -> $Target"

# -- PATH (persist to user PATH + this session) ----------------------------------
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$BinDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$BinDir", "User")
    $env:Path = "$env:Path;$BinDir"
    Write-Ok "Added $BinDir to your user PATH"
}

# -- Join (auto) or print manual next-steps --------------------------------------
if ($Join -and $Url) {
    Write-Info "Connecting to $Url"
    & $Target join --url $Url --token $Join
    if ($LASTEXITCODE -ne 0) {
        Fail "Could not join. The invite may be expired or already used - ask your admin for a new one."
    }
    Write-Host ""
    Write-Ok "You're all set. Open a new terminal, then run:  nexora"
    Write-Host ""
} else {
    Write-Host ""
    Write-Host "Connect to your instance:"
    Write-Host "  nexora join  --url https://your-instance --token <INVITE_TOKEN>   # from an admin invite"
    Write-Host "  nexora login --url https://your-instance                          # email/password"
    Write-Host "  (open a new terminal so PATH picks up nexora)"
    Write-Host ""
}
