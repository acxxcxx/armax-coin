# Testnet Backup and Reset Runbook

**Status: Operator procedure only. No reset has been performed.** This runbook is for an explicitly authorized local or public-Testnet maintenance window. It does not authorize resetting a public network, changing consensus, or modifying wallet material.

## 1. Identify the running node and its network

Do not infer the active database from a folder name, a backup filename, the current shell environment, or a remembered command. First identify every running daemon without printing credentials:

```powershell
$nodes = Get-CimInstance Win32_Process |
    Where-Object { $_.Name -ieq 'armcoind.exe' }
$nodes | ForEach-Object {
    $args = $_.CommandLine
    $args = $args -replace '(?i)(-rpcpassword(?:=|\s+))\S+', '$1<REDACTED>'
    $args = $args -replace '(?i)(-rpcuser(?:=|\s+))\S+', '$1<REDACTED>'
    [pscustomobject]@{
        PID = $_.ProcessId
        Executable = $_.ExecutablePath
        Arguments = $args
    }
}
```

If a daemon is running, do not stop it as part of path discovery. Record its executable, PID, launch arguments, service definition or scheduled-task command, and the startup log line that says `Blockchain database loaded and verified from:`. Redact RPC credentials, mining keys, and any other secret before saving or sharing command lines or logs.

Confirm the network from the same node, not from an unrelated config file:

```powershell
$nodeStatus = Invoke-RestMethod -Uri 'http://127.0.0.1:19333/status'
$nodeStatus.network_id
```

Use the daemon's actual loopback RPC port if it differs. Supply credentials through a secure prompt or approved secret store if RPC authentication is enabled; never put passwords in a shared script or report. The expected Testnet ID is exactly `ARMX-TESTNET-1`.

The daemon selects the profile from explicit `-network` first, then `ARMX_NETWORK` when `-network` was not explicitly supplied, and otherwise defaults to Testnet. `-datadir` sets `ARMX_DATA_DIR` before the blockchain opens. In the current `main.go`, the JSON config `data_dir` is applied after `OpenBlockchain`; do **not** treat that config field alone as proof of which database was opened. Require the startup log's exact database path.

## 2. Resolve and verify the database path

The current `core.GetDBPath()` selection is:

1. An explicit `core.SetDBPath` override, if an embedding application set one.
2. `ARMX_DATA_DIR`, with the active network subdirectory appended unless the directory is already named for that network.
3. An existing `<executable directory>\<network>\blockchain.db`.
4. An existing `<current working directory>\<network>\blockchain.db`.
5. Otherwise `<executable directory>\<network>\blockchain.db`.

For the daemon CLI, `-datadir <base>` sets `ARMX_DATA_DIR` before opening the chain. For a Testnet base directory not itself named `testnet`, the expected file is `<base>\testnet\blockchain.db`. The CLI `-config` option does not override this database path in the current startup order.

Record only metadata, not database contents:

```powershell
$dbPath = Read-Host 'Enter the exact database path recorded in the startup log'
Get-Item -LiteralPath $dbPath |
    Select-Object FullName, Length, LastWriteTimeUtc
Get-FileHash -LiteralPath $dbPath -Algorithm SHA256
```

If the process is stopped and there is no startup log or launch record proving the path, the active database path is **unknown**. Do not run `-resync` or rename any candidate. The daemon's `-resync` option renames the database selected by `GetDBPath()`; it does not first ask the operator to confirm that path.

## 3. Stop cleanly before backup

For a daemon started in the foreground, use `Ctrl+C` in that terminal and wait for the process to exit. If it is a Windows service, verify the exact service name, executable path, and data-directory arguments before stopping that service in an approved maintenance window:

```powershell
Get-CimInstance Win32_Service |
    Where-Object { $_.PathName -match 'armcoind\.exe' } |
    Select-Object Name, State, PathName
```

Use the service's documented graceful stop mechanism. Do not use Task Manager force-close or `Stop-Process` as the normal shutdown procedure. The current CLI `-shutdown` branch only prints a message; it does not send an RPC shutdown request.

After shutdown, verify that the specific PID is gone and that no other daemon uses the same path:

```powershell
Get-CimInstance Win32_Process |
    Where-Object { $_.Name -ieq 'armcoind.exe' } |
    Select-Object ProcessId, ExecutablePath
```

Check the daemon's final log for successful database flush/close. If shutdown or flush was incomplete, do not copy, reset, or restore over that database until an operator has diagnosed it.

## 4. Back up the database and wallet separately

Create a new, access-controlled backup directory on a separate approved storage location. Copy the closed database; do not rename, delete, or overwrite the original:

```powershell
$backupRoot = Read-Host 'Enter a new approved backup directory on separate storage'
New-Item -ItemType Directory -Path $backupRoot -Force | Out-Null
$dbBackup = Join-Path $backupRoot 'blockchain.db'
Copy-Item -LiteralPath $dbPath -Destination $dbBackup
$sourceHash = (Get-FileHash -LiteralPath $dbPath -Algorithm SHA256).Hash
$backupHash = (Get-FileHash -LiteralPath $dbBackup -Algorithm SHA256).Hash
if ($sourceHash -ne $backupHash) {
    throw 'Database backup SHA-256 mismatch; stop and investigate.'
}
Get-Item -LiteralPath $dbBackup | Select-Object FullName, Length, LastWriteTimeUtc
Get-FileHash -LiteralPath $dbBackup -Algorithm SHA256
```

Identify wallet, keystore, mnemonic, and private-key files from the wallet's documented configuration and operator records. Back them up **separately** to an encrypted, access-controlled location, using the wallet's supported backup procedure. Do not open or print their contents, put them beside a publicly accessible database backup, or assume they live inside the node data directory. Verify the wallet backup according to the wallet's documented restore procedure without exposing secrets.

## 5. Start a new isolated Testnet database

The safest reset test is to use a new, empty, explicitly isolated directory and leave the old directory untouched. Do not use a public seed or an existing Testnet/Mainnet directory:

```powershell
# Run from the repository root, where armcoind.exe is located.
$daemon = Join-Path (Get-Location) 'armcoind.exe'
$testnetRoot = Join-Path $env:TEMP 'Armaxcoin-local-testnet-YYYYMMDD-HHMM'
$logRoot = Join-Path $testnetRoot 'logs'
New-Item -ItemType Directory -Path $testnetRoot, $logRoot -Force | Out-Null
$rpcPort = 29333
$p2pPort = 29335
& $daemon `
    -network testnet `
    -datadir $testnetRoot `
    -rpcbind 127.0.0.1 `
    -rpcport $rpcPort `
    -p2pbind 127.0.0.1 `
    -p2pport $p2pPort `
    -noseeds `
    -logdir $logRoot
```

This runs in the foreground so the operator can stop it with `Ctrl+C`. Confirm the startup output names `ARMX-TESTNET-1` and the new `<testnetRoot>\testnet\blockchain.db` path. Confirm genesis height 0 and Testnet genesis hash `0000000dc84202e63a340a2439625fbf9cea5cb7b84119f93d52cff39f63a68b` (nonce `40272237`; target `0000000fffffffffffffffffffffffffffffffffffffffffffffffffffffffff`) before proceeding. Use `-verifychain` against this exact `-datadir` to verify the local chain. Never use Mainnet flags, a Mainnet data directory, or public peers for this test.

To mine or receive a first block, use a Testnet-only miner address and loopback/local peer. Confirm height 1 and block hash through the local RPC/status endpoints, then run `-verifychain` against the same directory. Exercise transactions only with disposable Testnet funds and a Testnet wallet backup. Exercise P2P block propagation and synchronization only between explicitly identified local loopback peers.

Do not connect a public explorer or faucet during the isolated reset test. Reconnect an explorer/faucet only after the owner confirms the public Testnet endpoint, network ID, data directory, and maintenance window. A local isolated chain is not a substitute for validating public-service configuration.

## 6. Recovery

If a reset attempt fails, stop the test daemon cleanly. Prefer leaving the original database untouched and pointing the daemon back to its verified original directory. If restoration is explicitly authorized, first verify the backup SHA-256 again, confirm the destination is the intended Testnet directory and that no process holds it open, then restore to a separate recovery directory or a newly empty destination. Never overwrite an unidentified database or wallet. Run `-verifychain` on the restored copy before any service reconnects.

## 7. Mandatory pre-reset checklist

- [ ] Network ID is confirmed as exactly `ARMX-TESTNET-1`.
- [ ] Executable, process arguments, and exact database path are confirmed from runtime evidence.
- [ ] The database path is not Mainnet, not an unknown custom path, and not a public node's live directory.
- [ ] No process is accessing the database; clean shutdown and flush are confirmed.
- [ ] Database backup exists in separate protected storage and SHA-256 matches.
- [ ] Wallet/keystore backup was made separately and its recovery procedure was verified without exposing secrets.
- [ ] The old database and wallet can be restored without overwriting an unrelated directory.
- [ ] The new database directory is fresh, isolated, loopback-only, and configured with no public seeds.
- [ ] The owner has explicitly approved the specific reset and maintenance window.

If any item is unchecked or uncertain, stop. This document records no approval to reset the public Testnet and no reset has been run.
