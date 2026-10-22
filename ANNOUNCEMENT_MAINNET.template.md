[center]
[color=red][size=16pt][b]*** DRAFT -- NOT FOR PUBLICATION ***[/b][/size][/color]
[b]MAINNET IS CURRENTLY LOCKED AND UNLAUNCHED. DO NOT DISTRIBUTE AS AN OFFICIAL RELEASE.[/b]
[/center]

[hr]

[center]
[size=24pt][b]ARMAXCOIN (ARMX)[/b][/size]
[size=14pt][b]Proof-of-Work Layer-1 Blockchain with Post-Quantum Signatures (NIST FIPS 204, ML-DSA-65)[/b][/size]
[size=12pt][b]Official Mainnet Launch Announcement[/b][/size]
[/center]

[hr]

[b]Official Links:[/b]
[*] [b]Source Code Repository:[/b] [url=<ISI_REPO_URL>]<ISI_REPO_URL>[/url]
[*] [b]Official Release Downloads:[/b] [url=<ISI_RELEASE_URL>]<ISI_RELEASE_URL>[/url]
[*] [b]Release Version:[/b] [code]<ISI_VERSION>[/code]
[*] [b]Git Commit Hash:[/b] [font=monospace]<ISI_COMMIT_HASH>[/font]
[*] [b]SHA256SUMS:[/b] [url=<ISI_CHECKSUM_URL>]SHA256SUMS[/url]
[*] [b]License:[/b] MIT Open Source License

[hr]

[b][size=14pt]1. Consensus Specifications[/size][/b]
[list]
[*] [b]Network Identifier:[/b] [font=monospace]ARMX-MAINNET-1[/font]
[*] [b]Wire Magic:[/b] [font=monospace]ARMX_MAIN[/font]
[*] [b]P2P Port:[/b] [b]19334[/b] (Public TCP peer mesh)
[*] [b]RPC Daemon Port:[/b] [b]19332[/b] (Bound strictly to loopback [font=monospace]127.0.0.1[/font] by default)
[*] [b]Block Header PoW Algorithm:[/b] Double-SHA256 (SHA-256d)
[*] [b]Digital Signatures:[/b] NIST FIPS 204 (ML-DSA-65) Post-Quantum Module-Lattice Signatures
[*] [b]Target Block Time:[/b] 120 seconds (2 minutes)
[*] [b]Difficulty Retargeting:[/b] Continuous LWMA-1 ($N=60$ sliding window)
[*] [b]Initial Block Reward:[/b] 60 ARMX (6,000,000,000 Armoids)
[*] [b]Halving Interval:[/b] Every 1,050,000 blocks (~4.0 years on 120s cadence)
[*] [b]Maximum Hard Cap:[/b] 126,000,000 ARMX (12,600,000,000,000,000 Armoids, 8 decimals)
[*] [b]Real Theoretical Lifetime Emission:[/b] 125,999,999.874 ARMX
[*] [b]Premine / Founder Allocation:[/b] [b]0% (Zero Premine, Zero ICO, Zero Developer Tax)[/b]
[*] [b]Genesis Block Allocation:[/b] 60 ARMX burned to unspendable zero address ([font=monospace]armx1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqplwcdd[/font])
[*] [b]Genesis Block Hash:[/b] [font=monospace]<ISI_GENESIS_HASH>[/font]
[*] [b]Genesis Merkle Root:[/b] [font=monospace]<ISI_GENESIS_MERKLE_ROOT>[/font]
[*] [b]Genesis Timestamp:[/b] <ISI_GENESIS_TIMESTAMP>
[*] [b]Genesis Nonce:[/b] <ISI_GENESIS_NONCE>
[*] [b]Genesis Target:[/b] [font=monospace]<ISI_GENESIS_TARGET>[/font]
[*] [b]Mainnet Seed Nodes:[/b]
[code]
<ISI_SEED_NODE_1>:19334
<ISI_SEED_NODE_2>:19334
<ISI_SEED_NODE_3>:19334
[/code]
[/list]

[hr]

[b][size=14pt]2. Measured Wire Sizes & Block Capacity[/size][/b]
[list]
[*] [b]Public Key Size:[/b] 1,952 bytes (3,904 hex characters on wire)
[*] [b]Signature Size:[/b] 3,309 bytes (6,618 hex characters on wire)
[*] [b]Raw Cryptographic Payload:[/b] ~5,385 bytes per transaction
[*] [b]Total JSON Wire Format:[/b] ~10,761 bytes per transaction
[*] [b]Block Size Limit:[/b] 2,000,000 bytes (2 MB)
[*] [b]Maximum Block Capacity:[/b] Up to 185 user transactions per block (+1 coinbase transaction = 186 total, ~1,990,513 bytes)
[*] [b]Transaction Throughput:[/b] ~1.54 transactions per second (at 120s block time)
[/list]

[hr]

[b][size=14pt]3. How to Participate[/size][/b]

[b]Step 1 -- Download & Verify Binaries:[/b]
[code]
sha256sum -c SHA256SUMS
chmod +x armcoind armminer armsend armwallet
[/code]

[b]Step 2 -- Create Wallet:[/b]
[code]
./armwallet -network mainnet -create -wallet mainnet_wallet.json
[/code]

[b]Step 3 -- Run Mainnet Node:[/b]
[code]
./armcoind -network mainnet
[/code]

[b]Step 4 -- Mine Mainnet Blocks:[/b]
[code]
./armminer -network mainnet -miner <YOUR_MAINNET_ADDRESS> -threads <N>
[/code]

[hr]

[b][size=14pt]4. Security Disclosures & Known Limitations[/size][/b]
[list]
[*] [b]Release Gate:[/b] This announcement may be published only after every item in [font=monospace]docs/MAINNET_READINESS.md[/font] has documented evidence, including an independent security audit and adversarial public-testnet results.
[*] [b]P2P Transport Security:[/b] TLS 1.3 and TOFU certificate pinning are enabled by default. Seed fingerprints must be published through authenticated out-of-band channels to mitigate first-contact MITM risk.
[*] [b]RPC Exposure:[/b] Public RPC requires explicit opt-in, HTTP Basic Auth, and configured TLS certificate/key files. Loopback-only RPC remains the recommended deployment.
[*] [b]Wallet Scope:[/b] Hardware wallet support must not be claimed unless a dedicated signer implementation and audit have been completed.
[*] [b]Proof-of-Work Hardware Profile:[/b] Block headers utilize standard double-SHA256 (SHA-256d). SHA-256d can be implemented in specialized hardware (GPU/ASIC). The Armaxcoin block header format is custom, so existing Bitcoin miners cannot mine it without specialized software. It is not claimed to be CPU-friendly or ASIC-resistant.
[*] [b]Initial Hashrate & Reorganization Risk:[/b] During early launch phases with low total hashrate, the network is vulnerable to 51% hashrate spikes. Reorganizations are strictly bounded by consensus to 720 blocks (~24 hours).
[*] [b]ARM Single-Board Computers:[/b] ML-DSA-65 modular polynomial arithmetic introduces measurable verification latency on low-power ARM CPUs.
[*] [b]No Economic Promises:[/b] ARMX is an open-source decentralized protocol. There are no expectations of monetary return, profit, investment yield, listing, or airdrop.
[/list]

[hr]

[b][size=14pt]Security Reporting[/size][/b]
Please report security vulnerabilities responsibly via GitHub Private Vulnerability Reporting on the repository ([b]Security[/b] -> [b]Advisories[/b] -> [b]Report a vulnerability[/b]).
