# Mainnet Readiness Gate

This repository is testnet software until every gate below is evidenced in a
release record. No feature flag, configuration setting, or successful local
test may be used to waive a gate.

## Security baseline enforced by the node

- RPC bound outside loopback requires explicit opt-in, HTTP Basic
  authentication, and configured TLS certificate/key files. The daemon refuses
  unsafe public RPC configurations.
- P2P defaults to TLS 1.3 plus TOFU pinning. Plaintext fallback requires an
  explicit non-default configuration suitable only for isolated legacy test
  networks.
- A damaged P2P identity file is fail-closed; operators must restore it from a
  protected backup rather than silently receiving a new network identity.
- Wallet keystores and backups are written through synced temporary files and
  committed atomically with owner-only permissions where the platform supports
  them.

## Gate status (update with evidence links on every release)

| # | Gate | Status |
|---|---|---|
| 1 | Open independent review of consensus, ArmaxHash v2, Armax-ASERT-AW, `TxBinaryV1`, and ML-DSA-65 code | Not started |
| 2 | Sustained adversarial public testnet report | In progress (nightly soak CI; no published report) |
| 3 | Reproducible artifacts, signed provenance/SBOM, vulnerability scan, race detector, fuzzing | Partial: `staticcheck`, `govulncheck`, `-race`, and fuzzing run in CI; no signed SBOM or provenance yet |
| 4 | Incident-response runbook, key-rotation procedure, upgrade/rollback policy | Partial: disclosure contact in `SECURITY.md`; runbook and rollback policy pending |
| 5 | Hardware-signing design review | Not implemented |

## Mandatory independent evidence before mainnet

1. Open independent review of consensus and cryptography code: all consensus
   state transition logic, ArmaxHash v2, difficulty adjustment (`Armax-ASERT-AW`),
   `TxBinaryV1` size accounting, NIST FIPS 204 ML-DSA-65 signature verification,
   storage recovery, P2P, RPC, and wallet code is public for peer review and
   adversarial testing, security findings are tracked through the responsible disclosure
   process (exploitable vulnerabilities handled privately until remediation;
   non-sensitive and resolved findings documented publicly), and all known Critical and
   High-severity issues must be resolved and verifiable before Mainnet
   activation. Any future bridge or contract will go through the same open
   independent review before holding real funds; paid commercial audits are not
   required and may be pursued optionally when grants or funding become available.
2. A sustained adversarial public testnet report covering partition, eclipse,
   reorg, malformed-message, database-corruption, and upgrade recovery tests.
3. Reproducible release artifacts, signed provenance/SBOM, dependency
   vulnerability scan, race detector, fuzzing, and platform build evidence.
4. An incident-response runbook, responsible-disclosure contact, key-rotation
   procedure, and a documented upgrade/rollback policy.
5. A hardware-signing design review. Private keys must remain outside the GUI
   process; device transport, address confirmation, transaction display, and
   signing-domain separation require dedicated implementation and open independent review.

## Hardware wallet status

Hardware wallet support is **not implemented**. It must not be represented as
available or equivalent to encrypted software keystores. Until the dedicated
signer interface and open independent review exist, operators should treat this wallet
as a testnet software wallet only.
