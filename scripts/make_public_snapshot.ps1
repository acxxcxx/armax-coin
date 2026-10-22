# ==============================================================================
# scripts/make_public_snapshot.ps1
# Creates a pristine, clean-history public repository snapshot of Armaxcoin.
# Usage:
#   .\scripts\make_public_snapshot.ps1 [-TargetDir <Path>] [-GitUserName <Name>] [-GitUserEmail <Email>]
# Example:
#   .\scripts\make_public_snapshot.ps1 -TargetDir "$env:TEMP\armax-coin-public" -GitUserName "acxxcxx" -GitUserEmail "acxxcxx@users.noreply.github.com"
# ==============================================================================

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$TargetDir = "",

    [Parameter(Position = 1)]
    [string]$GitUserName = "acxxcxx",

    [Parameter(Position = 2)]
    [string]$GitUserEmail = "acxxcxx@users.noreply.github.com",

    [Parameter()]
    [Alias("skip-gitleaks")]
    [switch]$SkipGitleaks = $false,

    [Parameter()]
    [Alias("f")]
    [switch]$Force = $false,

    [Parameter()]
    [switch]$Apply = $false,

    [Parameter()]
    [switch]$DryRun = $false,

    [Parameter()]
    [switch]$InjectPromotionFailure = $false
)

$ErrorActionPreference = "Stop"

# Default to dry-run unless -Apply is explicitly provided [P1-1]
$isDryRun = $true
if ($Apply -and -not $DryRun) {
    $isDryRun = $false
}

# ==============================================================================
# Canonical Path Resolution & Safety Guards [P1 Review]
# Resolves symlinks, NTFS junctions, '..', and relative paths before destructive evaluation
# ==============================================================================
function Resolve-CanonicalPath {
    param(
        [Parameter(Mandatory=$true)]
        [string]$Path
    )

    if ([string]::IsNullOrWhiteSpace($Path)) {
        return $null
    }

    try {
        $full = [System.IO.Path]::GetFullPath($Path)
    } catch {
        return $null
    }

    $root = [System.IO.Path]::GetPathRoot($full)
    if ([string]::IsNullOrEmpty($root)) {
        return $full
    }

    $relative = $full.Substring($root.Length)
    $segments = $relative.Split([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)

    $current = $root
    for ($i = 0; $i -lt $segments.Length; $i++) {
        $seg = $segments[$i]
        if ([string]::IsNullOrEmpty($seg)) {
            continue
        }
        $candidate = [System.IO.Path]::Combine($current, $seg)
        if (Test-Path -LiteralPath $candidate) {
            $visited = New-Object System.Collections.Generic.HashSet[string]
            $resolved = $candidate
            while (Test-Path -LiteralPath $resolved) {
                $item = Get-Item -LiteralPath $resolved -Force -ErrorAction SilentlyContinue
                if ($null -eq $item -or [string]::IsNullOrEmpty($item.LinkType)) {
                    break
                }
                $target = $item.Target
                if ([string]::IsNullOrEmpty($target)) {
                    break
                }
                if ($target -is [System.Array]) {
                    $target = $target[0]
                }
                if (-not [System.IO.Path]::IsPathRooted($target)) {
                    $target = [System.IO.Path]::Combine((Split-Path -Parent $resolved), $target)
                }
                try {
                    $target = [System.IO.Path]::GetFullPath($target)
                } catch {
                    return $null
                }
                if ($visited.Contains($target)) {
                    return $null # cyclic link
                }
                [void]$visited.Add($target)
                $resolved = $target
            }
            $current = [System.IO.Path]::GetFullPath($resolved)
        } else {
            $current = [System.IO.Path]::GetFullPath([System.IO.Path]::Combine($current, $seg))
        }
    }

    if ($current.Length -gt $root.Length) {
        return $current.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)
    }
    return $current
}

# Resolve script directory and repository root from the script path, NOT caller CWD [P1-1]
$scriptDir = $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($scriptDir)) {
    if ($MyInvocation.MyCommand.Definition) {
        $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
    }
}
if ([string]::IsNullOrWhiteSpace($scriptDir)) {
    $scriptDir = (Get-Location).Path
}

$scriptDirCanon = Resolve-CanonicalPath -Path $scriptDir
if ([string]::IsNullOrWhiteSpace($scriptDirCanon)) {
    Write-Host "[x] ERROR: Failed to canonically resolve script directory: '$scriptDir'" -ForegroundColor Red
    exit 1
}

# Discover repository root explicitly from the script directory [P1-1]
$repoRoot = & git -C $scriptDirCanon rev-parse --show-toplevel 2>$null
if (-not $repoRoot) {
    Write-Host "[x] ERROR: Failed to determine repository root from script directory: '$scriptDirCanon'" -ForegroundColor Red
    exit 1
}
$repoRootNorm = Resolve-CanonicalPath -Path $repoRoot
if ([string]::IsNullOrWhiteSpace($repoRootNorm)) {
    Write-Host "[x] ERROR: Failed to canonically resolve repository root path: '$repoRoot'" -ForegroundColor Red
    exit 1
}

if ([string]::IsNullOrWhiteSpace($TargetDir)) {
    $TargetDir = Join-Path ([System.IO.Path]::GetTempPath()) "armax-coin-public"
}

$normTarget = $TargetDir.TrimEnd('\', '/')
if ([string]::IsNullOrWhiteSpace($normTarget) -or $normTarget -eq "\" -or $normTarget -eq "/" -or $normTarget -match '^[a-zA-Z]:[\\/]?$') {
    Write-Host "[x] ERROR: Refusing root or dangerous target directory: '$TargetDir'" -ForegroundColor Red
    exit 1
}

$fullTarget = Resolve-CanonicalPath -Path $TargetDir
if ([string]::IsNullOrWhiteSpace($fullTarget)) {
    Write-Host "[x] ERROR: Failed to canonically resolve target directory path: '$TargetDir'" -ForegroundColor Red
    exit 1
}

# 1. Reject drive roots and root paths
if ($fullTarget -eq "\" -or $fullTarget -eq "/" -or $fullTarget -match '^[a-zA-Z]:[\\/]?$') {
    Write-Host "[x] ERROR: Refusing drive root as snapshot target: '$TargetDir' (resolved: '$fullTarget')" -ForegroundColor Red
    exit 1
}

# 2. Reject system directories
$systemDrive = $env:SystemDrive
if (-not $systemDrive) { $systemDrive = "C:" }
$sysDirs = @(
    $systemDrive + "\",
    $systemDrive + "\Windows",
    $systemDrive + "\Windows\System32",
    $systemDrive + "\Program Files",
    $systemDrive + "\Program Files (x86)"
)
foreach ($sys in $sysDirs) {
    $sysCanon = Resolve-CanonicalPath -Path $sys
    if ($sysCanon -and ($fullTarget -ieq $sysCanon -or $fullTarget.StartsWith($sysCanon + '\', [System.StringComparison]::OrdinalIgnoreCase))) {
        Write-Host "[x] ERROR: Refusing system directory as snapshot target: '$TargetDir' (resolved: '$fullTarget')" -ForegroundColor Red
        exit 1
    }
}

# 3. Reject user profile directory and ancestors
if ($env:USERPROFILE) {
    $userProfileNorm = Resolve-CanonicalPath -Path $env:USERPROFILE
    if ($userProfileNorm) {
        if ($fullTarget -ieq $userProfileNorm) {
            Write-Host "[x] ERROR: Refusing user HOME directory as snapshot target: '$TargetDir'" -ForegroundColor Red
            exit 1
        }
        if ($userProfileNorm.StartsWith($fullTarget + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
            Write-Host "[x] ERROR: Target directory '$TargetDir' is an ancestor of the user profile directory!" -ForegroundColor Red
            exit 1
        }
    }
}

# 4. Reject current working directory
$cwdNorm = Resolve-CanonicalPath -Path (Get-Location).Path
if ($cwdNorm -and $fullTarget -ieq $cwdNorm) {
    Write-Host "[x] ERROR: Target directory cannot be the current working directory!" -ForegroundColor Red
    exit 1
}

# 5. Reject repository root, targets inside repository, and ancestors containing repository [P1-4]
if ($fullTarget -ieq $repoRootNorm) {
    Write-Host "[x] ERROR: Target directory cannot be the current repository root!" -ForegroundColor Red
    exit 1
}
# Reject targets INSIDE the repository (e.g. .\wallet, .\core, etc.)
if ($fullTarget.StartsWith($repoRootNorm + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    Write-Host "[x] ERROR: Target directory '$TargetDir' is INSIDE the repository root! Refusing destructive operation." -ForegroundColor Red
    exit 1
}
# Reject targets that are ANCESTORS of the repository (e.g. drive root or parent directories)
if ($repoRootNorm.StartsWith($fullTarget + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    Write-Host "[x] ERROR: Target directory '$TargetDir' is an ANCESTOR of the repository root! Refusing destructive operation." -ForegroundColor Red
    exit 1
}

# Working Tree Cleanliness Check [P1-3]
if (-not $isDryRun -and $env:ARMAX_TEST_INJECT_PROMOTION_FAILURE -ne "1" -and -not $InjectPromotionFailure) {
    $dirtyStatus = & git -C $repoRootNorm status --porcelain 2>$null
    if ($dirtyStatus) {
        Write-Host "[x] ERROR: Working tree has uncommitted or untracked changes." -ForegroundColor Red
        Write-Host "    make_public_snapshot exports 'git archive HEAD' and requires a clean working tree." -ForegroundColor Red
        Write-Host "    Commit your changes or clean the tree before creating a public snapshot." -ForegroundColor Red
        exit 1
    }
}

$headCommit = & git -C $repoRootNorm rev-parse HEAD 2>$null
if (-not $headCommit) {
    Write-Host "[x] ERROR: Failed to resolve HEAD commit from repository '$repoRootNorm'." -ForegroundColor Red
    exit 1
}

Write-Host "==================================================" -ForegroundColor Cyan
Write-Host "[*] ARMAXCOIN - CLEAN PUBLIC SNAPSHOT GENERATOR" -ForegroundColor Cyan
Write-Host "==================================================" -ForegroundColor Cyan
Write-Host "Target Directory : $TargetDir"
Write-Host "Git User Name    : $GitUserName"
Write-Host "Git User Email   : $GitUserEmail"
Write-Host "Export Commit    : $headCommit"
Write-Host "Apply / Execute  : $([bool]$Apply)"
Write-Host "Force Overwrite  : $Force"
Write-Host "Dry Run Mode     : $isDryRun"
Write-Host "=================================================="

# Check if target exists [P1-5]
$targetExists = Test-Path -LiteralPath $fullTarget
if ($targetExists) {
    if (-not $Force) {
        Write-Host "[x] ERROR: Target directory '$TargetDir' already exists." -ForegroundColor Red
        Write-Host "    Pass -Force (-f) and -Apply to explicitly allow overwriting." -ForegroundColor Red
        exit 1
    }
    if ($isDryRun) {
        Write-Host "[DRY-RUN] Target exists. Will stage into unique temporary sibling directory and replace target only after successful verification." -ForegroundColor Yellow
    }
}

if ($isDryRun) {
    Write-Host "[DRY-RUN] Would create temporary staging directory next to target: $TargetDir"
    Write-Host "[DRY-RUN] Would export git archive HEAD ($headCommit) to staging directory"
    Write-Host "[DRY-RUN] Would scrub internal audit references"
    Write-Host "[DRY-RUN] Would verify prohibited files (*.cookie, *.exe, *.db, wallet.json, *.log, scratch/)"
    Write-Host "[DRY-RUN] Would initialize pristine git repository with single commit"
    if ($targetExists) {
        Write-Host "[DRY-RUN] Would atomically replace existing target with verified staging directory"
    } else {
        Write-Host "[DRY-RUN] Would move verified staging directory to target"
    }
    Write-Host "[OK] Dry-run completed successfully. No files were modified." -ForegroundColor Green
    Write-Host "     To execute real snapshot generation, rerun with '-Apply' (and '-Force' if overwriting)."
    exit 0
}

# ==============================================================================
# Isolated Staging Directory Generation [P1-5]
# Never delete an existing destination before export, extraction, sanitization,
# prohibited-file checks, and gitleaks scans have ALL succeeded!
# ==============================================================================
$targetParent = Split-Path -Parent $fullTarget
if (-not [string]::IsNullOrEmpty($targetParent) -and -not (Test-Path -LiteralPath $targetParent)) {
    New-Item -ItemType Directory -Path $targetParent -Force | Out-Null
}

$stagingDir = "$fullTarget.tmp." + [Guid]::NewGuid().ToString('N')
New-Item -ItemType Directory -Path $stagingDir -Force | Out-Null
$oldBackup = $null

try {
    # 1. Export clean working tree from HEAD via git archive into staging directory
    Write-Host "[+] Exporting working tree from Git HEAD into staging directory..." -ForegroundColor Green
    $systemTemp = [System.IO.Path]::GetTempPath()
    $tempArchive = Join-Path $systemTemp ('armax_snapshot_' + [Guid]::NewGuid().ToString('N') + '.tar')
    try {
        & git -C $repoRootNorm archive HEAD -o $tempArchive
        if ($LASTEXITCODE -ne 0) {
            throw "git archive failed with exit code $LASTEXITCODE"
        }

        # Extract tar archive into staging directory
        tar -xf $tempArchive -C $stagingDir
        if ($LASTEXITCODE -ne 0) {
            throw "tar extraction failed with exit code $LASTEXITCODE"
        }
    } finally {
        if (Test-Path -LiteralPath $tempArchive) {
            Remove-Item -Force -LiteralPath $tempArchive -ErrorAction SilentlyContinue
        }
    }

    # 2. Remove internal audit files and files containing private emails / local paths in staging directory
    Write-Host "[+] Scrubbing internal audit and local machine references..." -ForegroundColor Green
    $historyAudit = Join-Path $stagingDir "docs\HISTORY_AUDIT.md"
    if (Test-Path -LiteralPath $historyAudit) {
        Remove-Item -Force -LiteralPath $historyAudit
        Write-Host "    Removed: docs/HISTORY_AUDIT.md"
    }

    $cleanupRepo = Join-Path $stagingDir "docs\CLEANUP_REPO.md"
    if (Test-Path -LiteralPath $cleanupRepo) {
        $content = Get-Content -Raw -LiteralPath $cleanupRepo
        $content = $content -replace 'file:///[a-zA-Z]:/[^)]+', '/path/to/armax_coin_archive'
        Set-Content -Path $cleanupRepo -Value $content -NoNewline
    }

    # 3. Prohibited file pattern verification in staging directory
    Write-Host '[+] Verifying absence of prohibited files (*.cookie, *.exe, *.db, wallet.json, *.log, scratch/)...' -ForegroundColor Green
    $prohibitedPatterns = @("*.cookie", "*.exe", "*.db", "wallet.json", "*.log")
    $prohibitedFound = 0

    foreach ($pattern in $prohibitedPatterns) {
        $matches = Get-ChildItem -Path $stagingDir -Filter $pattern -Recurse -File -ErrorAction SilentlyContinue
        foreach ($m in $matches) {
            Write-Host "[-] PROHIBITED FILE DETECTED: $($m.FullName)" -ForegroundColor Red
            $prohibitedFound++
        }
    }

    $scratchDir = Join-Path $stagingDir "scratch"
    if (Test-Path -LiteralPath $scratchDir) {
        Write-Host "[-] PROHIBITED DIRECTORY DETECTED: $scratchDir" -ForegroundColor Red
        $prohibitedFound++
    }

    if ($prohibitedFound -gt 0) {
        Write-Host "[x] ERROR: $prohibitedFound prohibited files/directories detected in public snapshot. Aborting without modifying destination." -ForegroundColor Red
        exit 1
    }
    Write-Host "[OK] Prohibited file scan passed: 0 prohibited files found." -ForegroundColor Green

    # 4. Initialize fresh Git repository with single pristine commit in staging directory
    Write-Host "[+] Initializing pristine Git repository on branch 'main'..." -ForegroundColor Green
    Push-Location $stagingDir
    try {
        & git init -b main
        & git config user.name $GitUserName
        & git config user.email $GitUserEmail

        & git add .
        & git commit -m "Armaxcoin v1.0.0 (Public Open Testnet - Experimental)"
        if ($LASTEXITCODE -ne 0) {
            throw "git commit failed with exit code $LASTEXITCODE"
        }

        # 5. Run gitleaks secret detection across the clean repository in staging directory
        Write-Host "[+] Running gitleaks secret scan on snapshot repository..." -ForegroundColor Green
        $gitleaksPath = Get-Command gitleaks -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source
        if (-not $gitleaksPath) {
            $candidate = "$env:USERPROFILE\go\bin\gitleaks.exe"
            if (Test-Path -LiteralPath $candidate) {
                $gitleaksPath = $candidate
            }
        }

        if ($gitleaksPath) {
            & $gitleaksPath detect --source . --verbose --exit-code 1
            if ($LASTEXITCODE -ne 0) {
                throw "Gitleaks scan failed: potential secrets detected!"
            }
            Write-Host "[OK] Gitleaks secret verification passed!" -ForegroundColor Green
        } elseif ($SkipGitleaks -or ($args -contains "--skip-gitleaks")) {
            Write-Host "********************************************************************************" -ForegroundColor Yellow
            Write-Host "[!] WARNING: Gitleaks not found. Skipping secret scan due to explicit --skip-gitleaks flag!" -ForegroundColor Yellow
            Write-Host "    SECURITY RISK: Snapshot is NOT verified against credential/key leakage!" -ForegroundColor Yellow
            Write-Host "********************************************************************************" -ForegroundColor Yellow
        } else {
            Write-Host "********************************************************************************" -ForegroundColor Red
            Write-Host "[x] ERROR: 'gitleaks' executable not found in PATH or $env:USERPROFILE\go\bin\gitleaks.exe." -ForegroundColor Red
            Write-Host "    Snapshot verification failed-closed [L4-10-d]." -ForegroundColor Red
            Write-Host "    Install gitleaks (e.g. 'go install github.com/gitleaks/gitleaks/v8@latest') or" -ForegroundColor Red
            Write-Host "    pass '--skip-gitleaks' to explicitly bypass this check." -ForegroundColor Red
            Write-Host "********************************************************************************" -ForegroundColor Red
            exit 1
        }
    } finally {
        Pop-Location
    }

    # 6. Replace/rename to destination only after successful checks and authorization [P1-5, P2]
    Write-Host "[+] Replacing destination with validated snapshot..." -ForegroundColor Green
    if ($targetExists) {
        $oldBackup = "$fullTarget.old." + [Guid]::NewGuid().ToString('N')
        Move-Item -LiteralPath $fullTarget -Destination $oldBackup

        $promotionFailed = $false
        if ($env:ARMAX_TEST_INJECT_PROMOTION_FAILURE -eq "1" -or $InjectPromotionFailure) {
            Write-Host "[x] TEST INJECTION: Simulating failure during staging directory promotion!" -ForegroundColor Red
            $promotionFailed = $true
        } else {
            try {
                Move-Item -LiteralPath $stagingDir -Destination $fullTarget -ErrorAction Stop
                # Confirm new target before removing backup [P2]
                if (-not (Test-Path -LiteralPath (Join-Path $fullTarget "go.mod"))) {
                    Write-Host "[x] ERROR: Target confirmation failed after promotion." -ForegroundColor Red
                    $promotionFailed = $true
                }
            } catch {
                Write-Host "[x] ERROR: Move-Item failed: $_" -ForegroundColor Red
                $promotionFailed = $true
            }
        }

        if ($promotionFailed) {
            Write-Host "[!] Rolling back: restoring backup to $fullTarget..." -ForegroundColor Yellow
            if (Test-Path -LiteralPath $fullTarget) {
                Remove-Item -Recurse -Force -LiteralPath $fullTarget -ErrorAction SilentlyContinue
            }
            Move-Item -LiteralPath $oldBackup -Destination $fullTarget -ErrorAction Stop
            $oldBackup = $null
            exit 1
        }

        # Do not remove backup until the new target is confirmed [P2]
        if (Test-Path -LiteralPath $fullTarget) {
            Remove-Item -Recurse -Force -LiteralPath $oldBackup -ErrorAction SilentlyContinue
            $oldBackup = $null
        }
    } else {
        if ($env:ARMAX_TEST_INJECT_PROMOTION_FAILURE -eq "1" -or $InjectPromotionFailure) {
            Write-Host "[x] TEST INJECTION: Simulating failure during staging directory promotion!" -ForegroundColor Red
            exit 1
        }
        Move-Item -LiteralPath $stagingDir -Destination $fullTarget
    }

    $commitHash = & git -C $fullTarget rev-parse HEAD
    $branchName = & git -C $fullTarget branch --show-current

    Write-Host "==================================================" -ForegroundColor Cyan
    Write-Host "[OK] SUCCESS: Clean public snapshot ready at:" -ForegroundColor Cyan
    Write-Host "    $TargetDir"
    Write-Host "    Commit : $commitHash"
    Write-Host "    Branch : $branchName"
    Write-Host "==================================================" -ForegroundColor Cyan
} finally {
    if ($oldBackup -and (Test-Path -LiteralPath $oldBackup)) {
        if (-not (Test-Path -LiteralPath $fullTarget)) {
            Write-Host "[!] Staging cleanup finally: restoring $oldBackup to $fullTarget..." -ForegroundColor Yellow
            Move-Item -LiteralPath $oldBackup -Destination $fullTarget -ErrorAction SilentlyContinue
        }
    }
    if (Test-Path -LiteralPath $stagingDir) {
        Remove-Item -Recurse -Force -LiteralPath $stagingDir -ErrorAction SilentlyContinue
    }
}
