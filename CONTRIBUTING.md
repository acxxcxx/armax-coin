# Contributing to Armaxcoin (`armax-coin`)

Thank you for your interest in contributing to Armaxcoin. Armaxcoin is an experimental Layer-1 Proof-of-Work blockchain incorporating NIST FIPS 204 post-quantum digital signatures (ML-DSA-65) and the ArmaxHash v2 Stream-Fast algorithm.

Because consensus-critical systems require meticulous attention to correctness, determinism, and security, all contributions must adhere to the engineering standards and quality gates described in this guide.

---

## 1. Code of Conduct

All contributors and community participants are expected to adhere to our [Code of Conduct](CODE_OF_CONDUCT.md). Please treat all participants with respect, professionalism, and constructive collaboration.

---

## 2. Experimental Status & Security Boundaries

- **Experimental Software:** Armaxcoin is currently deployed in an open testnet phase (`ARMX-TESTNET-1`). It has not undergone an independent third-party security audit.
- **No Financial Guarantees:** Testnet coins (`ARMX`) have no monetary value, no guarantee of conversion, and no financial utility.
- **Vulnerability Reporting:** Do **NOT** open public GitHub issues for security vulnerabilities, consensus bypasses, or denial-of-service vectors. Follow the responsible disclosure procedures defined in [SECURITY.md](SECURITY.md).

---

## 3. Getting Started

### Prerequisites

- **Go:** Version **1.27 or higher** (strict requirement due to standard library `crypto/mldsa` post-quantum support).
- **Git:** Standard Git client.
- **Operating Systems Supported:**
  - Linux (`amd64`, `arm64`)
  - Windows 10/11 / Windows Server (`amd64`)
  - macOS Apple Silicon (`arm64`)
- **Linters & Tooling (Optional for local development, enforced in CI):**
  - `staticcheck` (pinned version v0.8.2)
  - `govulncheck` (pinned version v1.9.0)

### Clone & Build

```bash
git clone https://github.com/acxxcxx/armax-coin.git
cd armax-coin

# Verify Go toolchain
go version

# Compile all binaries
go build ./...

# Run the test suite
go test -count=1 ./...
```

---

## 4. Development Workflow & Guidelines

### Branching Strategy

1. Fork the repository on GitHub.
2. Create a focused topic branch from `main`:
   ```bash
   git checkout -b feature/my-feature-name
   ```
3. Keep commits atomic, well-scoped, and accompanied by corresponding tests.

### Commit Conventions

All commit messages must be written in English and follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` A new feature or user-facing functionality.
- `fix:` A bug fix or defect resolution.
- `docs:` Documentation additions or corrections.
- `refactor:` Code refactoring without behavioral changes.
- `test:` Adding or improving test suites.
- `build:` Build tooling, packaging scripts, or dependency changes.
- `ci:` GitHub Actions workflow modifications.
- `security:` Security hardening or vulnerability remediations.

Example:
```text
feat(miner): add thread auto-detection based on runtime.NumCPU
fix(p2p): prevent goroutine leak on handshake timeout
docs(mining): clarify ArmaxHash memory requirements
```

### Consensus & Determinism Rules

Any change to packages `core/`, `armaxhash/`, or consensus-critical logic in `node/` must satisfy:

1. **Strict Determinism:** No non-deterministic iteration order over Go maps when calculating hashes, Merkle trees, block headers, or transaction lists.
2. **Network Profile Isolation:** Changes must never hardcode mainnet or testnet constants outside of `core/network.go`.
3. **No Breaking Wire Changes:** P2P wire formats and RPC JSON schemas must maintain backward compatibility or be versioned explicitly.
4. **Zero Float Usage in Economics:** All financial calculations (balances, rewards, fees, totals) must strictly use integer Armoids (`1 ARMX = 100,000,000 Armoids`) with 64-bit integer arithmetic. Floating-point types are strictly forbidden in consensus paths.
5. **No Blind Renames:** Do not rename protocol identifiers, wire commands, network IDs (`ARMX-TESTNET-1`), database keys, or serialization tags.

---

## 5. Mandatory Verification Gates

Before submitting a Pull Request, you must run and pass all verification gates:

```bash
# 1. Formatting
gofmt -l .

# 2. Go Vet (including test hooks)
go vet -tags testhooks ./...

# 3. Comprehensive Unit & Integration Tests
go test -tags testhooks -count=1 -timeout 300s ./...

# 4. Personal Data & Repository Hygiene Gate
go test -v -run TestNoPersonalDataInTree .

# 5. Static Analysis (if installed)
staticcheck -tags testhooks ./...

# 6. Known Vulnerability Check (if installed)
govulncheck ./...
```

---

## 6. Pull Request Review Process

1. **Automated CI:** Every PR triggers `.github/workflows/ci.yml`, running vet, race detection, cross-compilation for Linux and Windows, static analysis, and hygiene tests.
2. **Review Criteria:**
   - Code correctness, readability, and idiomatic Go practices.
   - Test coverage for positive paths, error handling, edge cases, and failure modes.
   - Absence of concurrency race conditions, deadlocks, and resource leaks.
   - Preservation of existing security invariants.
3. **Maintainer Approval:** All PRs require review and approval from repository maintainers before merging.

---

## 7. Developer Certificate of Origin (DCO) & Licensing

All contributions to Armaxcoin are submitted under the terms of the project's [MIT License](LICENSE). By submitting a pull request or patch, you certify that you have the legal right to submit the contribution under this license, conforming to the Developer Certificate of Origin (DCO):

```text
Developer Certificate of Origin
Version 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Please add a `Signed-off-by:` trailer to each commit message (e.g. `git commit -s`).

---

## 8. Contributor Recognition

Contributions to Armaxcoin are entirely voluntary. Contributors are credited in [CONTRIBUTORS.md](CONTRIBUTORS.md) and official release notes.

> [!IMPORTANT]
> **No Financial or Token Compensation:**
> Armaxcoin is an experimental Layer-1 open-source protocol on public testnet (`ARMX-TESTNET-1`). Testnet coins (`ARMX`) have no monetary value, no promise of conversion, and no investment utility. No tokens, airdrops, retroactive allocations, or financial bounties are offered or implied.

