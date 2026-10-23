[center]
[size=24pt][b]ARMAXCOIN (ARMX)[/b][/size]
[size=14pt][b]Proof-of-Work Layer-1 Blockchain with Post-Quantum Signatures (NIST FIPS 204, ML-DSA-65)[/b][/size]
[size=12pt][b]Experimental Public Open Testnet Release Announcement (v1.0.1)[/b][/size]
[/center]

[hr]

[b]Official Links:[/b]
[*] [b]Source Code Repository:[/b] [url=https://github.com/acxxcxx/armax-coin]https://github.com/acxxcxx/armax-coin[/url]
[*] [b]Releases & Assets:[/b] [url=https://github.com/acxxcxx/armax-coin/releases]https://github.com/acxxcxx/armax-coin/releases[/url]
[*] [b]Git Tag Reference:[/b] [url=https://github.com/acxxcxx/armax-coin/tree/v1.0.1]refs/tags/v1.0.1[/url]
[*] [b]Documentation Suite:[/b] [url=https://github.com/acxxcxx/armax-coin#authoritative-documentation-index]Documentation Index[/url]
[*] [b]License:[/b] MIT Open Source License

[hr]

[b][size=14pt]1. Testnet Purpose & Scope[/size][/b]
The Armaxcoin Public Testnet ([font=monospace]ARMX-TESTNET-1[/font]) is an open, decentralized testing network for node operators, miners, developers, and security researchers to test and validate:
[*] Quantum-resistant digital signatures ([b]NIST FIPS 204 ML-DSA-65[/b]) in Layer-1 consensus transactions.
[*] Memory-hard CPU Proof-of-Work mining ([b]ArmaxHash[/b]: Argon2id 2 MB + Blake2b + SHA-256) activated from block #520.
[*] Responsive difficulty retargeting via [b]LWMA-1[/b] ($N=60$ sliding evaluation window).
[*] P2P mesh transport encrypted with mandatory [b]TLS 1.3[/b] and [b]TOFU certificate pinning[/b].

[color=red][b]CRITICAL EXPERIMENTAL TESTNET DISCLAIMER:[/b][/color]
[list]
[*] [b]Zero Economic Value:[/b] Testnet coins ([font=monospace]ARMX[/font]) carry [b]NO monetary value[/b], no investment utility, and no guarantee of exchange listing or token conversion.
[*] [b]Unaudited Codebase:[/b] The consensus engine, ArmaxHash implementation, and cryptographic integration are experimental and have [b]NOT yet undergone an independent third-party security audit[/b].
[*] [b]State Reset Possibility:[/b] As an active development testnet, blockchain state may be reset if critical protocol flaws are discovered.
[*] [b]Mainnet Status:[/b] Mainnet is [b]locked in source[/b] ([font=monospace]core.MainnetLaunched = false[/font]). Invoking [font=monospace]-network mainnet[/font] is strictly rejected by all binaries.
[/list]

[hr]

[b][size=14pt]2. Technical Specifications[/size][/b]
[list]
[*] [b]Network Identifier:[/b] [font=monospace]ARMX-TESTNET-1[/font] (isolated from mainnet)
[*] [b]Wire Magic:[/b] [font=monospace]ARMX_TEST[/font]
[*] [b]P2P Port:[/b] [b]19335[/b] (Public TCP peer gossip mesh)
[*] [b]RPC Daemon Port:[/b] [b]19333[/b] (Bound strictly to loopback [font=monospace]127.0.0.1[/font] by default)
[*] [b]Block Header PoW Algorithm:[/b]
    - [b]Blocks 0 .. 519:[/b] Double-SHA256 (SHA-256d bootstrap phase)
    - [b]Blocks 520+ (Active):[/b] [b]ArmaxHash[/b] (Memory-hard Argon2id 2,048 KiB, $t=1, p=1$ chained with Blake2b-512 and SHA-256)
[*] [b]Digital Signatures:[/b] NIST FIPS 204 (ML-DSA-65) Post-Quantum Module-Lattice Signatures (pure, non-hybrid)
[*] [b]Target Block Time:[/b] 120 seconds (2 minutes)
[*] [b]Difficulty Retargeting:[/b] Continuous LWMA-1 ($N=60$ sliding evaluation window)
[*] [b]Initial Block Reward:[/b] 60.00000000 ARMX (6,000,000,000 Armoids)
[*] [b]Halving Interval:[/b] Every 1,050,000 blocks (~4.0 years on 120s cadence)
[*] [b]Maximum Hard Cap:[/b] 126,000,000 ARMX (12,600,000,000,000,000 Armoids, 8 decimal places)
[*] [b]Premine / Founder Allocation:[/b] [b]0% (Zero Premine, Zero ICO, Zero Developer Tax)[/b]
[*] [b]Genesis Block Allocation:[/b] 60 ARMX burned to unspendable zero address ([font=monospace]armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd[/font])
[*] [b]Genesis Block Hash:[/b] [font=monospace]0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b[/font]
[*] [b]Genesis Merkle Root:[/b] [font=monospace]808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d[/font]
[*] [b]Genesis Timestamp:[/b] 1710000001 (Nonce: 40,272,237)
[*] [b]ArmaxHash Target Ceiling (Testnet):[/b] [font=monospace]000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff[/font] (Difficulty 3 bootstrap)
[*] [b]Coinbase Maturity:[/b] 10 confirmations on Testnet (100 confirmations on Mainnet)
[*] [b]Max Reorganization Depth:[/b] 720 blocks (~24 hours finality boundary)
[*] [b]Active DNS Seed Nodes:[/b]
[code]
testnet-seed1.armax-coin.cloud:19335  (IP: 202.10.34.180)
testnet-seed2.armax-coin.cloud:19335  (IP: 139.162.46.17)
testnet-seed3.armax-coin.cloud:19335  (IP: 202.10.34.180)
[/code]
[/list]

[hr]

[b][size=14pt]3. Measured Transaction & Wire Sizes[/size][/b]
Armaxcoin incorporates NIST FIPS 204 (ML-DSA-65) post-quantum signatures for every user transaction:
[list]
[*] [b]Public Key Size:[/b] 1,952 bytes (3,904 hex characters)
[*] [b]Signature Size:[/b] 3,309 bytes (6,618 hex characters)
[*] [b]Raw Cryptographic Payload:[/b] ~5,385 bytes per transaction
[*] [b]Total JSON Wire Format:[/b] ~10,761 bytes per transaction
[*] [b]Block Size Limit:[/b] 2,000,000 bytes (2 MB)
[*] [b]Measured Block Capacity:[/b] Up to 185 user transactions per block (+1 coinbase transaction = 186 total, ~1,990,513 bytes)
[*] [b]Transaction Throughput:[/b] ~1.54 transactions per second (at 120s block time)
[/list]

[hr]

[b][size=14pt]4. Quickstart: Building from Source & Running on Testnet[/size][/b]

[b]Prerequisites:[/b]
[*] [b]Go Compiler:[/b] Go 1.27 or higher (required for native [font=monospace]crypto/mldsa[/font] standard library package)
[*] [b]Git[/b]

[b]Step 1 -- Clone the Repository & Compile:[/b]
[code]
git clone https://github.com/acxxcxx/armax-coin.git
cd armax-coin

# Verify Go toolchain
go version

# Compile all binaries
go build -o armcoind main.go
go build -o armwallet ./cli/armwallet
go build -o armsend ./cli/armsend
go build -o armminer miner/miner.go
[/code]
[i]On Windows, run: [font=monospace].\build-all.ps1[/font][/i]

[b]Step 2 -- Create an Encrypted Post-Quantum Wallet:[/b]
[code]
./armwallet -create -wallet testnet_wallet.json
[/code]
Enter a strong passphrase and securely record your 24-word recovery phrase.

[b]Step 3 -- Run the Testnet Node Daemon:[/b]
[code]
./armcoind
[/code]
By default, [font=monospace]armcoind[/font] starts in testnet mode, discovers peers via DNS seeds, binds P2P to [font=monospace]0.0.0.0:19335[/font] with TLS 1.3, and binds RPC strictly to loopback [font=monospace]127.0.0.1:19333[/font].

[b]Step 4 -- Mine Testnet Blocks (Multi-Threaded CPU Engine):[/b]
[code]
# Launch miner with 4 CPU worker threads
./armminer -miner <YOUR_ARMX_ADDRESS> -threads 4
[/code]
[i]Active blocks (height >= 520) are evaluated using the memory-hard ArmaxHash algorithm (Argon2id 2 MB/thread). Hashrate is reported in real-time in H/s and kH/s.[/i]

[b]Step 5 -- Check Balance & Transfer Testnet Coins:[/b]
[code]
# Query spendable and confirmed balance
./armwallet -balance <YOUR_ARMX_ADDRESS>

# Send testnet coins (e.g. 10.0 ARMX with 0.00015 ARMX fee)
./armsend -to <RECIPIENT_ADDRESS> -amount 10.0 -fee 0.00015 -wallet testnet_wallet.json
[/code]
[i]Note: Freshly mined coinbase rewards require 10 confirmations to mature before becoming spendable.[/i]

[hr]

[b][size=14pt]5. Security Disclosures & Known Limitations[/size][/b]
[list]
[*] [b]Experimental Codebase:[/b] Neither the consensus engine nor the post-quantum cryptographic integration has undergone an external third-party security audit.
[*] [b]P2P Transport Security (TOFU):[/b] TLS 1.3 and Trust On First Use (TOFU) pinning are active by default. While TOFU prevents ongoing passive surveillance and subsequent tampering, initial contact remains vulnerable to active in-path MITM adversaries without out-of-band seed pins.
[*] [b]RPC Isolation:[/b] RPC daemon binds strictly to [font=monospace]127.0.0.1[/font]. Mutating endpoints require local cookie authentication ([font=monospace]armax.cookie[/font]) or HTTP Basic Auth. Binding to external interfaces requires explicit TLS certificates and credentials.
[*] [b]Low Bootstrap Hashrate & 51% Risk:[/b] During early testnet testing with few active miners, the network is susceptible to 51% hashrate spikes. Reorganizations are bounded by consensus to 720 blocks (~24 hours).
[*] [b]ARM Single-Board Computers:[/b] ML-DSA-65 modular polynomial arithmetic introduces measurable verification latency on low-power ARM single-board computers (Raspberry Pi 3/4).
[*] [b]No Economic Promises:[/b] ARMX is an experimental open-source protocol. There is no promise of future monetary value, exchange listing, airdrop, or financial return.
[/list]

[hr]

[b][size=14pt]Community & Contributor Program[/size][/b]
We invite node operators, miners, developers, and security researchers to join the testnet.
[*] [b]Proposed Contributor Incentive Framework:[/b] Read the proposed terms in [url=https://github.com/acxxcxx/armax-coin/blob/main/docs/CONTRIBUTOR_REWARDS.md]docs/CONTRIBUTOR_REWARDS.md[/url] (Marked [font=monospace]PROPOSED -- REQUIRES MAINTAINER APPROVAL[/font]).
[*] [b]Responsible Security Reporting:[/b] Please report security vulnerabilities responsibly via GitHub Private Vulnerability Reporting on the repository ([b]Security[/b] -> [b]Advisories[/b] -> [b]Report a vulnerability[/b]).
