$ErrorActionPreference = "Stop"

# Refresh PATH if Go was recently installed
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path","Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path","User")
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    if (Test-Path "C:\Program Files\Go\bin\go.exe") {
        $env:Path += ";C:\Program Files\Go\bin"
    } else {
        Write-Error "Go compiler (go.exe) not found! Please ensure Go is installed and added to PATH."
        exit 1
    }
}

Write-Host "=================================================="
Write-Host "[*] COMPILING ARMAXCOIN (ARMX) ECOSYSTEM (WINDOWS)"
Write-Host "=================================================="

# Note: Do NOT pass -ldflags="-s -w" or -trimpath on Windows executables.
# Stripping PE symbol tables and DWARF headers triggers Windows Defender ML
# false-positive heuristics (Trojan:Win32/Wacatac.B!ml / Bearfoos) on Go binaries.

Write-Host "[+] Compiling Daemon Node (armcoind.exe)..."
go build -o armcoind.exe .

Write-Host "[+] Compiling CPU Miner (armminer.exe)..."
go build -o armminer.exe ./miner

Write-Host "[+] Compiling Transaction Sender CLI (armsend.exe)..."
go build -o armsend.exe ./cli/armsend

Write-Host "[+] Compiling Wallet Manager CLI (armwallet.exe)..."
go build -o armwallet.exe ./cli/armwallet

Write-Host "[+] Compiling Desktop GUI Wallet & Node (armgui.exe)..."
go build -ldflags="-H=windowsgui" -o armgui.exe ./gui

# Optional Authenticode signing if local code-signing certificate is available
$SigningCert = $null
$PfxPath = Join-Path $PSScriptRoot "signing_keys\armax-code-signing.pfx"
if ((Test-Path $PfxPath) -and -not [string]::IsNullOrWhiteSpace($env:WINDOWS_PFX_PASSWORD)) {
    try {
        $SigningCert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2(
            $PfxPath,
            $env:WINDOWS_PFX_PASSWORD,
            [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::Exportable
        )
    } catch {
        Write-Host "[!] Could not load PFX certificate: $($_.Exception.Message)" -ForegroundColor Yellow
    }
}
if (-not $SigningCert) {
    $SigningCert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert -ErrorAction SilentlyContinue |
        Where-Object { $_.Subject -like "*ARMAX*" } |
        Select-Object -First 1
}

if ($SigningCert) {
    Write-Host "[+] Signing Windows binaries with Authenticode ($($SigningCert.Thumbprint))..." -ForegroundColor Cyan
    foreach ($Exe in @("armcoind.exe", "armminer.exe", "armsend.exe", "armwallet.exe", "armgui.exe")) {
        if (Test-Path $Exe) {
            $sig = Set-AuthenticodeSignature -FilePath $Exe -Certificate $SigningCert -HashAlgorithm SHA256 -TimestampServer "http://timestamp.digicert.com" -ErrorAction SilentlyContinue
            if (-not $sig -or $sig.Status -eq "UnknownError") {
                Set-AuthenticodeSignature -FilePath $Exe -Certificate $SigningCert -HashAlgorithm SHA256 | Out-Null
            }
        }
    }
}

Write-Host "=================================================="
Write-Host "[OK] ALL WINDOWS BINARIES SUCCESSFULLY COMPILED!"
Write-Host "=================================================="
