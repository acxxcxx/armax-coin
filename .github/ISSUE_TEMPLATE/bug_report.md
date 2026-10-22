---
name: Bug Report
about: Create a report to help us reproduce and fix a bug in Armaxcoin
title: "[BUG]: "
labels: ["bug", "needs-triage"]
assignees: ""
---

> [!IMPORTANT]
> **Security Notice:** If you have discovered a potential security vulnerability, consensus bypass, or remote denial-of-service vector, **DO NOT** file a public issue. Please follow our responsible disclosure procedure in [SECURITY.md](https://github.com/acxxcxx/armax-coin/blob/main/SECURITY.md) via [GitHub Private Vulnerability Reporting](https://github.com/acxxcxx/armax-coin/security/advisories/new).

### Describe the Bug
A clear and concise description of what the bug is.

### Affected Component
Please check all that apply:
- [ ] `armcoind` (Full Node Daemon / P2P / RPC)
- [ ] `armminer` (CPU Proof-of-Work Miner)
- [ ] `armwallet` (CLI Wallet)
- [ ] `armsend` (CLI Transaction Dispatcher)
- [ ] `armgui` (Desktop GUI Wallet)
- [ ] `consensus / armaxhash` (Core consensus rules)
- [ ] Documentation / Build scripts

### Environment Details
- **OS & Architecture:** [e.g., Ubuntu 24.04 x86_64, Windows 11 amd64, macOS 14.5 arm64]
- **Go Version:** [e.g., `go version go1.27 darwin/arm64`]
- **Binary Version / Git Commit:** [e.g., `v1.0.0` or output of `git rev-parse --short HEAD`]
- **Active Network Profile:** [e.g., `ARMX-TESTNET-1`]

### Steps to Reproduce
1. Run command '...'
2. Pass arguments '....'
3. See error

### Expected Behavior
A clear and concise description of what you expected to happen.

### Actual Behavior / Logs
```text
Paste sanitized console output, stack traces, or log entries here.
DO NOT include private keys, seed phrases, or sensitive credentials!
```

### Additional Context
Add any other context about the problem here (e.g., network conditions, peer count, memory usage).
