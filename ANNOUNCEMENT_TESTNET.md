[center]
[size=24pt][b]ARMAXCOIN TESTNET (tARMX)[/b][/size]
[size=14pt][b]Proof-of-Work Layer-1 Blockchain with Post-Quantum Signatures (NIST FIPS 204, ML-DSA-65)[/b][/size]
[size=12pt][b]Experimental Public Open Testnet Release Announcement (v1.0.8)[/b][/size]
[/center]

[hr]

[b]Scheduled Consensus Hard Fork Notice (v1.0.7 / v1.0.8):[/b]
[list]
[*] [b]Activation Height:[/b] [b]Testnet Block 20,000[/b]
[*] [b]Consensus Rule Change:[/b] Maximum allowed future block timestamp is tightened from [b]7,200 seconds (2 hours)[/b] down to [b]600 seconds (10 minutes)[/b] ahead of the validating node's local clock. Historical blocks below height 20,000 preserve the legacy 2-hour window.
[*] [b]Mandatory Operator Action:[/b] All full nodes, miners, and mining pools [b]MUST upgrade to v1.0.8 (or at least v1.0.7) before block 20,000[/b] and maintain accurate NTP clock synchronization ([tt]timedatectl set-ntp true[/tt] / [tt]w32tm /resync[/tt]). Un-upgraded v1.0.6 nodes will accept blocks rejected by v1.0.7+ after height 20,000, creating an unrecoverable chain split.
[/list]

[b]Official Links:[/b]
[list]
[*] [b]Official Website & Web Portal:[/b] [url=https://armax-coin.cloud]https://armax-coin.cloud[/url]
[*] [b]Testnet Blockchain Explorer:[/b] [url=https://armax-coin.cloud/explorer]https://armax-coin.cloud/explorer[/url]
[*] [b]Testnet Mining Pool:[/b] [url=https://pool.armax-coin.cloud]https://pool.armax-coin.cloud[/url]
[*] [b]Source Code Repository:[/b] [url=https://github.com/acxxcxx/armax-coin]https://github.com/acxxcxx/armax-coin[/url]
[*] [b]Releases & Assets:[/b] [url=https://github.com/acxxcxx/armax-coin/releases]https://github.com/acxxcxx/armax-coin/releases[/url]
[*] [b]Git Tag Reference:[/b] [url=https://github.com/acxxcxx/armax-coin/tree/v1.0.8]refs/tags/v1.0.8[/url]
[*] [b]v1.0.8 Release Notes:[/b] [url=https://github.com/acxxcxx/armax-coin/blob/main/docs/releases/v1.0.8.md]docs/releases/v1.0.8.md[/url]
[*] [b]v1.0.7 Release Notes:[/b] [url=https://github.com/acxxcxx/armax-coin/blob/main/docs/releases/v1.0.7.md]docs/releases/v1.0.7.md[/url]
[*] [b]Documentation Suite:[/b] [url=https://github.com/acxxcxx/armax-coin#authoritative-documentation-index]Documentation Index[/url]
[*] [b]License:[/b] MIT Open Source License
[/list]

[hr]

[b][size=14pt]1. Testnet Purpose & Scope[/size][/b]
The Armaxcoin Public Testnet ([tt]ARMX-TESTNET-1[/tt]) is an open, decentralized testing network for node operators, miners, developers, and security researchers to test and validate:
[list]
[*] Quantum-resistant digital signatures ([b]NIST FIPS 204 ML-DSA-65[/b]) in Layer-1 consensus transactions.
[*] High-efficiency CPU Proof-of-Work mining ([b]ArmaxHash v2 Stream-Fast[/b]) activated at block #2500 (historical v1 Argon2id active blocks 520..2499).
[*] Responsive difficulty retargeting via [b]LWMA-1[/b] (N=60 sliding evaluation window).
[*] P2P mesh transport encrypted with mandatory [b]TLS 1.3[/b], [b]TOFU certificate pinning[/b], optional SPKI seed fingerprint pinning, and [b]PUSH_BLOCKS[/b] NAT propagation.
[*] Scalable initial sync with [b]incremental persistence[/b] (O(1) batch write instead of O(N^2) rewrite).
[/list]

[color=red][b]CRITICAL EXPERIMENTAL TESTNET DISCLAIMER:[/b][/color]
[list]
[*] [b]Zero Economic Value:[/b] Testnet coins ([tt]tARMX[/tt]) carry [b]NO monetary value[/b], no investment utility, and no guarantee of exchange listing or token conversion.
[*] [b]Experimental Testnet:[/b] The consensus engine, ArmaxHash implementation, and cryptographic integration are active testnet implementations focused on verification and protocol stabilization.
[*] [b]State Reset Possibility:[/b] As an active development testnet, blockchain state may be reset if critical protocol flaws are discovered.
[*] [b]Mainnet Status:[/b] Mainnet is [b]locked in source[/b] ([tt]core.MainnetLaunched = false[/tt]). Invoking [tt]-network mainnet[/tt] is strictly rejected by all binaries.
[/list]

[hr]

[b][size=14pt]2. Technical Specifications[/size][/b]
[list]
[*] [b]Network Identifier:[/b] [tt]ARMX-TESTNET-1[/tt] (isolated from mainnet)
[*] [b]Wire Magic:[/b] [tt]ARMX_TEST[/tt]
[*] [b]P2P Port:[/b] [b]19335[/b] (Public TCP peer gossip mesh)
[*] [b]RPC Daemon Port:[/b] [b]19333[/b] (Bound strictly to loopback [tt]127.0.0.1[/tt] by default)
[*] [b]Block Header PoW Algorithm Timeline:[/b]
[list]
[*] [b]Blocks 0 .. 519:[/b] Double-SHA256 (SHA-256d bootstrap phase)
[*] [b]Blocks 520 .. 2499:[/b] ArmaxHash v1 (Argon2id 2,048 KiB + Blake2b-512)
[*] [b]Blocks 2500+ (Active):[/b] [b]ArmaxHash v2 Stream-Fast[/b] (domain-separated chained BLAKE2b; the fixed-header hash benchmark reports 0 allocations/op)
[/list]
[*] [b]Digital Signatures:[/b] NIST FIPS 204 (ML-DSA-65) Post-Quantum Module-Lattice Signatures (pure, non-hybrid)
[*] [b]Target Block Time:[/b] 120 seconds (2 minutes)
[*] [b]Difficulty Retargeting:[/b] Continuous LWMA-1 (N=60 sliding evaluation window)
[*] [b]Initial Block Reward:[/b] 60.00000000 tARMX (6,000,000,000 Armoids)
[*] [b]Halving Interval:[/b] Every 1,050,000 blocks (~4.0 years on 120s cadence)
[*] [b]Maximum Hard Cap:[/b] 126,000,000 tARMX (12,600,000,000,000,000 Armoids, 8 decimal places)
[*] [b]Mainnet Tokenomics Status:[/b] Mainnet has not launched. Verifiable consensus parameters in source are: 126,000,000 ARMX maximum supply, 60 ARMX initial block reward, 1,050,000-block halving interval, and 120-second target block time. Planned developer early-mining details are disclosed in the repository README (section: Consensus & Tokenomics Parameters); full details will be announced in a separate thread.
[*] [b]Genesis Block Allocation:[/b] 60 tARMX burned to unspendable zero address ([tt]armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd[/tt])
[*] [b]Genesis Block Hash:[/b] [tt]0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b[/tt]
[*] [b]Genesis Merkle Root:[/b] [tt]808a4007681dd123133bfe106b101d251ff0d2d023f600384394673bfc03768d[/tt]
[*] [b]Genesis Timestamp:[/b] 1710000001 (Nonce: 40,272,237)
[*] [b]ArmaxHash Target Ceiling (Testnet):[/b] [tt]000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff[/tt] (Difficulty 3 bootstrap)
[*] [b]Coinbase Maturity:[/b] 10 confirmations on Testnet (100 confirmations on Mainnet)
[*] [b]Max Reorganization Depth:[/b] 720 blocks (~24 hours at target cadence; local reorganization limit, not transaction finality)
[*] [b]Active DNS Seed Nodes:[/b]
[code]
testnet-seed1.armax-coin.cloud:19335
testnet-seed2.armax-coin.cloud:19335
testnet-seed3.armax-coin.cloud:19335
[/code]
[/list]

[hr]

[b][size=14pt]3. Measured Transaction & Wire Sizes[/size][/b]
Armaxcoin incorporates NIST FIPS 204 (ML-DSA-65) post-quantum signatures for every user transaction:
[list]
[*] [b]Public Key Size:[/b] 1,952 bytes (3,904 hex characters)
[*] [b]Signature Size:[/b] 3,309 bytes (6,618 hex characters)
[*] [b]Combined Public Key and Signature:[/b] 5,261 raw bytes per transaction (before addresses and transaction fields)
[*] [b]Total JSON Wire Format:[/b] ~10,761 bytes per transaction
[*] [b]Block Size Limit:[/b] 2,000,000 bytes (2 MB)
[*] [b]Measured Block Capacity:[/b] Up to 185 user transactions per block (+1 coinbase transaction = 186 total, ~1,990,513 bytes)
[*] [b]Transaction Throughput:[/b] ~1.54 transactions per second (at 120s block time)
[/list]

[hr]

[b][size=14pt]4. Release Verification[/size][/b]
Official pre-compiled release packages and archives are accompanied by cryptographic checksum manifests and detached OpenPGP signatures. Always verify the authenticity and integrity of downloaded files before execution:

[list]
[*] [b]Checksum Manifest:[/b] [tt]SHA256SUMS[/tt]
[*] [b]Detached Signature File:[/b] [tt]SHA256SUMS.asc[/tt]
[*] [b]Release Signing Public Key:[/b] [tt]ARMAX-PGP-KEY.asc[/tt] (available in release assets and repository root)
[*] [b]Official Release Key Fingerprint:[/b]
[code]
C857 E67F 3FC6 3BE3 5125  C3D3 2ACE 863E D4BF 1460
[/code]
[*] [b]Verification Commands (Linux / macOS):[/b]
[code]
# 1. Import official PGP release key & verify fingerprint
gpg --import ARMAX-PGP-KEY.asc
gpg --fingerprint dev@armax-coin.cloud

# 2. Verify detached signature on SHA256SUMS
gpg --verify SHA256SUMS.asc SHA256SUMS

# 3. Verify downloaded archive checksum
sha256sum --check --ignore-missing SHA256SUMS
[/code]
[*] [b]Verification Commands (Windows PowerShell):[/b]
[code]
# Compute SHA-256 hash of archive
Get-FileHash -Algorithm SHA256 .\armax-v1.0.8-windows-amd64.zip

# Match against published SHA256SUMS manifest
Get-Content .\SHA256SUMS | Select-String "armax-v1.0.8-windows-amd64.zip"
[/code]
[/list]

[hr]

[b][size=14pt]5. Quickstart: Building from Source & Running on Testnet[/size][/b]

[b]Prerequisites:[/b]
[list]
[*] [b]Go Compiler:[/b] Go 1.27 or higher (required for native [tt]crypto/mldsa[/tt] standard library package)
[*] [b]Git[/b]
[/list]

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
[i]On Windows, run: [tt].\build-all.ps1[/tt][/i]

[b]Step 2 -- Create an Encrypted Post-Quantum Wallet:[/b]
[code]
./armwallet -create -wallet testnet_wallet.json
[/code]
Enter a strong passphrase and securely record your 24-word recovery phrase.

[b]Step 3 -- Run the Testnet Node Daemon:[/b]
[code]
./armcoind
[/code]
By default, [tt]armcoind[/tt] starts in testnet mode, discovers peers via DNS seeds, binds P2P to [tt]0.0.0.0:19335[/tt] with TLS 1.3, and binds RPC strictly to loopback [tt]127.0.0.1:19333[/tt].

[b]Step 4 -- Mine Testnet Blocks (Multi-Threaded CPU Engine):[/b]
[code]
# Launch miner with 4 CPU worker threads
./armminer -miner <YOUR_ARMX_ADDRESS> -threads 4
[/code]
[i]Active blocks (height >= 2500) use ArmaxHash v2 Stream-Fast (domain-separated chained BLAKE2b). The fixed-header hash benchmark reports 0 allocations/op; actual mining throughput depends on hardware and workload. Hashrate is reported in real time in H/s, kH/s, and MH/s.[/i]

[b]Step 5 -- Check Balance & Transfer Testnet Coins:[/b]
[code]
# Query spendable and confirmed balance
./armwallet -balance <YOUR_ARMX_ADDRESS>

# Send testnet coins (e.g. 10.0 tARMX with 0.00015 tARMX fee)
./armsend -to <RECIPIENT_ADDRESS> -amount 10.0 -fee 0.00015 -wallet testnet_wallet.json
[/code]
[i]Note: Freshly mined coinbase rewards require 10 confirmations to mature before becoming spendable.[/i]

[hr]

[b][size=14pt]6. Security Disclosures & Known Limitations[/size][/b]
[list]
[*] [b]Active Testnet Stabilization:[/b] The network is operating under continuous peer validation, multi-node synchronization testing, and consensus verification.
[*] [b]P2P Transport Security (TOFU):[/b] TLS 1.3 and Trust On First Use (TOFU) pinning are active by default. While TOFU prevents ongoing passive surveillance and subsequent tampering, initial contact remains vulnerable to active in-path MITM adversaries without out-of-band seed pins.
[*] [b]RPC Isolation:[/b] RPC daemon binds strictly to [tt]127.0.0.1[/tt]. Mutating endpoints require local cookie authentication ([tt]armax.cookie[/tt]) or HTTP Basic Auth. Binding to external interfaces requires explicit TLS certificates and credentials.
[*] [b]Low Bootstrap Hashrate & 51% Risk:[/b] During early testnet testing with few active miners, the network is susceptible to 51% hashrate spikes. Reorganizations are bounded by consensus to 720 blocks (~24 hours).
[*] [b]ARM Single-Board Computers:[/b] ML-DSA-65 modular polynomial arithmetic introduces measurable verification latency on low-power ARM single-board computers (Raspberry Pi 3/4).
[*] [b]No Economic Promises:[/b] Testnet coins (tARMX) have no economic value. The ARMX protocol is an experimental open-source protocol. There is no promise of future monetary value, exchange listing, airdrop, or financial return.
[/list]

[hr]

[b][size=14pt]Community & Contributions[/size][/b]
We invite node operators, miners, developers, and security researchers to join and test the experimental testnet. Contributions are voluntary and credited in [url=https://github.com/acxxcxx/armax-coin/blob/main/CONTRIBUTORS.md]CONTRIBUTORS.md[/url]. Testnet coins have no monetary value, and no airdrops or financial rewards are promised.
[list]
[*] [b]Responsible Security Reporting:[/b] Please report security vulnerabilities responsibly via GitHub Private Vulnerability Reporting on the repository ([b]Security[/b] -> [b]Advisories[/b] -> [b]Report a vulnerability[/b]).
[/list]
