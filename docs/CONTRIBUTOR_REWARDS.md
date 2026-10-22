# ARMAXcoin Testnet Contributor Reward Program (Proposal)

> [!NOTE]
> **PROPOSAL -- FOR COMMUNITY DISCUSSION ONLY**
>
> This document outlines a potential framework for recognizing contributions to the ARMAXcoin public testnet. No rewards or mainnet coin allocations have been approved or guaranteed. Participation does not create an entitlement to future rewards. Any future program will be announced separately, subject to project readiness and maintainer approval.

---

## 1. Overview & Community Goals

The goal of this proposed program is to encourage open collaboration, testing, and development across the ARMAXcoin public testnet (`ARMX-TESTNET-1`).

By participating, community members help harden the network prior to any future mainnet considerations. Key areas where community participation is most valuable include:

- **Network Reliability:** Testing peer-to-peer connectivity, TLS 1.3 handshakes, and peer discovery across diverse consumer and cloud environments.
- **Consensus & PoW Testing:** Evaluating the memory-hard ArmaxHash algorithm and LWMA-1 difficulty retargeting under varying hashrate levels.
- **Post-Quantum Cryptography:** Benchmarking NIST FIPS 204 (ML-DSA-65) signing and verification performance across multiple hardware architectures, including x86_64 servers and ARM single-board computers (Raspberry Pi).
- **Security & Stability:** Discovering and responsibly reporting consensus edge cases, state handling bugs, memory leaks, or race conditions.
- **Documentation & Tooling:** Improving setup guides, deployment scripts, multi-platform build instructions, and developer documentation.

---

## 2. Participation Guidelines

The program welcomes developers, node operators, miners, and security researchers who contribute in good faith. Participants are expected to:

1. Follow the project's [Code of Conduct](../CODE_OF_CONDUCT.md) and technical guidelines.
2. Collaborate constructively without attempting to disrupt testnet infrastructure, flood peer connections, or artificially manipulate metrics.
3. Provide publicly or cryptographically verifiable evidence for any claimed contributions.
4. Provide a valid post-quantum public address (`armx1...`) generated through official software when requested.
5. **Never share private keys or 24-word seed phrases.** Project maintainers will never ask for private credentials.

*Note: Running testnet software or opening issues does not automatically qualify a participant for rewards. All contributions are reviewed based on verifiable technical merit.*

---

## 3. Recognized Contribution Areas

Contributions are recognized across six main technical categories:

1. **Independent Node Operations:**
   Operating a stable, protocol-compliant testnet node (`armcoind`) with verified peer synchronization and high availability.
2. **Testnet Mining & Hashrate Testing:**
   Running `armminer` to validate ArmaxHash execution, verify block creation, and test difficulty transitions across varying network hashrates.
3. **Security Research & Bug Disclosures:**
   Discovering, reproducing, and privately disclosing potential security vulnerabilities in accordance with [SECURITY.md](../SECURITY.md).
4. **Core Protocol & Tooling Contributions:**
   Submitting pull requests that fix bugs, improve concurrency, optimize resource usage, or expand automated test coverage.
5. **Documentation & DevOps:**
   Writing setup tutorials, maintaining systemd or container configurations, translating guides, or improving troubleshooting docs.
6. **Stress Testing & Benchmarking:**
   Running verifiable network partition simulations, mempool load testing, or performance benchmarks on constrained hardware.

---

## 4. Verification & Evidence

To ensure fairness, contributions must be backed by objective evidence. Self-submitted screenshots or unverified text logs are not sufficient.

| Category | Primary Verification Method | Evidence Required | Privacy Standard |
|---|---|---|---|
| **Public Node Operation** | Inbound dial verification and P2P peer-store status | Public IP or hostname, peer ID, TLS fingerprint | No internal machine metrics collected |
| **NAT / Outbound Node** | Continuous outbound synchronization observed by peers | Outbound session telemetry, regular block sync | Local network details remain private |
| **Testnet Mining** | On-chain block coinbase verification | Verified block height and coinbase address (`armx1...`) | On-chain verification only |
| **Security Research** | Maintainer triage via GitHub Security Advisories | Detailed report and reproducible demonstration | Kept private until resolved |
| **Code Contributions** | Merged pull requests in repository history | GitHub PR link, commit SHA | Standard open-source git history |
| **Stress Testing** | Independent reproduction on test harnesses | Reproducible scripts, test environment profile | Sanitized configuration only |

Telemetry collected during verification is retained solely for audit purposes during the testnet and is safely removed afterward. No personal identification documents, financial information, or proprietary server data are ever collected.

---

## 5. Proposed Scoring Model

To balance broad participation with fair recognition for high-impact technical work, a hybrid model is proposed:

- **Base Participation:** A portion of recognition is allocated among verified node operators and miners meeting minimum uptime and protocol compliance thresholds.
- **Merit & Technical Impact:** Additional weighting is awarded for verified technical achievements based on impact and complexity.

### Illustrative Scoring Rubric:

| Contribution Level | Point Range | Impact Criteria |
|---|---|---|
| **Critical Security Finding** | 200 - 500 pts | Remote execution, consensus split, state corruption (CVSS Critical/High) |
| **High Security / Protocol Defect** | 100 - 200 pts | Denial-of-service, memory exhaustion, peer partition vulnerability |
| **Core Protocol Enhancement** | 50 - 120 pts | Architectural improvements, significant optimizations, major bug fixes |
| **Tooling & Test Automation** | 25 - 60 pts | Multi-node test harnesses, cross-compilation fixes, CLI improvements |
| **Verified Public Node (High Uptime)** | 15 - 35 pts | Continuous public availability ($\ge 95\%$ observed uptime) |
| **Verified NAT Node (Active Sync)** | 5 - 15 pts | Reliable outbound relay, transaction processing, and peer discovery |
| **Active Testnet Mining** | 10 - 30 pts | Consistent mining of valid blocks on the canonical chain |
| **Technical Documentation** | 10 - 25 pts | High-quality setup guides, tutorials, or devops scripts |

### Scoring Principles:
- **Verified Outcomes:** Points are awarded only after contributions are verified and merged or resolved.
- **No Double-Counting:** The same contribution cannot be credited across multiple overlapping categories (e.g., a bug report and its corresponding pull request are evaluated as a single contribution).
- **Impartial Review:** Maintainers and reviewers do not score their own contributions or those of affiliated accounts.

---

## 6. Sybil Resistance & Fairness

To protect against artificial inflation while remaining welcoming to genuine contributors:

1. **Subnets and Hosting Providers:**
   Multiple nodes originating from the same subnet or cloud provider (e.g., Hetzner, DigitalOcean, AWS, OVH) are reviewed using multi-factor checks (connection stability, peer interaction, software behavior) rather than being automatically disqualified.
2. **Inclusivity for NAT Nodes:**
   Participants unable to open inbound ports can still take part in client-side testing, synchronization, and mining, and are evaluated under appropriate outbound categories.
3. **Established Accounts:**
   Code contributions and research submissions should come from established accounts to prevent automated spam submissions.

---

## 7. Review, Feedback & Dispute Resolution

1. **Provisional Summary:** At the conclusion of the testnet evaluation, maintainers plan to publish a provisional list of verified contributions and points.
2. **Feedback Window:** Contributors will have 7 calendar days to submit factual corrections or additional technical evidence (e.g., commit signatures, signed handshake proofs).
3. **Fair Review:** Where possible, appeals will be reviewed by a different maintainer than the original reviewer.
4. **Distinguishing Errors from Abuse:** Unintentional configuration mistakes will simply result in point adjustments. Intentional attacks, Sybil farming, or falsified reports will lead to disqualification.
5. **Network Protection:** Maintainers retain the right to disconnect or block nodes that actively attack peers or degrade network performance.

---

## 8. Privacy, Security & Scam Prevention

> [!CAUTION]
> **SAFETY REMINDER: NEVER REVEAL PRIVATE KEYS OR SEED PHRASES**
> - The ARMAXcoin project will **NEVER** request your private key, seed phrase, or wallet password.
> - Anyone asking for your recovery words, private keys, or wallet backup files is an **impostor**.

- **Public Addresses Only:** When officially requested, submit only your public receiving address (`armx1...`) generated by official ARMAXcoin tools (`armwallet` or `armgui`).
- **Proof of Ownership:** Maintainers may ask for a message signed by your public key to confirm address ownership without compromising security.
- **Privacy Minimization:** Address collection will use secure, authenticated channels with no unnecessary personal data required.

---

## 9. Summary of Proposed Program Parameters

| Program Parameter | Proposed Definition | Status |
|---|---|---|
| **Program Framework** | Hybrid Participation and Merit Model | Proposed |
| **Total Reward Pool** | To be determined | Not yet approved |
| **Individual Reward Cap** | To be determined | Not yet approved |
| **Scoring System** | Technical contribution and impact | Proposed |
| **Sybil Resistance** | Multi-factor participation verification | Proposed |
| **Distribution Schedule** | To be determined after mainnet planning | Tentative |

---

## 10. Important Considerations

1. **Experimental Software:** ARMAXcoin is experimental open-source software under active development. The testnet may experience unexpected halts, hard forks, or resets.
2. **No Guarantee of Future Value:** ARMAXcoin (ARMX) is an experimental open-source protocol. Testnet coins have no economic value and cannot be redeemed for fiat currency or other assets.
3. **Independent Program Announcement:** Any future reward program requires formal maintainer approval, economic modeling, and a separate official announcement. If a mainnet launch does not occur, no distribution will take place.
4. **Participant Responsibilities:** Contributors are independently responsible for understanding and complying with any local legal and tax obligations related to open-source contributions.

---
*Maintained under the ARMAXcoin Documentation Suite (`github.com/acxxcxx/armax-coin`).*
