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

## Mandatory independent evidence before mainnet

1. An independent, public cryptography and consensus audit covering block
   validation, state transitions, ML-DSA integration, storage recovery, P2P,
   RPC, and wallet code; all findings must be resolved or formally accepted.
2. A sustained adversarial public testnet report covering partition, eclipse,
   reorg, malformed-message, database-corruption, and upgrade recovery tests.
3. Reproducible release artifacts, signed provenance/SBOM, dependency
   vulnerability scan, race detector, fuzzing, and platform build evidence.
4. An incident-response runbook, responsible-disclosure contact, key-rotation
   procedure, and a documented upgrade/rollback policy.
5. A hardware-signing design review. Private keys must remain outside the GUI
   process; device transport, address confirmation, transaction display, and
   signing-domain separation require dedicated implementation and audit.

## Hardware wallet status

Hardware wallet support is **not implemented**. It must not be represented as
available or equivalent to encrypted software keystores. Until the dedicated
signer interface and external audit exist, operators should treat this wallet
as a testnet software wallet only.
