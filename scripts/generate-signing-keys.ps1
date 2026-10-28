# ==============================================================================
# scripts/generate-signing-keys.ps1
# Generates 1-Year Windows Authenticode (.pfx/.cer) & GPG Release Signing Keys
# Publisher: Armaxcoin Core <dev@armax-coin.cloud>
# ==============================================================================
# Usage:
#   .\scripts\generate-signing-keys.ps1
# ==============================================================================

[CmdletBinding()]
param(
    [string]$PublisherName = "Armaxcoin Core",
    [string]$PublisherEmail = "dev@armax-coin.cloud",
    [int]$ValidityYears = 1
)

$ErrorActionPreference = "Stop"

$root = $PSScriptRoot | Split-Path -Parent
$secretDir = Join-Path $root "signing_keys"
$publicCertDir = Join-Path $root "scripts\certs"

New-Item -ItemType Directory -Path $secretDir -Force | Out-Null
New-Item -ItemType Directory -Path $publicCertDir -Force | Out-Null

Write-Host "==================================================================" -ForegroundColor Cyan
Write-Host " ARMAXCOIN RELEASE SIGNING KEY GENERATOR ($ValidityYears-Year Validity)" -ForegroundColor Cyan
Write-Host " Publisher : $PublisherName <$PublisherEmail>" -ForegroundColor Cyan
Write-Host "==================================================================" -ForegroundColor Cyan

# 1. Generate random password for Windows Authenticode PFX
$rngBytes = New-Object byte[] 24
[System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($rngBytes)
$pfxPasswordPlain = [Convert]::ToBase64String($rngBytes).Replace("+", "A").Replace("/", "B").Replace("=", "")
$pfxPasswordSecure = ConvertTo-SecureString -String $pfxPasswordPlain -Force -AsPlainText

# 2. Generate 1-Year Self-Signed Code Signing Certificate
$notBefore = (Get-Date).AddMinutes(-5)
$notAfter = (Get-Date).AddYears($ValidityYears)
$subject = "CN=$PublisherName, O=$PublisherName, E=$PublisherEmail"

Write-Host "`n[+] Generating Windows Authenticode Code Signing Certificate..." -ForegroundColor Green
$cert = New-SelfSignedCertificate `
    -Subject $subject `
    -Type CodeSigningCert `
    -KeySpec Signature `
    -KeyUsage DigitalSignature `
    -KeyAlgorithm RSA `
    -KeyLength 3072 `
    -HashAlgorithm SHA256 `
    -NotBefore $notBefore `
    -NotAfter $notAfter `
    -CertStoreLocation "Cert:\CurrentUser\My"

$pfxPath = Join-Path $secretDir "armax-code-signing.pfx"
$cerSecretPath = Join-Path $secretDir "armax-code-signing.cer"
$cerPublicPath = Join-Path $publicCertDir "armax-code-signing.cer"

Export-PfxCertificate -Cert $cert -FilePath $pfxPath -Password $pfxPasswordSecure | Out-Null
Export-Certificate -Cert $cert -FilePath $cerSecretPath | Out-Null
Copy-Item -Path $cerSecretPath -Destination $cerPublicPath -Force

# Remove private key from local Windows CertStore after exporting to PFX so it stays isolated in signing_keys/
Remove-Item -Path "Cert:\CurrentUser\My\$($cert.Thumbprint)" -Force -ErrorAction SilentlyContinue

$pfxBase64 = [Convert]::ToBase64String([System.IO.File]::ReadAllBytes($pfxPath))
[System.IO.File]::WriteAllText((Join-Path $secretDir "WINDOWS_PFX_BASE64.txt"), $pfxBase64)
[System.IO.File]::WriteAllText((Join-Path $secretDir "WINDOWS_PFX_PASSWORD.txt"), $pfxPasswordPlain)

Write-Host "    [OK] Thumbprint : $($cert.Thumbprint)"
Write-Host "    [OK] Expires    : $($notAfter.ToString('yyyy-MM-dd HH:mm:ss'))"
Write-Host "    [OK] Public CER : $cerPublicPath"
Write-Host "    [OK] Secret PFX : $pfxPath"

# 3. Generate 1-Year GPG Keypair for SHA256SUMS.asc Release Signing
$gpgExe = "gpg"
if (-not (Get-Command $gpgExe -ErrorAction SilentlyContinue)) {
    $gitGpg = Join-Path $env:ProgramFiles "Git\usr\bin\gpg.exe"
    if (Test-Path $gitGpg) {
        $gpgExe = $gitGpg
    }
}

if (Get-Command $gpgExe -ErrorAction SilentlyContinue) {
    Write-Host "`n[+] Generating 1-Year GPG Release Signing Keypair..." -ForegroundColor Green
    $rngGpgBytes = New-Object byte[] 24
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($rngGpgBytes)
    $gpgPassPlain = [Convert]::ToBase64String($rngGpgBytes).Replace("+", "X").Replace("/", "Y").Replace("=", "")

    Set-Location $root
    $tempGpgHomeRel = "signing_keys/gnupg_temp"
    $tempGpgHome = Join-Path $secretDir "gnupg_temp"
    if (Test-Path $tempGpgHome) { Remove-Item -Recurse -Force $tempGpgHome }
    New-Item -ItemType Directory -Path $tempGpgHome -Force | Out-Null

    $batchSpec = @"
Key-Type: RSA
Key-Length: 3072
Subkey-Type: RSA
Subkey-Length: 3072
Name-Real: $PublisherName
Name-Email: $PublisherEmail
Expire-Date: ${ValidityYears}y
Passphrase: $gpgPassPlain
%commit
"@
    $batchFile = Join-Path $tempGpgHome "keygen.batch"
    $batchFileRel = "signing_keys/gnupg_temp/keygen.batch"
    [System.IO.File]::WriteAllText($batchFile, ($batchSpec -replace "`r`n", "`n"))

    $ErrorActionPreference = "Continue"
    & $gpgExe --homedir $tempGpgHomeRel --batch --gen-key $batchFileRel 2>&1 | Out-Null
    $ErrorActionPreference = "Stop"

    $pubAscSecret = Join-Path $secretDir "ARMAX-PGP-KEY.asc"
    $pubAscPublic = Join-Path $publicCertDir "ARMAX-PGP-KEY.asc"
    $privAscSecret = Join-Path $secretDir "GPG_PRIVATE_KEY.txt"

    $pubArmor = & $gpgExe --homedir $tempGpgHomeRel --batch --armor --export $PublisherEmail | Out-String
    [System.IO.File]::WriteAllText($pubAscSecret, $pubArmor)
    Copy-Item -Path $pubAscSecret -Destination $pubAscPublic -Force

    $privArmor = & $gpgExe --homedir $tempGpgHomeRel --batch --yes --pinentry-mode loopback --passphrase $gpgPassPlain --armor --export-secret-keys $PublisherEmail | Out-String
    [System.IO.File]::WriteAllText($privAscSecret, $privArmor)
    [System.IO.File]::WriteAllText((Join-Path $secretDir "GPG_PASSPHRASE.txt"), $gpgPassPlain)

    # Stop gpg-agent and remove temporary keyring directory
    $gpgConf = Join-Path (Split-Path $gpgExe -Parent) "gpgconf.exe"
    if (Test-Path $gpgConf) {
        $ErrorActionPreference = "Continue"
        & $gpgConf --homedir $tempGpgHomeRel --kill gpg-agent 2>&1 | Out-Null
        $ErrorActionPreference = "Stop"
    }
    Remove-Item -Recurse -Force $tempGpgHome -ErrorAction SilentlyContinue

    Write-Host "    [OK] Public PGP Key : $pubAscPublic"
    Write-Host "    [OK] Secret PGP Key : $privAscSecret"
}

Write-Host "`n==================================================================" -ForegroundColor Cyan
Write-Host " GITHUB ACTIONS SECRETS READY (in git-ignored folder 'signing_keys/'):" -ForegroundColor Green
Write-Host "   1. WINDOWS_PFX_BASE64   -> signing_keys\WINDOWS_PFX_BASE64.txt"
Write-Host "   2. WINDOWS_PFX_PASSWORD -> signing_keys\WINDOWS_PFX_PASSWORD.txt"
Write-Host "   3. GPG_PRIVATE_KEY      -> signing_keys\GPG_PRIVATE_KEY.txt"
Write-Host "   4. GPG_PASSPHRASE       -> signing_keys\GPG_PASSPHRASE.txt"
Write-Host "==================================================================" -ForegroundColor Cyan
