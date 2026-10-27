# ==============================================================================
# scripts/build-release.ps1
# Armaxcoin Local Multi-Platform Release Packager
# Mirrors .github/workflows/release.yml build specification
# ==============================================================================

[CmdletBinding()]
param(
    [string]$Version = "v1.0.5",
    [string]$OutputDir = "release_assets"
)

$ErrorActionPreference = "Stop"

Write-Host "==================================================" -ForegroundColor Cyan
Write-Host "[*] ARMAXCOIN LOCAL RELEASE BUILDER: $Version" -ForegroundColor Cyan
Write-Host "==================================================" -ForegroundColor Cyan

$root = $PSScriptRoot | Split-Path -Parent
Set-Location $root

# Ensure output directory is fresh
$outPath = Join-Path $root $OutputDir
if (Test-Path $outPath) {
    Remove-Item -Recurse -Force $outPath
}
New-Item -ItemType Directory -Path $outPath -Force | Out-Null

$targets = @(
    @{ os = "linux";   arch = "amd64"; ext = "";     format = "tar.gz" },
    @{ os = "linux";   arch = "arm64"; ext = "";     format = "tar.gz" },
    @{ os = "windows"; arch = "amd64"; ext = ".exe"; format = "zip"    },
    @{ os = "darwin";  arch = "arm64"; ext = "";     format = "tar.gz" }
)

$archiveList = @()

foreach ($t in $targets) {
    $os = $t.os
    $arch = $t.arch
    $ext = $t.ext
    $pkgBase = "armax-${Version}-${os}-${arch}"
    $tempDist = Join-Path $root "build_temp_${os}_${arch}"

    Write-Host "`n[+] Compiling binaries for target: $os / $arch ..." -ForegroundColor Green

    if (Test-Path $tempDist) { Remove-Item -Recurse -Force $tempDist }
    New-Item -ItemType Directory -Path $tempDist -Force | Out-Null

    # Copy base documentation and configs
    Copy-Item (Join-Path $root "README.md") $tempDist
    Copy-Item (Join-Path $root "armax.conf.example") $tempDist
    Copy-Item (Join-Path $root "LICENSE") $tempDist

    # Set Go build environment
    $env:CGO_ENABLED = "0"
    $env:GOOS = $os
    $env:GOARCH = $arch

    # Deterministic build flags
    $ldflags = "-s -w -buildid="

    # Compile core binaries
    Write-Host "    -> armcoind$ext"
    go build -trimpath -ldflags "-s -w -buildid=" -o (Join-Path $tempDist "armcoind$ext") main.go
    if (-not (Test-Path (Join-Path $tempDist "armcoind$ext"))) { throw "Failed to build armcoind$ext for $os/$arch" }

    Write-Host "    -> armminer$ext"
    go build -trimpath -ldflags "-s -w -buildid=" -o (Join-Path $tempDist "armminer$ext") miner/miner.go
    if (-not (Test-Path (Join-Path $tempDist "armminer$ext"))) { throw "Failed to build armminer$ext for $os/$arch" }

    Write-Host "    -> armsend$ext"
    go build -trimpath -ldflags "-s -w -buildid=" -o (Join-Path $tempDist "armsend$ext") ./cli/armsend
    if (-not (Test-Path (Join-Path $tempDist "armsend$ext"))) { throw "Failed to build armsend$ext for $os/$arch" }

    Write-Host "    -> armwallet$ext"
    go build -trimpath -ldflags "-s -w -buildid=" -o (Join-Path $tempDist "armwallet$ext") ./cli/armwallet
    if (-not (Test-Path (Join-Path $tempDist "armwallet$ext"))) { throw "Failed to build armwallet$ext for $os/$arch" }

    # Windows GUI Desktop Wallet
    if ($os -eq "windows" -and $arch -eq "amd64") {
        Write-Host "    -> armgui.exe (Windows Desktop GUI)"
        Copy-Item (Join-Path $root "gui/armgui.exe.manifest") $tempDist -ErrorAction SilentlyContinue
        Copy-Item (Join-Path $root "gui/armax_logo.ico") $tempDist -ErrorAction SilentlyContinue
        go build -trimpath -ldflags "-s -w -H windowsgui -buildid=" -o (Join-Path $tempDist "armgui.exe") ./gui
        if (-not (Test-Path (Join-Path $tempDist "armgui.exe"))) { throw "Failed to build armgui.exe" }
    }

    # Generate internal SHA256SUMS for the package contents
    Write-Host "    -> Generating internal SHA256SUMS..."
    $fileHashes = @()
    Get-ChildItem -Path $tempDist -File | Sort-Object Name | ForEach-Object {
        $h = (Get-FileHash -Path $_.FullName -Algorithm SHA256).Hash.ToLower()
        $fileHashes += "$h  $($_.Name)"
    }
    $internalSums = Join-Path $tempDist "SHA256SUMS"
    [System.IO.File]::WriteAllLines($internalSums, $fileHashes)

    # Compress package
    Write-Host "    -> Packaging into $outPath..." -ForegroundColor Yellow
    if ($t.format -eq "zip") {
        $archivePath = Join-Path $outPath "$pkgBase.zip"
        Compress-Archive -Path "$tempDist\*" -DestinationPath $archivePath -Force
        $archiveList += "$pkgBase.zip"
    } else {
        $archivePath = Join-Path $outPath "$pkgBase.tar.gz"
        # Use bsdtar with -C to ensure relative paths inside archive
        tar.exe -czvf $archivePath -C $tempDist .
        $archiveList += "$pkgBase.tar.gz"
    }

    # Cleanup temp build directory
    Remove-Item -Recurse -Force $tempDist
}

# Generate master SHA256SUMS in release_assets
Write-Host "`n[+] Generating Master SHA256SUMS in $OutputDir ..." -ForegroundColor Green
$masterHashes = @()
foreach ($archive in ($archiveList | Sort-Object)) {
    $targetFile = Join-Path $outPath $archive
    if (Test-Path $targetFile) {
        $h = (Get-FileHash -Path $targetFile -Algorithm SHA256).Hash.ToLower()
        $masterHashes += "$h  $archive"
        Write-Host "    $h  $archive"
    }
}

$masterSumsFile = Join-Path $outPath "SHA256SUMS"
[System.IO.File]::WriteAllLines($masterSumsFile, $masterHashes)

Write-Host "`n==================================================" -ForegroundColor Cyan
Write-Host "[OK] All 4 release archives and Master SHA256SUMS created successfully!" -ForegroundColor Green
Write-Host "Location: $outPath" -ForegroundColor Cyan
Write-Host "==================================================" -ForegroundColor Cyan
