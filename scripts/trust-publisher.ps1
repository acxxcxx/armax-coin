# ==============================================================================
# trust-publisher.ps1
# Installs the Armaxcoin Core Publisher Certificate into CurrentUser Trust Store
# so Windows recognizes signed Armaxcoin binaries (armgui.exe, armcoind.exe, etc.)
# ==============================================================================

[CmdletBinding()]
param(
    [string]$CertFile = "armax-code-signing.cer"
)

$ErrorActionPreference = "Stop"

$certPath = Join-Path $PSScriptRoot $CertFile
if (-not (Test-Path $certPath)) {
    Write-Host "[-] Certificate file not found: $certPath" -ForegroundColor Red
    exit 1
}

Write-Host "[+] Installing Armaxcoin Core publisher certificate for CurrentUser..." -ForegroundColor Cyan
Import-Certificate -FilePath $certPath -CertStoreLocation "Cert:\CurrentUser\Root" | Out-Null
Import-Certificate -FilePath $certPath -CertStoreLocation "Cert:\CurrentUser\TrustedPublisher" | Out-Null
Write-Host "[OK] Armaxcoin Core certificate trusted for CurrentUser! You can now launch armgui.exe without Unknown Publisher warnings." -ForegroundColor Green

