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

Write-Host "[+] Compiling Daemon Node (armcoind.exe)..."
go build -ldflags="-s -w" -o armcoind.exe main.go

Write-Host "[+] Compiling CPU Miner (armminer.exe)..."
go build -ldflags="-s -w" -o armminer.exe miner/miner.go

Write-Host "[+] Compiling Transaction Sender CLI (armsend.exe)..."
go build -ldflags="-s -w" -o armsend.exe ./cli/armsend

Write-Host "[+] Compiling Wallet Manager CLI (armwallet.exe)..."
go build -ldflags="-s -w" -o armwallet.exe ./cli/armwallet

Write-Host "[+] Compiling Desktop GUI Wallet & Node (armgui.exe)..."
go build -ldflags="-s -w -H=windowsgui" -o armgui.exe gui/main.go

Write-Host "=================================================="
Write-Host "[OK] ALL WINDOWS BINARIES SUCCESSFULLY COMPILED!"
Write-Host "=================================================="
