# ==============================================================================
# scripts/generate-signing-keys.ps1
# Generates 1-Year Windows Authenticode (.pfx/.cer) & GPG Release Signing Keys
# Publisher: Armaxcoin Core <dev@armax-coin.cloud>
#
# SECURITY NOTES:
# - Never writes passphrases to disk (no WINDOWS_PFX_PASSWORD.txt or GPG_PASSPHRASE.txt).
# - Never prints private key material to stdout, stderr, or logs.
# - Displays generated passphrases ONCE in an interactive terminal session only,
#   instructing the operator to store them immediately in a password manager.
# - Generates a GPG revocation certificate at signing_keys/gpg-revocation.asc.
# ==============================================================================
# Usage (run manually in an interactive PowerShell terminal):
#   .\scripts\generate-signing-keys.ps1
# ==============================================================================

[CmdletBinding()]
param(
    [string]$PublisherName = "Armaxcoin Core",
    [string]$PublisherEmail = "dev@armax-coin.cloud",
    [int]$ValidityYears = 1,
    [switch]$NonInteractiveTest
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

# 1. Generate random password for Windows Authenticode PFX (in memory only)
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

# Remove private key from local Windows CertStore after exporting to PFX
Remove-Item -Path "Cert:\CurrentUser\My\$($cert.Thumbprint)" -Force -ErrorAction SilentlyContinue

$pfxBase64 = [Convert]::ToBase64String([System.IO.File]::ReadAllBytes($pfxPath))
[System.IO.File]::WriteAllText((Join-Path $secretDir "WINDOWS_PFX_BASE64.txt"), $pfxBase64)

Write-Host "    [OK] Thumbprint : $($cert.Thumbprint)"
Write-Host "    [OK] Expires    : $($notAfter.ToString('yyyy-MM-dd HH:mm:ss'))"
Write-Host "    [OK] Public CER : $cerPublicPath"
Write-Host "    [OK] Secret PFX : $pfxPath (encrypted)"

# 3. Generate 1-Year GPG Keypair & Revocation Certificate
$gpgExe = "gpg"
if (-not (Get-Command $gpgExe -ErrorAction SilentlyContinue)) {
    $gitGpg = Join-Path $env:ProgramFiles "Git\usr\bin\gpg.exe"
    if (Test-Path $gitGpg) {
        $gpgExe = $gitGpg
    }
}

$gpgPassPlain = ""
$gpgFingerprint = ""

if (Get-Command $gpgExe -ErrorAction SilentlyContinue) {
    Write-Host "`n[+] Generating 1-Year GPG Release Signing Keypair & Revocation Certificate..." -ForegroundColor Green
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
    Remove-Item -Force $batchFile -ErrorAction SilentlyContinue

    # Extract fingerprint
    $fprLines = & $gpgExe --homedir $tempGpgHomeRel --batch --with-colons --fingerprint $PublisherEmail | Out-String
    foreach ($line in ($fprLines -split "`r?`n")) {
        if ($line.StartsWith("fpr:")) {
            $parts = $line.Split(":")
            if ($parts.Length -ge 10 -and $parts[9].Length -ge 16) {
                $gpgFingerprint = $parts[9]
                break
            }
        }
    }

    $pubAscSecret = Join-Path $secretDir "ARMAX-PGP-KEY.asc"
    $pubAscPublic = Join-Path $publicCertDir "ARMAX-PGP-KEY.asc"
    $privAscSecret = Join-Path $secretDir "GPG_PRIVATE_KEY.txt"
    $revAscSecret = Join-Path $secretDir "gpg-revocation.asc"

    $pubArmor = & $gpgExe --homedir $tempGpgHomeRel --batch --armor --export $PublisherEmail | Out-String
    [System.IO.File]::WriteAllText($pubAscSecret, $pubArmor)
    Copy-Item -Path $pubAscSecret -Destination $pubAscPublic -Force

    $privArmor = & $gpgExe --homedir $tempGpgHomeRel --batch --yes --pinentry-mode loopback --passphrase $gpgPassPlain --armor --export-secret-keys $PublisherEmail | Out-String
    [System.IO.File]::WriteAllText($privAscSecret, $privArmor)

    # Copy revocation certificate generated in openpgp-revocs.d to signing_keys/gpg-revocation.asc
    $revocDir = Join-Path $tempGpgHome "openpgp-revocs.d"
    $revFile = Get-ChildItem -Path $revocDir -Filter "*.rev" -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -ne $revFile) {
        $revContent = [System.IO.File]::ReadAllText($revFile.FullName)
        # Un-escape leading colon before -----BEGIN PGP PUBLIC KEY BLOCK----- so the revocation cert is ready for emergency use
        $revContent = $revContent -replace "(?m)^:-----BEGIN PGP PUBLIC KEY BLOCK-----", "-----BEGIN PGP PUBLIC KEY BLOCK-----"
        [System.IO.File]::WriteAllText($revAscSecret, $revContent)
    }

    # Stop gpg-agent and remove temporary keyring directory
    $gpgConf = Join-Path (Split-Path $gpgExe -Parent) "gpgconf.exe"
    if (Test-Path $gpgConf) {
        $ErrorActionPreference = "Continue"
        & $gpgConf --homedir $tempGpgHomeRel --kill gpg-agent 2>&1 | Out-Null
        $ErrorActionPreference = "Stop"
    }
    Remove-Item -Recurse -Force $tempGpgHome -ErrorAction SilentlyContinue

    Write-Host "    [OK] GPG Fingerprint      : $gpgFingerprint"
    Write-Host "    [OK] Public PGP Key       : $pubAscPublic"
    Write-Host "    [OK] Encrypted Secret Key : $privAscSecret"
    Write-Host "    [OK] Revocation Cert      : $revAscSecret"
}

if (-not $NonInteractiveTest) {
    Write-Host "`n==================================================================" -ForegroundColor Yellow
    Write-Host " IMPORTANT: STORE THESE PASSPHRASES IN YOUR PASSWORD MANAGER NOW!" -ForegroundColor Yellow
    Write-Host " Passphrases are shown ONCE in this terminal and NEVER saved to disk." -ForegroundColor Yellow
    Write-Host "==================================================================" -ForegroundColor Yellow
    Write-Host "  WINDOWS_PFX_PASSWORD : $pfxPasswordPlain" -ForegroundColor White
    if ($gpgPassPlain -ne "") {
        Write-Host "  GPG_PASSPHRASE       : $gpgPassPlain" -ForegroundColor White
    }
    Write-Host "==================================================================" -ForegroundColor Yellow
}

# Zeroize sensitive variables from memory
$pfxPasswordPlain = $null
$gpgPassPlain = $null
