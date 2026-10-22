#!/usr/bin/env bash
# ==============================================================================
# scripts/make_public_snapshot.sh
# Creates a pristine, clean-history public repository snapshot of Armaxcoin.
# Safe by default: refuses dangerous target paths and requires --force to overwrite.
# Usage:
#   ./scripts/make_public_snapshot.sh [OPTIONS] [TARGET_DIR] [GIT_USER_NAME] [GIT_USER_EMAIL]
# Options:
#   --apply            Explicitly execute snapshot creation (default is safe dry-run)
#   --force, -f        Overwrite existing target directory (safe non-repo paths only; requires --apply)
#   --dry-run          Explicit dry-run simulation (default)
#   --skip-gitleaks    Skip gitleaks scan (explicit opt-in)
# ==============================================================================

set -euo pipefail

SKIP_GITLEAKS=0
FORCE=0
APPLY=0
DRY_RUN=1
INJECT_PROMOTION_FAILURE=0
POSITIONAL_ARGS=()

for arg in "$@"; do
    case "$arg" in
        --apply)
            APPLY=1
            DRY_RUN=0
            ;;
        --dry-run)
            DRY_RUN=1
            APPLY=0
            ;;
        --skip-gitleaks)
            SKIP_GITLEAKS=1
            ;;
        --force|-f)
            FORCE=1
            ;;
        --inject-promotion-failure)
            INJECT_PROMOTION_FAILURE=1
            ;;
        -*)
            echo "[x] ERROR: Unknown option: $arg" >&2
            exit 1
            ;;
        *)
            POSITIONAL_ARGS+=("$arg")
            ;;
    esac
done

TARGET_DIR="${POSITIONAL_ARGS[0]:-/tmp/armax-coin-public}"
GIT_USER_NAME="${POSITIONAL_ARGS[1]:-acxxcxx}"
GIT_USER_EMAIL="${POSITIONAL_ARGS[2]:-acxxcxx@users.noreply.github.com}"

# ==============================================================================
# Canonical Path Resolution & Safety Guards [P1-1, P1-3, P1 Review]
# Resolves symlinks, '..', and relative paths before evaluation and before any destructive operation
# ==============================================================================
resolve_canonical_path() {
    local target="$1"
    local resolved=""
    if command -v realpath >/dev/null 2>&1; then
        resolved="$(realpath -m "${target}" 2>/dev/null || realpath "${target}" 2>/dev/null || echo "${target}")"
    elif command -v readlink >/dev/null 2>&1; then
        resolved="$(readlink -f "${target}" 2>/dev/null || echo "${target}")"
    elif command -v python3 >/dev/null 2>&1; then
        resolved="$(python3 -c "import os, sys; print(os.path.realpath(sys.argv[1]))" "${target}" 2>/dev/null || echo "${target}")"
    else
        resolved="${target}"
    fi

    # On Windows under MSYS2/Git Bash, normalize drive letters to POSIX format so repo comparisons match
    if command -v cygpath >/dev/null 2>&1; then
        resolved="$(cygpath -u "${resolved}" 2>/dev/null || echo "${resolved}")"
    fi

    echo "${resolved}"
}

# Resolve script directory and repository root from the script's own path, NOT caller CWD [P1-1]
SCRIPT_PATH="${BASH_SOURCE[0]}"
if [ -z "${SCRIPT_PATH}" ]; then
    echo "[x] ERROR: Failed to determine script path from BASH_SOURCE." >&2
    exit 1
fi

SCRIPT_DIR="$(cd -P "$(dirname "${SCRIPT_PATH}")" >/dev/null 2>&1 && pwd)"
if [ -z "${SCRIPT_DIR}" ]; then
    echo "[x] ERROR: Failed to determine script directory from '${SCRIPT_PATH}'." >&2
    exit 1
fi

SCRIPT_DIR_CANONICAL="$(resolve_canonical_path "${SCRIPT_DIR}" | tr '\\' '/' | sed 's:/*$::')"
if [ -z "${SCRIPT_DIR_CANONICAL}" ]; then
    echo "[x] ERROR: Failed to canonically resolve script directory: '${SCRIPT_DIR}'" >&2
    exit 1
fi

# Discover repository root explicitly from the script directory [P1-1]
REPO_ROOT="$(git -C "${SCRIPT_DIR_CANONICAL}" rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "${REPO_ROOT}" ]; then
    echo "[x] ERROR: Failed to determine repository root from script directory: '${SCRIPT_DIR_CANONICAL}'" >&2
    exit 1
fi

REPO_CANONICAL="$(resolve_canonical_path "${REPO_ROOT}" | tr '\\' '/' | sed 's:/*$::')"
if [ -z "${REPO_CANONICAL}" ]; then
    echo "[x] ERROR: Failed to canonically resolve repository root path: '${REPO_ROOT}'" >&2
    exit 1
fi

TARGET_CANONICAL="$(resolve_canonical_path "${TARGET_DIR}" | tr '\\' '/' | sed 's:/*$::')"
TARGET_CLEAN="$(echo "${TARGET_DIR}" | tr '\\' '/' | sed 's:/*$::')"

if [ -z "${TARGET_CANONICAL}" ] || [ "${TARGET_CANONICAL}" = "/" ] || [ "${TARGET_CANONICAL}" = "." ] || [ "${TARGET_CANONICAL}" = ".." ]; then
    echo "[x] ERROR: Refusing dangerous or invalid target directory: '${TARGET_DIR}'" >&2
    exit 1
fi

# Guard against root filesystems and core operating system mount points
case "${TARGET_CANONICAL}" in
    /|//|/root|/home|/etc|/usr|/bin|/sbin|/var|/dev|/sys|/proc|[a-zA-Z]:|[a-zA-Z]:/|/[a-zA-Z])
        echo "[x] ERROR: Refusing system root directory as snapshot target: '${TARGET_DIR}' (canonical: '${TARGET_CANONICAL}')" >&2
        exit 1
        ;;
esac

# Guard against user $HOME directory and ancestors of $HOME
if [ -n "${HOME:-}" ]; then
    HOME_CANONICAL="$(resolve_canonical_path "${HOME}" | tr '\\' '/' | sed 's:/*$::')"
    if [ -n "${HOME_CANONICAL}" ]; then
        if [ "${TARGET_CANONICAL}" = "${HOME_CANONICAL}" ] || [ "${TARGET_CLEAN}" = "$(echo "${HOME}" | tr '\\' '/' | sed 's:/*$::')" ]; then
            echo "[x] ERROR: Refusing user HOME directory as snapshot target: '${TARGET_DIR}'" >&2
            exit 1
        fi
        # Reject if target is ancestor of HOME (e.g. /home or /Users)
        if [[ "${HOME_CANONICAL}" == "${TARGET_CANONICAL}/"* ]]; then
            echo "[x] ERROR: Target directory '${TARGET_DIR}' is an ancestor of HOME directory!" >&2
            exit 1
        fi
    fi
fi

# Guard against current working directory
PWD_CANONICAL="$(resolve_canonical_path "$PWD" | tr '\\' '/' | sed 's:/*$::')"
if [ -n "${PWD_CANONICAL}" ] && [ "${TARGET_CANONICAL}" = "${PWD_CANONICAL}" ]; then
    echo "[x] ERROR: Target directory cannot be the current working directory!" >&2
    exit 1
fi

# Guard against git repository root, subpaths inside repository, and ancestors containing repository [P1-4]
if [ "${TARGET_CANONICAL}" = "${REPO_CANONICAL}" ]; then
    echo "[x] ERROR: Target directory cannot be the repository root: '${TARGET_DIR}'" >&2
    exit 1
fi
# Reject targets INSIDE the repository (e.g. ./wallet, ./core, subdirectories)
if [[ "${TARGET_CANONICAL}" == "${REPO_CANONICAL}/"* ]]; then
    echo "[x] ERROR: Target directory '${TARGET_DIR}' is INSIDE the repository root! Refusing destructive operation." >&2
    exit 1
fi
# Reject targets that are ANCESTORS of the repository (e.g. /mnt/data when repo is /mnt/data/armax-coin)
if [[ "${REPO_CANONICAL}" == "${TARGET_CANONICAL}/"* ]]; then
    echo "[x] ERROR: Target directory '${TARGET_DIR}' is an ANCESTOR of the repository root! Refusing destructive operation." >&2
    exit 1
fi

# ==============================================================================
# Working Tree Cleanliness Check [P1-3]
# git archive HEAD only exports committed state; refuse if working tree is dirty!
# ==============================================================================
if [ "${DRY_RUN}" -eq 0 ] && [ "${ARMAX_TEST_INJECT_PROMOTION_FAILURE:-0}" != "1" ] && [ "${INJECT_PROMOTION_FAILURE:-0}" != "1" ]; then
    DIRTY_STATUS="$(git -C "${REPO_CANONICAL}" status --porcelain 2>/dev/null || true)"
    if [ -n "${DIRTY_STATUS}" ]; then
        echo "[x] ERROR: Working tree has uncommitted or untracked changes." >&2
        echo "    make_public_snapshot exports 'git archive HEAD' and requires a clean working tree." >&2
        echo "    To prevent publishing a stale or mismatched tree, commit your changes or" >&2
        echo "    clean the working tree before creating a public snapshot." >&2
        echo "    Uncommitted status preview:" >&2
        echo "${DIRTY_STATUS}" | head -n 10 >&2
        exit 1
    fi
fi

HEAD_COMMIT="$(git -C "${REPO_CANONICAL}" rev-parse HEAD 2>/dev/null || echo "UNKNOWN")"
if [ "${HEAD_COMMIT}" = "UNKNOWN" ]; then
    echo "[x] ERROR: Failed to resolve HEAD commit from repository '${REPO_CANONICAL}'." >&2
    exit 1
fi

echo "=================================================="
echo " ARMAXCOIN - CLEAN PUBLIC SNAPSHOT GENERATOR"
echo "=================================================="
echo "Target Directory : ${TARGET_DIR}"
echo "Git User Name    : ${GIT_USER_NAME}"
echo "Git User Email   : ${GIT_USER_EMAIL}"
echo "Export Commit    : ${HEAD_COMMIT}"
echo "Apply / Execute  : ${APPLY}"
echo "Force Overwrite  : ${FORCE}"
echo "Dry Run Mode     : ${DRY_RUN}"
echo "=================================================="

# Check if target exists [P1-5]
TARGET_EXISTS=0
if [ -e "${TARGET_DIR}" ] || [ -e "${TARGET_CANONICAL}" ]; then
    TARGET_EXISTS=1
    if [ "${FORCE}" -ne 1 ]; then
        echo "[x] ERROR: Target directory '${TARGET_DIR}' already exists." >&2
        echo "    To overwrite an existing directory, explicitly pass --force (and --apply to execute)." >&2
        exit 1
    fi
    if [ "${DRY_RUN}" -eq 1 ]; then
        echo "[DRY-RUN] Target directory exists. Will stage into temporary sibling directory and replace target only after successful verification."
    fi
fi

if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[DRY-RUN] Would create temporary staging directory next to target: ${TARGET_DIR}"
    echo "[DRY-RUN] Would export git archive HEAD (${HEAD_COMMIT}) to staging directory"
    echo "[DRY-RUN] Would scrub internal audit references"
    echo "[DRY-RUN] Would scan prohibited files (*.cookie, *.exe, *.db, wallet.json, *.log, scratch/)"
    echo "[DRY-RUN] Would initialize pristine git repo with single commit"
    if [ "${TARGET_EXISTS}" -eq 1 ]; then
        echo "[DRY-RUN] Would atomically replace existing target with verified staging directory"
    else
        echo "[DRY-RUN] Would move verified staging directory to target"
    fi
    echo "[OK] Dry-run completed successfully. No files were modified."
    echo "    To execute real snapshot generation, rerun with '--apply' (and '--force' if overwriting)."
    exit 0
fi

# ==============================================================================
# Isolated Staging Directory Generation [P1-5]
# Never delete an existing destination before export, extraction, sanitization,
# prohibited-file checks, and gitleaks scans have ALL succeeded!
# ==============================================================================
TARGET_PARENT="$(dirname "${TARGET_CANONICAL}")"
mkdir -p "${TARGET_PARENT}"

STAGING_DIR="${TARGET_CANONICAL}.tmp.$$.$(date +%s%N 2>/dev/null || echo $RANDOM)"
rm -rf "${STAGING_DIR}"
mkdir -p "${STAGING_DIR}"

BACKUP_DIR=""
cleanup_staging() {
    local exit_code=$?
    trap - EXIT INT TERM
    if [ -n "${BACKUP_DIR:-}" ] && [ -e "${BACKUP_DIR}" ]; then
        if [ ! -e "${TARGET_CANONICAL}" ]; then
            echo "[!] Staging cleanup trap: restoring backup to ${TARGET_CANONICAL}..." >&2
            mv "${BACKUP_DIR}" "${TARGET_CANONICAL}" 2>/dev/null || true
        fi
    fi
    if [ -n "${STAGING_DIR:-}" ] && [ -d "${STAGING_DIR}" ]; then
        rm -rf "${STAGING_DIR}" 2>/dev/null || true
    fi
    exit ${exit_code}
}
trap cleanup_staging EXIT INT TERM

# 1. Export clean working tree from HEAD via git archive into staging directory
echo "[+] Exporting working tree from Git HEAD into staging directory..."
ARCHIVE_TAR="${STAGING_DIR}/temp_snapshot.tar"
git -C "${REPO_CANONICAL}" archive HEAD -o "${ARCHIVE_TAR}"
tar -xf "${ARCHIVE_TAR}" -C "${STAGING_DIR}"
rm -f "${ARCHIVE_TAR}"

# 2. Remove internal audit files and files containing private emails / local paths in staging directory
echo "[+] Scrubbing internal audit and local machine references..."
rm -f "${STAGING_DIR}/docs/HISTORY_AUDIT.md"

if [ -f "${STAGING_DIR}/docs/CLEANUP_REPO.md" ]; then
    sed -i.bak -E 's|file:///[a-zA-Z]:/[^)]+|/path/to/armax_coin_archive|g' "${STAGING_DIR}/docs/CLEANUP_REPO.md"
    rm -f "${STAGING_DIR}/docs/CLEANUP_REPO.md.bak"
fi

# 3. Prohibited file pattern verification in staging directory
echo "[+] Verifying absence of prohibited files (*.cookie, *.exe, *.db, wallet.json, *.log, scratch/)..."
PROHIBITED_FOUND=0

while IFS= read -r -d '' file; do
    echo "[-] PROHIBITED FILE DETECTED: ${file}"
    PROHIBITED_FOUND=1
done < <(find "${STAGING_DIR}" \( -name "*.cookie" -o -name "*.exe" -o -name "*.db" -o -name "wallet.json" -o -name "*.log" -o -path "*/scratch/*" \) -print0)

if [ "${PROHIBITED_FOUND}" -ne 0 ]; then
    echo "[x] ERROR: Prohibited files detected in public snapshot. Aborting without modifying destination." >&2
    exit 1
fi
echo "[OK] Prohibited file scan passed: 0 prohibited files found."

# 4. Initialize fresh Git repository with single pristine commit in staging directory
echo "[+] Initializing pristine Git repository on branch 'main'..."
(
    cd "${STAGING_DIR}"
    git init -b main
    git config user.name "${GIT_USER_NAME}"
    git config user.email "${GIT_USER_EMAIL}"
    git add .
    git commit -m "Armaxcoin v1.0.0 (Public Open Testnet - Experimental)"
)

# 5. Run gitleaks secret detection across the clean repository in staging directory
echo "[+] Running gitleaks secret scan on snapshot repository..."
GITLEAKS_CMD="gitleaks"
if ! command -v "${GITLEAKS_CMD}" &> /dev/null; then
    if [ -x "${HOME}/go/bin/gitleaks" ]; then
        GITLEAKS_CMD="${HOME}/go/bin/gitleaks"
    else
        echo "[!] Warning: gitleaks not found in PATH or ~/go/bin. Skipping gitleaks scan."
        GITLEAKS_CMD=""
    fi
fi

if [ -n "${GITLEAKS_CMD}" ]; then
    (
        cd "${STAGING_DIR}"
        "${GITLEAKS_CMD}" detect --source . --verbose --exit-code 1
    )
    echo "[OK] Gitleaks secret verification passed!"
elif [ "${SKIP_GITLEAKS}" -eq 1 ]; then
    echo "********************************************************************************"
    echo "[!] WARNING: Gitleaks not found. Skipping secret scan due to explicit --skip-gitleaks flag!"
    echo "    SECURITY RISK: Snapshot is NOT verified against credential/key leakage!"
    echo "********************************************************************************"
else
    echo "********************************************************************************"
    echo "[x] ERROR: 'gitleaks' executable not found in PATH or ~/go/bin/gitleaks."
    echo "    Snapshot verification failed-closed [L4-10-d]."
    echo "    Install gitleaks (e.g. 'go install github.com/gitleaks/gitleaks/v8@latest') or"
    echo "    pass '--skip-gitleaks' to explicitly bypass this check."
    echo "********************************************************************************"
    exit 1
fi

# 6. Replace/rename to destination only after successful checks and authorization [P1-5, P2]
echo "[+] Replacing destination with validated snapshot..."
if [ "${TARGET_EXISTS}" -eq 1 ]; then
    BACKUP_DIR="${TARGET_CANONICAL}.old.$$.$(date +%s%N 2>/dev/null || echo $RANDOM)"
    mv "${TARGET_CANONICAL}" "${BACKUP_DIR}"

    # Simulated promotion failure injection [P2]
    if [ "${ARMAX_TEST_INJECT_PROMOTION_FAILURE:-0}" = "1" ] || [ "${INJECT_PROMOTION_FAILURE:-0}" = "1" ]; then
        echo "[x] TEST INJECTION: Simulating failure during staging directory promotion!" >&2
        echo "[!] Rolling back: restoring backup to ${TARGET_CANONICAL}..." >&2
        mv "${BACKUP_DIR}" "${TARGET_CANONICAL}"
        BACKUP_DIR=""
        rm -rf "${STAGING_DIR}" 2>/dev/null || true
        exit 1
    fi

    # Attempt move of staging directory to destination
    if ! mv "${STAGING_DIR}" "${TARGET_CANONICAL}"; then
        echo "[x] ERROR: Failed to promote staging directory to destination. Rolling back..." >&2
        if [ -e "${TARGET_CANONICAL}" ]; then
            rm -rf "${TARGET_CANONICAL}"
        fi
        mv "${BACKUP_DIR}" "${TARGET_CANONICAL}"
        BACKUP_DIR=""
        rm -rf "${STAGING_DIR}" 2>/dev/null || true
        exit 1
    fi

    # Confirm the new target before removing backup [P2]
    if [ -d "${TARGET_CANONICAL}" ] && [ -f "${TARGET_CANONICAL}/go.mod" ]; then
        rm -rf "${BACKUP_DIR}"
        BACKUP_DIR=""
    else
        echo "[x] ERROR: Target confirmation failed after promotion. Rolling back..." >&2
        rm -rf "${TARGET_CANONICAL}"
        mv "${BACKUP_DIR}" "${TARGET_CANONICAL}"
        BACKUP_DIR=""
        rm -rf "${STAGING_DIR}" 2>/dev/null || true
        exit 1
    fi
else
    if [ "${ARMAX_TEST_INJECT_PROMOTION_FAILURE:-0}" = "1" ] || [ "${INJECT_PROMOTION_FAILURE:-0}" = "1" ]; then
        echo "[x] TEST INJECTION: Simulating failure during staging directory promotion!" >&2
        rm -rf "${STAGING_DIR}" 2>/dev/null || true
        exit 1
    fi
    mv "${STAGING_DIR}" "${TARGET_CANONICAL}"
fi

# Staging directory successfully transferred; disable trap cleanup
trap - EXIT INT TERM

FINAL_COMMIT="$(git -C "${TARGET_CANONICAL}" rev-parse HEAD 2>/dev/null || echo "${HEAD_COMMIT}")"
FINAL_BRANCH="$(git -C "${TARGET_CANONICAL}" branch --show-current 2>/dev/null || echo "main")"

echo "=================================================="
echo " SUCCESS: Clean public snapshot ready at:"
echo "    ${TARGET_DIR}"
echo "    Commit : ${FINAL_COMMIT}"
echo "    Branch : ${FINAL_BRANCH}"
echo "=================================================="
