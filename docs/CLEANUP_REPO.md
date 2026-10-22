# Repository Hygiene & Public Release Clean-History Procedure

## 1. Repository Architecture & Renaming Strategy [L4-2]

To maintain full compliance with the Go module declaration (`module github.com/acxxcxx/armax-coin`), while completely shielding the public from internal development commits and legacy ephemeral blobs:

1. **Archive Old Private Repository:**
   - In GitHub repository settings, rename or archive the existing private repository as **`armax_coin_archive`** (or archive in place).
   - Keep it private as an internal audit and development archive.

2. **Create Fresh Public Repository:**
   - Create a brand-new public repository named **`armax-coin`** under `github.com/acxxcxx/armax-coin`.
   - The public repository will contain a single pristine initial release commit with 100% clean history, zero sensitive files, and full compliance with `go.mod`.

---

## 2. Automated Clean Public Snapshot Generation

Two automated snapshot scripts are provided in the repository to generate, scrub, verify, and initialize the public repository in a single command:
- **Bash / Linux / macOS:** [`scripts/make_public_snapshot.sh`](../scripts/make_public_snapshot.sh)
- **PowerShell / Windows:** [`scripts/make_public_snapshot.ps1`](../scripts/make_public_snapshot.ps1)

### Execution on Linux / macOS:
```bash
# Generate clean snapshot into target folder
./scripts/make_public_snapshot.sh /tmp/armax-coin "acxxcxx" "acxxcxx@users.noreply.github.com"

# Push to the newly created public GitHub repository
cd /tmp/armax-coin
git remote add origin https://github.com/acxxcxx/armax-coin.git
git push -u origin main --tags
```

### Execution on Windows (PowerShell):
```powershell
# Generate clean snapshot into target folder
.\scripts\make_public_snapshot.ps1 -TargetDir "$env:TEMP\armax-coin" -GitUserName "acxxcxx" -GitUserEmail "acxxcxx@users.noreply.github.com"

# Push to the newly created public GitHub repository
cd "$env:TEMP\armax-coin"
git remote add origin https://github.com/acxxcxx/armax-coin.git
git push -u origin main --tags
```

### What the Snapshot Script Automates:
1. **`git archive HEAD`**: Exports only tracked files from HEAD without Git metadata or untracked local files.
2. **Scrubbing Internal Files**: Deletes `docs/HISTORY_AUDIT.md` and replaces local paths.
3. **Prohibited File Validation**: Strictly verifies that zero `*.cookie`, `*.exe`, `*.db`, `wallet.json`, `*.log`, or `scratch/` directories exist. Aborts (`exit != 0`) if any prohibited file is detected.
4. **Git Initialization**: Initializes `git init -b main`, sets `user.name` and `user.email` (GitHub noreply), and creates a single initial commit: `"Armaxcoin v1.0.0 (public testnet)"`.
5. **Gitleaks Secret Scan**: Executes `gitleaks detect` on the snapshot to mathematically guarantee zero secret or credential leakage.

---

## 3. Mandatory Pre-Flight Verification Checklist

Before publishing, verify the following gates:

| Verification Gate | Command | Expected Result |
|---|---|---|
| **Compilation** | `go build ./...` | Exit code 0 |
| **Go Vet** | `go vet -tags testhooks ./...` | Exit code 0 |
| **Race Detector** | `go test -tags testhooks -race -count=1 ./...` | All tests PASS in CI |
| **Static Analysis** | `staticcheck -tags testhooks ./...` | 0 errors |
| **Vulnerability Audit** | `govulncheck ./...` | 0 vulnerabilities |
| **Default Passphrase Check** | Verify legacy default passphrase string absent | 0 matches (Empty) |
| **Go Version Alignment** | `go version` | Go 1.27+ |
| **Direct Dependencies** | `go.mod` | No direct dependencies marked as `// indirect` |
| **Windows GUI Resource** | `gui/rsrc_windows_amd64.syso` | Present and verifiable via `rsrc` |
| **Snapshot Gitleaks** | Automated in `make_public_snapshot` | Exit code 0 |
