package main

// webUIHTML renders the modern cross-platform desktop UI (Light default + armax_explorer Dark theme).
const webUIHTML = `<!DOCTYPE html>
<html lang="en" data-theme="light">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Armaxcoin Core Wallet</title>
<link rel="icon" type="image/x-icon" href="/favicon.ico">
<style>
  :root, [data-theme="light"] {
    --bg: #f8fafc;
    --bg-subtle: #f1f5f9;
    --surface: #ffffff;
    --surface-2: #f1f5f9;
    --surface-hover: #e2e8f0;
    --border: rgba(15, 23, 42, 0.11);
    --border-strong: rgba(15, 23, 42, 0.18);
    --border-focus: #0284c7;
    --text: #0f172a;
    --text-muted: #475569;
    --text-dim: #64748b;
    --accent: #0369a1;
    --accent-hover: #075985;
    --accent-subtle: rgba(3, 105, 161, 0.10);
    --amber: #f59e0b;
    --amber-hover: #d97706;
    --amber-text: #ffffff;
    --brand-gold: #d97706;
    --pill-active-bg: #0f172a;
    --pill-active-fg: #ffffff;
    --success: #059669;
    --success-subtle: rgba(5, 150, 105, 0.12);
    --warning: #b45309;
    --warning-subtle: #fef3c7;
    --danger: #b91c1c;
    --danger-subtle: rgba(185, 28, 28, 0.10);
    --badge-info-bg: #e0f2fe;
    --badge-info-fg: #0369a1;
    --console-bg: #0f172a;
    --console-fg: #e2e8f0;
    --shadow-sm: 0 1px 3px rgba(15, 23, 42, 0.05);
    --shadow-md: 0 4px 16px rgba(15, 23, 42, 0.08);
    --font-sans: "Segoe UI", system-ui, -apple-system, BlinkMacSystemFont, Roboto, Arial, sans-serif;
    --font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }

  [data-theme="dark"] {
    /* Exact armax_explorer Dark Theme Palette */
    --bg: #0b0f14;
    --bg-subtle: #0e141c;
    --surface: #121922;
    --surface-2: #182230;
    --surface-hover: #1e2b3c;
    --border: rgba(255, 255, 255, 0.10);
    --border-strong: rgba(255, 255, 255, 0.16);
    --border-focus: #38bdf8;
    --text: #f1f5f9;
    --text-muted: #9fb0c4;
    --text-dim: #8b9cb3;
    --accent: #0ea5e9;
    --accent-hover: #38bdf8;
    --accent-subtle: rgba(14, 165, 233, 0.14);
    --amber: #f59e0b;
    --amber-hover: #fbbf24;
    --amber-text: #0b0f14;
    --brand-gold: #f59e0b;
    --pill-active-bg: #0ea5e9;
    --pill-active-fg: #0b0f14;
    --success: #10b981;
    --success-subtle: rgba(16, 185, 129, 0.12);
    --warning: #f59e0b;
    --warning-subtle: rgba(245, 158, 11, 0.14);
    --danger: #ef4444;
    --danger-subtle: rgba(239, 68, 68, 0.14);
    --badge-info-bg: rgba(14, 165, 233, 0.15);
    --badge-info-fg: #38bdf8;
    --console-bg: #080c11;
    --console-fg: #cbd5e1;
    --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.25);
    --shadow-md: 0 6px 20px rgba(0, 0, 0, 0.38);
  }

  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  html, body {
    height: 100%;
    font-family: var(--font-sans);
    background: var(--bg);
    color: var(--text);
    font-size: 14px;
    line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }
  .mono { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }

  /* Startup Authentication Gate (Full Screen — Zero Bypass When Locked on Launch) */
  .startup-gate {
    min-height: 100vh;
    display: none;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--bg);
  }
  .startup-gate.active { display: flex; }
  .gate-card {
    width: 100%;
    max-width: 440px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 18px;
    padding: 32px;
    box-shadow: var(--shadow-md);
  }
  .gate-brand {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    margin-bottom: 22px;
  }
  .gate-brand img.logo-mark { width: 36px; height: 36px; border-radius: 8px; }
  .gate-brand img.wordmark-hidden { display: none; }
  .brand-wordmark {
    font-weight: 800;
    font-size: 17px;
    letter-spacing: 0.06em;
    color: var(--text);
  }
  .brand-wordmark span { color: var(--brand-gold); }

  /* App Shell */
  #appShell {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }

  /* Top Navigation Header (Screenshot Style) */
  .topbar {
    position: sticky;
    top: 0;
    z-index: 50;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    padding: 0 24px;
    min-height: 62px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 14px;
  }
  .topbar-left {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }
  .topbar-logo { width: 28px; height: 28px; border-radius: 7px; object-fit: contain; }
  .topbar-tabs {
    display: flex;
    align-items: center;
    gap: 4px;
    overflow-x: auto;
    padding: 4px 0;
  }
  .tab-btn, .btn-sidebar {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 13px;
    font-weight: 600;
    padding: 8px 14px;
    border-radius: 999px;
    cursor: pointer;
    white-space: nowrap;
    transition: background-color 0.15s ease, color 0.15s ease;
  }
  .tab-btn:hover { color: var(--text); background: var(--surface-2); }
  .tab-btn.active {
    background: var(--pill-active-bg);
    color: var(--pill-active-fg);
  }
  .topbar-right {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .wallet-quick-select {
    background: var(--surface-2);
    border: 1px solid var(--border);
    color: var(--text);
    border-radius: 999px;
    padding: 6px 12px;
    font-size: 12px;
    font-weight: 600;
    max-width: 170px;
    cursor: pointer;
    outline: none;
  }
  .theme-toggle-btn {
    background: var(--surface-2);
    border: 1px solid var(--border);
    color: var(--text);
    width: 34px;
    height: 34px;
    border-radius: 999px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    font-size: 15px;
    transition: background-color 0.15s ease;
  }
  .theme-toggle-btn:hover { background: var(--surface-hover); }
  .net-pill {
    padding: 5px 12px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.05em;
    background: var(--warning-subtle);
    color: var(--warning);
    border: 1px solid rgba(245, 158, 11, 0.25);
  }
  .net-pill.mainnet {
    background: var(--success-subtle);
    color: var(--success);
    border-color: rgba(16, 185, 129, 0.28);
  }
  .net-pill.regtest {
    background: var(--badge-info-bg);
    color: var(--badge-info-fg);
    border-color: rgba(14, 165, 233, 0.28);
  }

  /* Main Container */
  .main-container {
    width: 100%;
    max-width: 1180px;
    margin: 0 auto;
    padding: 24px 24px 40px 24px;
    flex: 1;
  }
  .tab-pane { display: none; }
  .tab-pane.active { display: block; }

  /* Cards & Grids */
  .card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 16px;
    padding: 22px 24px;
    box-shadow: var(--shadow-sm);
    margin-bottom: 18px;
  }
  .overview-grid {
    display: grid;
    grid-template-columns: minmax(0, 1.85fr) minmax(290px, 1fr);
    gap: 18px;
    align-items: start;
  }
  .grid-2 {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 18px;
  }
  @media (max-width: 900px) {
    .overview-grid, .grid-2 { grid-template-columns: 1fr; }
    .topbar { flex-wrap: wrap; padding: 10px 16px; }
  }

  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 14px;
  }
  .card-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-muted);
  }
  .section-title {
    font-size: 16px;
    font-weight: 700;
    color: var(--text);
  }

  /* Hero Balance */
  .wallet-meta-row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    margin-bottom: 10px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .status-pill {
    display: inline-flex;
    align-items: center;
    padding: 3px 10px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 700;
    background: var(--badge-info-bg);
    color: var(--badge-info-fg);
  }
  .status-pill.unlocked {
    background: var(--success-subtle);
    color: var(--success);
  }
  .status-pill.warn {
    background: var(--warning-subtle);
    color: var(--warning);
  }
  .status-pill.danger {
    background: var(--danger-subtle);
    color: var(--danger);
  }
  .hero-balance {
    font-size: 40px;
    font-weight: 800;
    letter-spacing: -0.025em;
    line-height: 1.1;
    color: var(--text);
    display: flex;
    align-items: baseline;
    gap: 10px;
    flex-wrap: wrap;
  }
  .hero-ticker {
    font-size: 20px;
    font-weight: 800;
    color: var(--brand-gold);
  }
  .hero-sub {
    margin-top: 8px;
    font-size: 13px;
    color: var(--text-dim);
  }
  .action-row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    margin-top: 18px;
  }

  /* Buttons */
  .btn {
    border: 1px solid transparent;
    border-radius: 10px;
    padding: 9px 18px;
    font-size: 13px;
    font-weight: 700;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    background: var(--amber);
    color: var(--amber-text);
    transition: all 0.15s ease;
  }
  .btn:hover:not(:disabled) { background: var(--amber-hover); }
  .btn:disabled { opacity: 0.45; cursor: not-allowed; }
  .btn-dark {
    background: var(--pill-active-bg);
    color: var(--pill-active-fg);
  }
  .btn-dark:hover:not(:disabled) { opacity: 0.9; background: var(--pill-active-bg); }
  .btn-outline, .btn-secondary {
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--border-strong);
  }
  .btn-outline:hover:not(:disabled), .btn-secondary:hover:not(:disabled) {
    background: var(--surface-2);
  }
  .btn-danger {
    background: var(--danger-subtle);
    color: var(--danger);
    border: 1px solid rgba(239, 68, 68, 0.25);
  }
  .btn-sm { padding: 6px 12px; font-size: 12px; border-radius: 8px; }
  .btn-block { width: 100%; }

  /* Address & Form Inputs */
  .address-box {
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 12px 14px;
    font-family: var(--font-mono);
    font-size: 13px;
    word-break: break-all;
    color: var(--text);
  }
  .form-group { margin-bottom: 14px; }
  .form-label {
    display: block;
    font-size: 12px;
    font-weight: 700;
    color: var(--text-muted);
    margin-bottom: 6px;
  }
  .form-control {
    width: 100%;
    background: var(--bg);
    border: 1px solid var(--border-strong);
    color: var(--text);
    border-radius: 10px;
    padding: 10px 12px;
    font-size: 13px;
    outline: none;
    transition: border-color 0.15s ease;
  }
  .form-control:focus { border-color: var(--border-focus); }

  /* Info Key-Value Rows */
  .kv-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 0;
    border-bottom: 1px solid var(--border);
    font-size: 13px;
    gap: 12px;
  }
  .kv-row:last-child { border-bottom: none; padding-bottom: 0; }
  .kv-key { color: var(--text-dim); }
  .kv-val { font-weight: 700; color: var(--text); text-align: right; word-break: break-word; }

  /* Tables */
  .table-wrap { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th {
    text-align: left;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-dim);
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
  }
  td {
    padding: 11px 12px;
    border-bottom: 1px solid var(--border);
    color: var(--text);
    vertical-align: middle;
  }
  tr:last-child td { border-bottom: none; }
  tbody tr.clickable:hover { background: var(--surface-2); cursor: pointer; }

  /* Fee Pills */
  .fee-pills { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 8px; }
  .fee-pill {
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    color: var(--text);
    padding: 7px 12px;
    border-radius: 8px;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
  }
  .fee-pill:hover, .fee-pill.active {
    border-color: var(--amber);
    background: var(--warning-subtle);
  }

  /* Diagnostics Enlarged Console Log */
  .console-output {
    background: var(--console-bg);
    color: var(--console-fg);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 16px;
    font-family: var(--font-mono);
    font-size: 13px;
    line-height: 1.55;
    min-height: 460px;
    height: 54vh;
    resize: vertical;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .console-output.expanded {
    height: 78vh;
  }
  .miner-log-box {
    background: var(--console-bg);
    color: var(--console-fg);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 12px 14px;
    font-family: var(--font-mono);
    font-size: 12px;
    height: 220px;
    overflow-y: auto;
    white-space: pre-wrap;
  }

  /* Receive QR */
  .qr-frame {
    background: #ffffff;
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 12px;
    width: 220px;
    height: 220px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .qr-frame img { width: 196px; height: 196px; object-fit: contain; }

  /* Modals */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(11, 15, 20, 0.65);
    backdrop-filter: blur(4px);
    display: none;
    align-items: center;
    justify-content: center;
    z-index: 200;
    padding: 16px;
  }
  .modal-backdrop.active { display: flex; }
  .modal {
    width: 100%;
    max-width: 560px;
    max-height: 90vh;
    overflow-y: auto;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 16px;
    box-shadow: var(--shadow-md);
  }
  .modal-header {
    padding: 16px 22px;
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .modal-title { font-size: 16px; font-weight: 700; }
  .modal-close {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 22px;
    cursor: pointer;
    line-height: 1;
  }
  .modal-body { padding: 20px 22px; }
  .banner-alert {
    padding: 12px 16px;
    border-radius: 10px;
    font-size: 13px;
    font-weight: 600;
    margin-bottom: 14px;
    background: var(--warning-subtle);
    color: var(--warning);
    border: 1px solid rgba(245, 158, 11, 0.3);
  }
  .banner-alert.danger {
    background: var(--danger-subtle);
    color: var(--danger);
    border-color: rgba(239, 68, 68, 0.3);
  }
  .banner-alert.success {
    background: var(--success-subtle);
    color: var(--success);
    border-color: rgba(16, 185, 129, 0.3);
  }
</style>
</head>
<body>

<!-- INITIAL LOADING / ERROR SPLASH -->
<div id="startupLoadingScreen" class="startup-gate active">
  <div class="gate-card" style="text-align:center;">
    <div class="gate-brand">
      <img class="logo-mark" src="/assets/armax_logo.png" alt="Armaxcoin">
      <div class="brand-wordmark">ARMAX<span>COIN</span></div>
    </div>
    <div id="lblLoadingStatus" style="font-size:13px; color:var(--text-muted); margin-top:8px;">Loading Armaxcoin Core Wallet...</div>
    <button type="button" id="btnLoadingRetry" class="btn btn-outline" style="display:none; margin:16px auto 0 auto;" onclick="bootstrapApplication()">Retry</button>
  </div>
</div>

<!-- STRICT STARTUP AUTHENTICATION SCREEN (Blocks Desktop Access Until Unlocked) -->
<div id="startupLockScreen" class="startup-gate">
  <div class="gate-card">
    <div class="gate-brand">
      <img class="logo-mark" src="/assets/armax_logo.png" alt="Armaxcoin">
      <img class="wordmark-hidden" src="/assets/armax_text.png" alt="ARMAXCOIN">
      <div class="brand-wordmark">ARMAX<span>COIN</span></div>
    </div>
    <h2 style="text-align:center; font-size:20px; font-weight:800; margin-bottom:6px;">Unlock Wallet</h2>
    <p id="lblStartupWalletDesc" style="text-align:center; color:var(--text-muted); font-size:13px; margin-bottom:18px;">
      Enter your encryption passphrase to open your Armaxcoin wallet.
    </p>
    <div id="startupLockError" class="banner-alert danger" style="display:none;"></div>
    <div class="form-group">
      <label class="form-label">Wallet Passphrase</label>
      <input type="password" class="form-control" id="txtStartupPass" placeholder="Enter wallet passphrase..." onkeydown="if(event.key==='Enter') unlockStartupWallet()">
    </div>
    <div style="display:flex; gap:10px; margin-top:16px;">
      <button type="button" class="btn btn-outline" style="flex:1;" onclick="exitDesktopApp()">Cancel / Exit</button>
      <button type="button" class="btn" id="btnStartupUnlock" style="flex:1.4;" onclick="unlockStartupWallet()">Unlock Wallet</button>
    </div>
    <div id="lblStartupNodeStatus" style="margin-top:14px; text-align:center; font-size:12px; color:var(--text-dim);">Checking local node...</div>
  </div>
</div>

<!-- ONBOARDING GATE WHEN NO WALLET EXISTS -->
<div id="startupOnboardingGate" class="startup-gate">
  <div class="gate-card" id="noWalletCard">
    <div class="gate-brand">
      <img class="logo-mark" src="/assets/armax_logo.png" alt="Armaxcoin">
      <div class="brand-wordmark">ARMAX<span>COIN</span></div>
    </div>
    <h2 style="text-align:center; font-size:20px; font-weight:800; margin-bottom:6px;">Initialize Your Wallet</h2>
    <p style="text-align:center; color:var(--text-muted); font-size:13px; margin-bottom:18px;">
      No encrypted keystore was found at <code id="lblMissingWalletPath" class="mono" style="word-break:break-all; overflow-wrap:anywhere;">wallet.json</code>. Create or restore an encrypted post-quantum wallet to continue.
    </p>
    <div style="display:flex; flex-direction:column; gap:10px;">
      <button type="button" class="btn btn-block" onclick="openModal('setupWalletModal')">Create New Encrypted Wallet</button>
      <button type="button" class="btn btn-dark btn-block" onclick="openModal('restoreWalletModal')">Restore from 24-Word Mnemonic</button>
      <button type="button" class="btn btn-outline btn-block" onclick="exitDesktopApp()">Cancel / Exit</button>
    </div>
    <div id="lblOnboardingNodeStatus" style="margin-top:14px; text-align:center; font-size:12px; color:var(--text-dim);">Checking local node...</div>
  </div>
</div>

<!-- MAIN APPLICATION SHELL (Hidden until startup auth completes) -->
<div id="appShell" style="display:none;">
  <header class="topbar">
    <div class="topbar-left">
      <img class="topbar-logo" src="/assets/armax_logo.png" alt="ARMX">
      <div class="brand-wordmark">ARMAX<span>COIN</span></div>
    </div>

    <nav class="topbar-tabs" aria-label="Primary Navigation">
      <button class="tab-btn btn-sidebar active" data-tab="overview" onclick="switchTab('overview', this)">Overview</button>
      <button class="tab-btn" data-tab="send" onclick="switchTab('send', this)">Send</button>
      <button class="tab-btn" data-tab="receive" onclick="switchTab('receive', this)">Receive</button>
      <button class="tab-btn" data-tab="miner" onclick="switchTab('miner', this)">CPU Miner</button>
      <button class="tab-btn" data-tab="transactions" onclick="switchTab('transactions', this)">Transactions</button>
      <button class="tab-btn" data-tab="addressbook" onclick="switchTab('addressbook', this)">Address Book</button>
      <button class="tab-btn" data-tab="convert" onclick="switchTab('convert', this)">Convert</button>
      <button class="tab-btn" data-tab="diagnostics" onclick="switchTab('diagnostics', this)">Diagnostics</button>
    </nav>

    <div class="topbar-right">
      <select id="selQuickWallet" class="wallet-quick-select" title="Switch Active Wallet" onchange="selectWallet1Click(this.value)"></select>
      <button type="button" class="theme-toggle-btn" id="btnThemeToggle" title="Toggle Light / Dark Theme" onclick="toggleTheme()">&#9790;</button>
      <span id="lblNetPill" class="net-pill">TESTNET</span>
    </div>
  </header>

  <main class="main-container">
    <div id="netMismatchAlert" class="banner-alert danger" style="display:none;"></div>

    <!-- TAB 1: OVERVIEW (Matches Clean Reference Layout) -->
    <section id="tab-overview" class="tab-pane active">
      <div class="overview-grid">
        <div>
          <div class="card">
            <div class="wallet-meta-row">
              <span id="lblOverviewWalletMeta">Default Wallet</span>
              <span id="lblLockStatusBadge" class="status-pill">Locked</span>
            </div>
            <div class="hero-balance">
              <span id="lblHeroBalance">0.00000000</span>
              <span id="lblHeroTicker" class="hero-ticker dynTicker">{{TICKER}}</span>
            </div>
            <div id="lblHeroSub" class="hero-sub">Confirmed 0.00000000 &middot; 100% settled &middot; Available 0.00000000</div>

            <div class="action-row">
              <button type="button" class="btn" onclick="switchTab('send')">Send</button>
              <button type="button" class="btn btn-dark" onclick="switchTab('receive')">Receive</button>
              <button type="button" class="btn btn-outline" id="btnOverviewUnlockToggle" onclick="handleOverviewLockAction()">Unlock wallet</button>
              <button type="button" class="btn btn-outline" onclick="openWalletManager()">Manage wallets</button>
            </div>
          </div>

          <div class="card">
            <div class="card-header" style="margin-bottom:10px;">
              <div class="card-title">Receiving address</div>
            </div>
            <div id="lblOverviewAddress" class="address-box">Loading address...</div>
            <div class="action-row" style="margin-top:12px;">
              <button type="button" class="btn btn-outline btn-sm" onclick="copyText(currentAddress)">Copy address</button>
              <button type="button" class="btn btn-outline btn-sm" onclick="openModal('visualAddressModal')">Address card</button>
              <button type="button" class="btn btn-outline btn-sm" onclick="copyText(buildBip21URI(currentAddress))">URI</button>
            </div>
          </div>

          <div class="card">
            <div class="card-header">
              <div class="card-title">Recent transactions</div>
              <button type="button" class="btn btn-outline btn-sm" onclick="switchTab('transactions')">View all</button>
            </div>
            <div id="recentTxContainer" style="font-size:13px; color:var(--text-dim);">No transactions yet</div>
          </div>
        </div>

        <div>
          <div class="card">
            <div class="card-title" style="margin-bottom:6px;">CPU Miner</div>
            <div id="lblOverviewMinerState" style="font-size:28px; font-weight:800; line-height:1.15;">Idle</div>
            <div id="lblOverviewHashrate" style="font-size:13px; color:var(--text-dim); margin:6px 0 16px 0;">Hashrate: &mdash; H/s</div>
            <button type="button" id="btnOverviewMineToggle" class="btn btn-block" onclick="toggleMiningFromOverview()">Start mining</button>
          </div>

          <div class="card">
            <div class="card-title" style="margin-bottom:6px;">Network</div>
            <div class="kv-row"><span class="kv-key">Height</span><span id="lblNetHeight" class="kv-val">0</span></div>
            <div class="kv-row"><span class="kv-key">Peers</span><span id="lblNetPeers" class="kv-val">0 active</span></div>
            <div class="kv-row"><span class="kv-key">Difficulty</span><span id="lblNetDiff" class="kv-val">0</span></div>
            <div class="kv-row"><span class="kv-key">Algorithm</span><span id="lblNetAlgo" class="kv-val">Armax-ASERT-AW</span></div>
            <div class="kv-row"><span class="kv-key">Mempool</span><span id="lblNetMempool" class="kv-val">0 txs</span></div>
            <div class="kv-row"><span class="kv-key">Signatures</span><span class="kv-val">ML-DSA-65</span></div>
            <div class="kv-row"><span class="kv-key">Maturity</span><span id="lblNetMaturity" class="kv-val">10 blocks</span></div>
          </div>
        </div>
      </div>
    </section>

    <!-- TAB 2: SEND -->
    <section id="tab-send" class="tab-pane">
      <div class="grid-2">
        <div class="card">
          <div class="card-header">
            <div class="section-title">Send <span id="lblTabSendTicker" class="dynTicker">{{TICKER}}</span></div>
            <span class="status-pill" id="lblSendAvailableBadge">Available: 0.00000000</span>
          </div>
          <div class="form-group">
            <label class="form-label">Recipient Bech32 Address (armx1...)</label>
            <div style="display:flex; gap:8px;">
              <input type="text" class="form-control mono" id="txtSendTo" placeholder="armx1...">
              <button type="button" class="btn btn-outline btn-sm" onclick="switchTab('addressbook')">Contacts</button>
            </div>
          </div>
          <div class="form-group">
            <label class="form-label">Amount (<span class="dynTicker">{{TICKER}}</span>)</label>
            <div style="display:flex; gap:8px;">
              <input type="text" class="form-control mono" id="txtSendAmount" placeholder="0.00000000" oninput="updateSendPreview()">
              <button type="button" class="btn btn-outline btn-sm" onclick="setSendMax()">Max</button>
            </div>
          </div>
          <div class="form-group">
            <label class="form-label">Transaction Fee (<span class="dynTicker">{{TICKER}}</span>) &mdash; <span id="lblFeeRuleNote" style="font-weight:500; color:var(--text-dim);">Calculating from node rule...</span></label>
            <input type="text" class="form-control mono" id="txtSendFee" value="0.00011000" oninput="updateSendPreview()">
            <div class="fee-pills" id="feePresetContainer">
              <button type="button" class="fee-pill active" id="pillFeeEconomy" onclick="selectFeeTier('economy', this)">Economy (Min): 0.00011000</button>
              <button type="button" class="fee-pill" id="pillFeeStandard" onclick="selectFeeTier('standard', this)">Standard: 0.00017000</button>
              <button type="button" class="fee-pill" id="pillFeePriority" onclick="selectFeeTier('priority', this)">Priority: 0.00022000</button>
            </div>
            <div id="lblFeeWarning" style="display:none; margin-top:6px; font-size:12px; font-weight:600; color:var(--danger);"></div>
          </div>
          <div class="form-group" id="sendPassGroup">
            <label class="form-label">Passphrase (only needed if wallet is currently locked in-session)</label>
            <input type="password" class="form-control" id="txtSendPass" placeholder="Leave blank if wallet is already unlocked">
          </div>
          <div class="kv-row" style="margin-bottom:14px;">
            <span class="kv-key">Total Deduction</span>
            <span class="kv-val mono" id="lblSendTotal">0.00011000 {{TICKER}}</span>
          </div>
          <button type="button" class="btn btn-block" id="btnSendTx" onclick="submitSendTransaction()">Broadcast Signed Transaction</button>
        </div>

        <div class="card">
          <div class="card-header"><div class="section-title">Broadcast Status</div></div>
          <div id="sendResultBox" class="address-box" style="min-height:180px;">Ready to sign ML-DSA-65 transactions.</div>
        </div>
      </div>
    </section>

    <!-- TAB 3: RECEIVE -->
    <section id="tab-receive" class="tab-pane">
      <div class="card">
        <div class="card-header"><div class="section-title">Receive <span class="dynTicker">{{TICKER}}</span></div></div>
        <div style="display:flex; gap:24px; flex-wrap:wrap; align-items:flex-start;">
          <div class="qr-frame">
            <img id="imgReceiveQR" src="" alt="QR Code">
          </div>
          <div style="flex:1; min-width:260px;">
            <div class="form-group">
              <label class="form-label">Active Receiving Address (Bech32)</label>
              <div id="lblReceiveAddress" class="address-box"></div>
            </div>
            <div class="form-group">
              <label class="form-label">Visual Verification Blocks (4-character groups)</label>
              <div id="lblReceiveChunks" class="address-box" style="font-size:12px;"></div>
            </div>
            <div class="grid-2">
              <div class="form-group">
                <label class="form-label">Optional Requested Amount (<span class="dynTicker">{{TICKER}}</span>)</label>
                <input type="text" class="form-control mono" id="txtReceiveAmount" placeholder="e.g. 25.0" oninput="refreshReceiveQR()">
              </div>
              <div class="form-group">
                <label class="form-label">BIP-21 Payment URI</label>
                <input type="text" class="form-control mono" id="txtReceiveURI" readonly>
              </div>
            </div>
            <div class="action-row" style="margin-top:8px;">
              <button type="button" class="btn btn-sm" onclick="copyText(currentAddress)">Copy Address</button>
              <button type="button" class="btn btn-dark btn-sm" onclick="copyText(document.getElementById('txtReceiveURI').value)">Copy BIP-21 URI</button>
              <button type="button" class="btn btn-outline btn-sm" onclick="downloadActiveBackup()">Backup Keystore JSON</button>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- TAB 4: CPU MINER -->
    <section id="tab-miner" class="tab-pane">
      <div class="grid-2">
        <div class="card">
          <div class="card-header">
            <div class="section-title">CPU Miner Configuration</div>
            <span id="lblMinerBadge" class="status-pill">IDLE</span>
          </div>
          <div id="minerZeroPeerWarn" class="banner-alert" style="display:none;">
            0 P2P peers connected &mdash; blocks mined solo cannot confirm on the public network until peers sync.
          </div>
          <div id="minerSyncBanner" class="banner-alert" style="display:none; border-color:var(--warn, #e5a93b); color:var(--warn, #e5a93b);">
            Synchronizing blockchain... Mining will begin automatically once sync completes.
          </div>
          <div id="minerStartError" class="banner-alert" style="display:none; border-color:var(--danger); color:var(--danger);"></div>
          <div class="form-group">
            <label class="form-label">Mining Reward Address</label>
            <input type="text" class="form-control mono" id="txtMinerAddress">
            <label style="display:inline-flex; align-items:center; gap:6px; margin-top:8px; font-size:12px; color:var(--text-muted); cursor:pointer;">
              <input type="checkbox" id="chkMinerFollowActive" checked onchange="syncMinerAddrIfChecked()"> Follow active wallet address
            </label>
          </div>
          <div class="grid-2" id="cpuMinerOptions">
            <div class="form-group">
              <label class="form-label">CPU Threads</label>
              <input type="number" class="form-control" id="txtMinerThreads" value="4" min="1">
            </div>
            <div class="form-group">
              <label class="form-label">Round Delay (ms)</label>
              <input type="number" class="form-control" id="txtMinerDelay" value="1000" min="100">
            </div>
          </div>
          <div class="action-row" style="margin-top:6px;">
            <button type="button" class="btn" id="btnStartMining" onclick="startMining()">Start Mining</button>
            <button type="button" class="btn btn-danger" id="btnStopMining" onclick="stopMining()" disabled>Stop Mining</button>
          </div>
        </div>

        <div class="card">
          <div class="card-header"><div class="section-title">Live Session Statistics</div></div>
          <div class="kv-row"><span class="kv-key">Hashrate</span><span id="lblMinerHps" class="kv-val mono">0 H/s</span></div>
          <div class="kv-row"><span class="kv-key">Elapsed Time</span><span id="lblMinerElapsed" class="kv-val mono">00:00:00</span></div>
          <div class="kv-row"><span class="kv-key">Valid Session Blocks</span><span id="lblSessionBlocks" class="kv-val">0</span></div>
          <div class="kv-row"><span class="kv-key">Verified Session Rewards</span><span id="lblSessionRewards" class="kv-val mono">0.00000000 {{TICKER}}</span></div>
          <div class="kv-row"><span class="kv-key">Orphaned / Stale Blocks</span><span id="lblOrphanedBlocks" class="kv-val" style="color:var(--danger);">0</span></div>
        </div>
      </div>

      <div class="card">
        <div class="card-header"><div class="section-title">Session Mined Blocks (Live Main-Chain Verification)</div></div>
        <div class="table-wrap">
          <table>
            <thead>
              <tr><th>Height</th><th>Block Hash</th><th>Reward</th><th>Peers</th><th>Maturity</th><th>Status</th></tr>
            </thead>
            <tbody id="tbodyMinedBlocks">
              <tr><td colspan="6" style="color:var(--text-dim);">No blocks mined in this session yet.</td></tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card">
        <div class="card-header"><div class="section-title">Live Mining Log</div></div>
        <div id="minerLogOutput" class="miner-log-box">[*] Miner ready.</div>
      </div>
    </section>

    <!-- TAB 5: TRANSACTIONS (Local bbolt Cache + Auto Delta Sync) -->
    <section id="tab-transactions" class="tab-pane">
      <div class="card">
        <div class="card-header">
          <div>
            <div class="section-title">Transaction History</div>
            <div id="lblTxSyncMeta" style="font-size:12px; color:var(--text-dim); margin-top:2px;">Local DB ready</div>
          </div>
          <div style="display:flex; gap:8px; flex-wrap:wrap;">
            <button type="button" class="btn btn-outline btn-sm" onclick="exportCSV()">Export CSV</button>
            <button type="button" class="btn btn-sm" onclick="syncHistoryFromDB(true)">Full Resync</button>
          </div>
        </div>

        <div style="display:grid; grid-template-columns:minmax(180px,2fr) 150px 135px 135px auto; gap:8px; margin-bottom:14px;">
          <input type="text" class="form-control" id="txtTxSearch" placeholder="Search TXID, address..." oninput="applyTxFilter()">
          <select class="form-control" id="selTxType" onchange="applyTxFilter()">
            <option value="All">All Types</option>
            <option value="Mined (Coinbase)">Mined (Coinbase)</option>
            <option value="Sent">Sent</option>
            <option value="Received">Received</option>
            <option value="Pending">Pending</option>
          </select>
          <input type="date" class="form-control" id="txtTxDateFrom" onchange="applyTxFilter()">
          <input type="date" class="form-control" id="txtTxDateTo" onchange="applyTxFilter()">
          <button type="button" class="btn btn-outline btn-sm" onclick="clearTxFilters()">Clear</button>
        </div>

        <div class="table-wrap">
          <table>
            <thead>
              <tr><th>Type</th><th>Amount</th><th>Status / Height</th><th>TXID</th><th>Timestamp</th></tr>
            </thead>
            <tbody id="tbodyTransactions">
              <tr><td colspan="5" style="color:var(--text-dim);">Loading cached history...</td></tr>
            </tbody>
          </table>
        </div>

        <div style="display:flex; align-items:center; justify-content:space-between; margin-top:14px; flex-wrap:wrap; gap:10px;">
          <div id="lblTxPageInfo" style="font-size:12px; color:var(--text-dim);">Page 1 / 1</div>
          <div style="display:flex; gap:6px;">
            <button type="button" class="btn btn-outline btn-sm" onclick="setTxPage(0)">First</button>
            <button type="button" class="btn btn-outline btn-sm" onclick="setTxPage(currentTxPage-1)">Prev</button>
            <button type="button" class="btn btn-outline btn-sm" onclick="setTxPage(currentTxPage+1)">Next</button>
            <button type="button" class="btn btn-outline btn-sm" onclick="setTxPage(999999)">Last</button>
          </div>
        </div>
      </div>
    </section>

    <!-- TAB 6: ADDRESS BOOK -->
    <section id="tab-addressbook" class="tab-pane">
      <div class="grid-2">
        <div class="card">
          <div class="card-header"><div class="section-title">Add Contact</div></div>
          <div class="form-group">
            <label class="form-label">Contact Name</label>
            <input type="text" class="form-control" id="txtContactName" placeholder="e.g. Treasury / Friend">
          </div>
          <div class="form-group">
            <label class="form-label">Armaxcoin Address (armx1...)</label>
            <input type="text" class="form-control mono" id="txtContactAddr" placeholder="armx1...">
          </div>
          <button type="button" class="btn" onclick="addContact()">Save Contact</button>
        </div>
        <div class="card">
          <div class="card-header"><div class="section-title">Saved Contacts</div></div>
          <div class="table-wrap">
            <table>
              <thead><tr><th>Name</th><th>Address</th><th>Actions</th></tr></thead>
              <tbody id="tbodyContacts">
                <tr><td colspan="3" style="color:var(--text-dim);">No contacts saved yet.</td></tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </section>

    <!-- TAB 7: CONVERT (UNDER DEVELOPMENT) -->
    <section id="tab-convert" class="tab-pane">
      <div class="card" style="max-width:720px; margin:0 auto;">
        <div class="card-header">
          <div class="section-title">Testnet &rarr; Mainnet Conversion (tARMX &rarr; ARMX)</div>
          <span class="status-pill warn">UNDER DEVELOPMENT</span>
        </div>
        <p style="font-size:13px; color:var(--text-muted); margin-bottom:16px;">
          This module is being prepared so eligible testnet testers can convert or claim <b>tARMX</b> balances into <b>ARMX</b> once the Mainnet bridge &amp; snapshot go live.
        </p>
        <div class="grid-2">
          <div class="form-group">
            <label class="form-label">Source Asset</label>
            <input type="text" class="form-control" value="tARMX (Armaxcoin Testnet)" readonly>
          </div>
          <div class="form-group">
            <label class="form-label">Destination Asset</label>
            <input type="text" class="form-control" value="ARMX (Armaxcoin Mainnet)" readonly>
          </div>
          <div class="form-group">
            <label class="form-label">Amount (tARMX)</label>
            <input type="text" class="form-control" placeholder="Enabled on Mainnet launch" disabled>
          </div>
          <div class="form-group">
            <label class="form-label">Destination Mainnet Address</label>
            <input type="text" class="form-control" placeholder="armx1..." disabled>
          </div>
        </div>
        <button type="button" class="btn" disabled>Convert Tokens (Coming Soon)</button>
      </div>
    </section>

    <!-- TAB 8: DIAGNOSTICS (ENLARGED COMMAND CONSOLE LOG) -->
    <section id="tab-diagnostics" class="tab-pane">
      <div class="card">
        <div class="card-header">
          <div>
            <div class="section-title">Interactive RPC Command Console &amp; Log</div>
            <div id="lblDiagSummary" style="font-size:12px; color:var(--text-dim); margin-top:2px;">Height: 0 &middot; Peers: 0 active &middot; Mempool: 0 txs</div>
          </div>
          <div style="display:flex; gap:6px; flex-wrap:wrap;">
            <button type="button" class="btn btn-outline btn-sm" onclick="runCLI('getnetworkinfo')">/stats</button>
            <button type="button" class="btn btn-outline btn-sm" onclick="runCLI('getpeerinfo')">/peers</button>
            <button type="button" class="btn btn-outline btn-sm" onclick="fetchRawEndpoint('/api/chain')">/chain</button>
            <button type="button" class="btn btn-outline btn-sm" onclick="runCLI('getmempoolinfo')">/mempool</button>
            <button type="button" class="btn btn-outline btn-sm" id="btnToggleConsoleSize" onclick="toggleConsoleExpand()">Expand Log (78vh)</button>
            <button type="button" class="btn btn-outline btn-sm" onclick="copyText(document.getElementById('txtCLIOutput').innerText)">Copy Output</button>
            <button type="button" class="btn btn-outline btn-sm" onclick="document.getElementById('txtCLIOutput').innerText = ''">Clear</button>
          </div>
        </div>
        <div style="display:flex; gap:8px; margin-bottom:12px;">
          <input type="text" class="form-control mono" id="txtCLICommand" placeholder="Commands: help, getnetworkinfo, getblockchaininfo, getpeerinfo, getbalance [addr], gethistory [addr], getmempoolinfo" onkeydown="if(event.key==='Enter') executeConsoleInput()">
          <button type="button" class="btn" onclick="executeConsoleInput()">Execute</button>
        </div>
        <pre id="txtCLIOutput" class="console-output">Click any diagnostic query above or enter an RPC command to inspect full node output.</pre>
      </div>

      <div class="card">
        <div class="card-header">
          <div class="section-title">Connected Public Peers &amp; Seeds (Privacy Filtered)</div>
          <button type="button" class="btn btn-outline btn-sm" onclick="loadPeerInspector()">Refresh Peers</button>
        </div>
        <div class="table-wrap">
          <table>
            <thead><tr><th>Peer Endpoint</th><th>Role</th><th>Status</th></tr></thead>
            <tbody id="tbodyPeerInspector">
              <tr><td colspan="3" style="color:var(--text-dim);">Loading safe peer list...</td></tr>
            </tbody>
          </table>
        </div>
      </div>
    </section>
  </main>
</div>

<!-- MODAL: SETUP NEW WALLET -->
<div class="modal-backdrop" id="setupWalletModal">
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">Create New Armaxcoin Wallet</div>
      <button class="modal-close" onclick="closeModal('setupWalletModal')">&times;</button>
    </div>
    <div class="modal-body">
      <div class="form-group">
        <label class="form-label">Wallet Label (optional)</label>
        <input type="text" class="form-control" id="txtModalCreateLabel" placeholder="Primary Wallet">
      </div>
      <div class="form-group">
        <label class="form-label">New Keystore Passphrase (minimum 12 characters)</label>
        <input type="password" class="form-control" id="txtModalCreatePass" placeholder="Enter strong passphrase...">
      </div>
      <div class="form-group">
        <label class="form-label">Confirm Keystore Passphrase</label>
        <input type="password" class="form-control" id="txtModalCreatePassConfirm" placeholder="Confirm passphrase...">
      </div>
      <div id="modalCreateActions" style="display:flex; justify-content:flex-end; gap:8px; margin-top:16px;">
        <button class="btn btn-outline" type="button" onclick="closeModal('setupWalletModal')">Cancel</button>
        <button class="btn" type="button" id="btnModalCreateWallet" onclick="executeModalCreateWallet()">Generate &amp; Save Wallet</button>
      </div>
      <div id="modalCreateMnemonicBox" style="margin-top:14px; display:none; padding:14px; border-radius:10px; background:var(--console-bg); color:var(--console-fg); border:1px solid var(--success); font-family:var(--font-mono); font-size:13px;"></div>
    </div>
  </div>
</div>

<!-- MODAL: RESTORE WALLET -->
<div class="modal-backdrop" id="restoreWalletModal">
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">Restore Wallet from 24-Word Seed</div>
      <button class="modal-close" onclick="closeModal('restoreWalletModal')">&times;</button>
    </div>
    <div class="modal-body">
      <div class="form-group">
        <label class="form-label">Wallet Label (optional)</label>
        <input type="text" class="form-control" id="txtModalRestoreLabel" placeholder="Restored Wallet">
      </div>
      <div class="form-group">
        <label class="form-label">Recovery Mnemonic Seed Phrase</label>
        <input type="text" class="form-control mono" id="txtModalRestoreMnemonic" placeholder="word1 word2 ... word24">
      </div>
      <div class="form-group">
        <label class="form-label">New Keystore Passphrase (minimum 12 characters)</label>
        <input type="password" class="form-control" id="txtModalRestorePass" placeholder="Enter passphrase to encrypt keystore...">
      </div>
      <div style="display:flex; justify-content:flex-end; gap:8px; margin-top:16px;">
        <button class="btn btn-outline" type="button" onclick="closeModal('restoreWalletModal')">Cancel</button>
        <button class="btn" type="button" onclick="executeModalRestoreWallet()">Restore Wallet</button>
      </div>
    </div>
  </div>
</div>

<!-- MODAL: MULTI-WALLET MANAGER -->
<div class="modal-backdrop" id="walletManagerModal">
  <div class="modal" style="max-width:740px;">
    <div class="modal-header">
      <div class="modal-title">Multi-Wallet Manager</div>
      <button class="modal-close" onclick="closeModal('walletManagerModal')">&times;</button>
    </div>
    <div class="modal-body">
      <div id="walletManagerList" style="margin-bottom:16px;">Loading wallets...</div>
      <div class="grid-2" style="border-top:1px solid var(--border); padding-top:16px;">
        <div>
          <div style="font-weight:700; margin-bottom:8px;">Create Managed Wallet</div>
          <div class="form-group">
            <label class="form-label">Label</label>
            <input type="text" class="form-control" id="txtMgrCreateLabel" placeholder="e.g. Treasury">
          </div>
          <div class="form-group">
            <label class="form-label">Passphrase (min 12 chars)</label>
            <input type="password" class="form-control" id="txtMgrCreatePass" placeholder="Passphrase...">
          </div>
          <button class="btn btn-sm" type="button" onclick="createManagedWallet()">Create &amp; Switch</button>
          <div id="mgrMnemonicBox" style="margin-top:10px; display:none; padding:10px; background:var(--console-bg); color:var(--console-fg); border-radius:8px; font-family:var(--font-mono); font-size:12px;"></div>
        </div>
        <div>
          <div style="font-weight:700; margin-bottom:8px;">Import Encrypted Keystore JSON</div>
          <div class="form-group">
            <label class="form-label">Label</label>
            <input type="text" class="form-control" id="txtMgrImportLabel" placeholder="e.g. Backup Keystore">
          </div>
          <div class="form-group">
            <label class="form-label">Keystore File (.json)</label>
            <input type="file" class="form-control" id="fileMgrImportJSON" accept=".json,application/json">
          </div>
          <button class="btn btn-outline btn-sm" type="button" onclick="importKeystoreWallet()">Import Keystore</button>
        </div>
      </div>
      <div style="border-top:1px solid var(--border); margin-top:16px; padding-top:14px; display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:10px;">
        <div style="display:flex; align-items:center; gap:8px;">
          <label class="form-label" style="margin:0;">Idle Auto-Lock:</label>
          <select id="selAutoLockMins" class="form-control" style="width:auto; padding:6px 10px;" onchange="setAutoLockMinutes(this.value)">
            <option value="0">Off</option>
            <option value="5">5 minutes</option>
            <option value="15" selected>15 minutes</option>
            <option value="30">30 minutes</option>
            <option value="60">60 minutes</option>
          </select>
        </div>
        <button class="btn btn-danger btn-sm" type="button" onclick="lockAllWallets()">Lock All Keys</button>
      </div>
    </div>
  </div>
</div>

<!-- MODAL: IN-SESSION UNLOCK WALLET -->
<div class="modal-backdrop" id="unlockSessionModal">
  <div class="modal" style="max-width:420px;">
    <div class="modal-header">
      <div class="modal-title">Unlock Active Wallet</div>
      <button class="modal-close" onclick="closeModal('unlockSessionModal')">&times;</button>
    </div>
    <div class="modal-body">
      <div class="form-group">
        <label class="form-label">Keystore Passphrase</label>
        <input type="password" class="form-control" id="txtSessionUnlockPass" placeholder="Enter passphrase..." onkeydown="if(event.key==='Enter') executeSessionUnlock()">
      </div>
      <div style="display:flex; justify-content:flex-end; gap:8px;">
        <button class="btn btn-outline" type="button" onclick="closeModal('unlockSessionModal')">Cancel</button>
        <button class="btn" type="button" onclick="executeSessionUnlock()">Unlock</button>
      </div>
    </div>
  </div>
</div>

<!-- MODAL: VISUAL ADDRESS CARD -->
<div class="modal-backdrop" id="visualAddressModal">
  <div class="modal" style="max-width:540px;">
    <div class="modal-header">
      <div class="modal-title">Visual Address Card</div>
      <button class="modal-close" onclick="closeModal('visualAddressModal')">&times;</button>
    </div>
    <div class="modal-body">
      <div class="form-group">
        <label class="form-label">4-Character Verification Blocks</label>
        <div id="lblModalAddressBlocks" class="address-box"></div>
      </div>
      <div class="form-group">
        <label class="form-label">BIP-21 Payment URI</label>
        <div id="lblModalAddressURI" class="address-box" style="font-size:12px;"></div>
      </div>
      <div style="display:flex; justify-content:flex-end; gap:8px;">
        <button class="btn btn-outline btn-sm" type="button" onclick="copyText(currentAddress)">Copy Address</button>
        <button class="btn btn-sm" type="button" onclick="closeModal('visualAddressModal')">Close</button>
      </div>
    </div>
  </div>
</div>

<!-- MODAL: TRANSACTION DETAILS -->
<div class="modal-backdrop" id="txDetailsModal">
  <div class="modal" style="max-width:600px;">
    <div class="modal-header">
      <div class="modal-title">Transaction Details</div>
      <button class="modal-close" onclick="closeModal('txDetailsModal')">&times;</button>
    </div>
    <div class="modal-body" id="txDetailsBody"></div>
  </div>
</div>

<script>
const GUI_SESSION_TOKEN = "{{SESSION_TOKEN}}";

const _nativeFetch = window.fetch;
window.fetch = function(resource, init) {
  init = init || {};
  let headers = init.headers;
  if (!headers) {
    headers = {};
  } else if (headers instanceof Headers) {
    const h = {};
    headers.forEach((v, k) => { h[k] = v; });
    headers = h;
  } else {
    headers = Object.assign({}, headers);
  }
  if (GUI_SESSION_TOKEN && !headers['X-Armx-GUI-Token']) {
    headers['X-Armx-GUI-Token'] = GUI_SESSION_TOKEN;
  }
  init.headers = headers;
  return _nativeFetch(resource, init);
};

let appStarted = false;
let currentAddress = "";
let currentWalletID = "";
let currentWalletLabel = "Default Wallet";
let currentWalletUnlocked = false;
let currentTicker = "{{TICKER}}";
let currentMaturity = 10;
let currentSpendableArmoids = 0;
let currentFeeEstimate = { min_fee_armoids: 11000, economy_armoids: 11000, standard_armoids: 17000, priority_armoids: 22000 };
let currentTxRecords = [];
let filteredTxRecords = [];
let currentTxPage = 0;
let lastSyncedHeight = -1;
let lastSyncedMempool = -1;
let historySyncInFlight = false;
let contactsList = [];
let managedWallets = [];

function escapeHTML(val) {
  return String(val === undefined || val === null ? '' : val)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function applyTheme(theme) {
  const t = theme === 'dark' ? 'dark' : 'light';
  document.documentElement.setAttribute('data-theme', t);
  try { localStorage.setItem('armax_gui_theme', t); } catch (_) {}
  const btn = document.getElementById('btnThemeToggle');
  if (btn) btn.innerHTML = t === 'dark' ? '&#9728;' : '&#9790;';
}

function toggleTheme() {
  const cur = document.documentElement.getAttribute('data-theme') || 'light';
  applyTheme(cur === 'dark' ? 'light' : 'dark');
}

(function initSavedTheme() {
  let saved = 'light';
  try { saved = localStorage.getItem('armax_gui_theme') || 'light'; } catch (_) {}
  applyTheme(saved);
})();

function openModal(id) { document.getElementById(id).classList.add('active'); }
function closeModal(id) { document.getElementById(id).classList.remove('active'); }

function copyText(val) {
  if (!val) return;
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(val);
  }
}

function formatArmx(armoids) {
  return ((armoids || 0) / 100000000).toFixed(8) + " " + currentTicker;
}

function formatNum(armoids) {
  return ((armoids || 0) / 100000000).toFixed(8);
}

function formatHashrate(hps) {
  if (hps >= 1e9) return (hps / 1e9).toFixed(2) + " GH/s";
  if (hps >= 1e6) return (hps / 1e6).toFixed(2) + " MH/s";
  if (hps >= 1e3) return (hps / 1e3).toFixed(2) + " kH/s";
  return (hps > 0 ? Math.round(hps) + " H/s" : "— H/s");
}

function chunkAddress4(addr) {
  if (!addr) return "";
  const chunks = [];
  for (let i = 0; i < addr.length; i += 4) chunks.push(addr.slice(i, i + 4));
  return chunks.join('  ');
}

function buildBip21URI(addr) {
  if (!addr) return "";
  const amtRaw = (document.getElementById('txtReceiveAmount')?.value || '').trim();
  const params = [];
  if (amtRaw && parseFloat(amtRaw) > 0) params.push('amount=' + encodeURIComponent(amtRaw));
  params.push('label=DesktopWallet');
  return 'armax:' + addr + '?' + params.join('&');
}

function switchTab(name, btnEl) {
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));
  const targetBtn = btnEl || document.querySelector('.tab-btn[data-tab="' + name + '"]');
  if (targetBtn) targetBtn.classList.add('active');
  const pane = document.getElementById('tab-' + name);
  if (pane) pane.classList.add('active');

  // Transactions tab renders IMMEDIATELY from local cache without re-fetching all history.
  if (name === 'transactions') {
    applyTxFilter();
  }
  if (name === 'send') {
    loadFeeEstimate();
  }
  if (name === 'addressbook') {
    loadContacts();
  }
  if (name === 'diagnostics') {
    loadPeerInspector();
  }
}

async function exitDesktopApp() {
  try {
    await fetch('/api/app/exit', { method: 'POST' });
  } catch (_) {}
  try { window.close(); } catch (_) {}
  document.body.innerHTML = '<div style="display:flex;height:100vh;align-items:center;justify-content:center;font-family:sans-serif;color:#64748b;">Armaxcoin Wallet closed safely.</div>';
}

let nodeStatusTimer = null;
let nodeRetryScheduled = false;

async function pollNodeBootstrapStatus() {
  try {
    const res = await fetch('/api/node/status');
    if (!res.ok) return null;
    const st = await res.json();
    const lockEl = document.getElementById('lblStartupNodeStatus');
    const onbEl = document.getElementById('lblOnboardingNodeStatus');
    let msg = st.detail || 'Checking local node...';
    let color = 'var(--text-dim)';
    if (st.state === 'online') {
      color = 'var(--success)';
      if (nodeStatusTimer) { clearInterval(nodeStatusTimer); nodeStatusTimer = null; }
    } else if (st.state === 'error') {
      color = 'var(--danger)';
      msg = 'Node error: ' + (st.error || st.detail || 'Unavailable');
    }
    if (lockEl) { lockEl.innerText = msg; lockEl.style.color = color; }
    if (onbEl) { onbEl.innerText = msg; onbEl.style.color = color; }
    return st;
  } catch (_) {
    return null;
  }
}

async function retryEmbeddedNode() {
  try {
    await fetch('/api/node/retry', { method: 'POST' });
    pollNodeBootstrapStatus();
    setTimeout(pollStatsAndBalance, 800);
  } catch (_) {}
}

async function bootstrapApplication() {
  const loadScreen = document.getElementById('startupLoadingScreen');
  const loadText = document.getElementById('lblLoadingStatus');
  const loadRetry = document.getElementById('btnLoadingRetry');
  if (loadRetry) loadRetry.style.display = 'none';
  if (loadText) {
    loadText.style.color = 'var(--text-muted)';
    loadText.innerText = 'Loading Armaxcoin Core Wallet...';
  }

  if (!nodeStatusTimer) {
    pollNodeBootstrapStatus();
    nodeStatusTimer = setInterval(pollNodeBootstrapStatus, 1500);
  }
  try {
    const res = await fetch('/api/wallet/info');
    if (!res.ok) {
      const errMsg = await res.text();
      if (loadText) {
        loadText.style.color = 'var(--danger)';
        loadText.innerText = 'Initialization error (HTTP ' + res.status + '): ' + (errMsg || 'Failed to load wallet info');
      }
      if (loadRetry) loadRetry.style.display = 'inline-flex';
      return;
    }
    const info = await res.json();

    applyDynamicTicker(info.ticker || '{{TICKER}}');

    if (!info.initialized) {
      if (loadScreen) loadScreen.classList.remove('active');
      document.getElementById('lblMissingWalletPath').innerText = info.wallet_path || 'wallet.json';
      document.getElementById('startupOnboardingGate').classList.add('active');
      document.getElementById('startupLockScreen').classList.remove('active');
      document.getElementById('appShell').style.display = 'none';
      return;
    }

    if (info.startup_locked) {
      if (loadScreen) loadScreen.classList.remove('active');
      const label = info.active_wallet_label || 'Active Wallet';
      document.getElementById('lblStartupWalletDesc').innerText =
        'Enter passphrase to unlock "' + label + '" (' + (info.network || 'testnet') + ').';
      document.getElementById('startupLockScreen').classList.add('active');
      document.getElementById('startupOnboardingGate').classList.remove('active');
      document.getElementById('appShell').style.display = 'none';
      setTimeout(() => document.getElementById('txtStartupPass')?.focus(), 50);
      return;
    }

    enterAuthenticatedApp(info);
  } catch (e) {
    if (loadText) {
      loadText.style.color = 'var(--danger)';
      loadText.innerText = 'Initialization error: ' + e.message;
    }
    if (loadRetry) loadRetry.style.display = 'inline-flex';
  }
}

function enterAuthenticatedApp(info) {
  document.getElementById('startupLoadingScreen')?.classList.remove('active');
  document.getElementById('startupLockScreen').classList.remove('active');
  document.getElementById('startupOnboardingGate').classList.remove('active');
  document.getElementById('appShell').style.display = 'flex';

  applyActiveWalletIdentity(info.active_wallet_id || '', info.active_wallet_label || 'Default Wallet', info.address || '', !!info.unlocked);

  if (!appStarted) {
    appStarted = true;
    loadWallets();
    loadFeeEstimate();
    pollStatsAndBalance();
    syncHistoryFromDB(false);
    setInterval(pollStatsAndBalance, 3000);
    setInterval(pollMinerStatus, 2500);
  }
}

async function unlockStartupWallet() {
  const pass = document.getElementById('txtStartupPass').value;
  const errBox = document.getElementById('startupLockError');
  errBox.style.display = 'none';
  if (!pass) {
    errBox.innerText = 'Passphrase is required.';
    errBox.style.display = 'block';
    return;
  }
  const btn = document.getElementById('btnStartupUnlock');
  btn.disabled = true;
  btn.innerText = 'Decrypting...';
  try {
    const res = await fetch('/api/wallets/unlock', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ passphrase: pass })
    });
    if (!res.ok) {
      errBox.innerText = await res.text();
      errBox.style.display = 'block';
      return;
    }
    document.getElementById('txtStartupPass').value = '';
    const infoRes = await fetch('/api/wallet/info');
    const info = await infoRes.json();
    enterAuthenticatedApp(info);
  } catch (e) {
    errBox.innerText = 'Failed to unlock: ' + e.message;
    errBox.style.display = 'block';
  } finally {
    btn.disabled = false;
    btn.innerText = 'Unlock Wallet';
  }
}

function applyDynamicTicker(ticker) {
  if (!ticker) return;
  currentTicker = ticker;
  document.querySelectorAll('.dynTicker').forEach(el => { el.innerText = ticker; });
}

function applyActiveWalletIdentity(id, label, addr, unlocked) {
  currentWalletID = id;
  currentWalletLabel = label || 'Default Wallet';
  currentAddress = addr;
  currentWalletUnlocked = !!unlocked;

  const shortAddr = addr.length > 16 ? addr.slice(0, 8) + '...' + addr.slice(-6) : addr;
  document.getElementById('lblOverviewWalletMeta').innerText = currentWalletLabel + (shortAddr ? ' · ' + shortAddr : '');

  const badge = document.getElementById('lblLockStatusBadge');
  const toggleBtn = document.getElementById('btnOverviewUnlockToggle');
  if (currentWalletUnlocked) {
    badge.innerText = 'Unlocked';
    badge.className = 'status-pill unlocked';
    toggleBtn.innerText = 'Lock keys';
  } else {
    badge.innerText = 'Locked · Watch-only';
    badge.className = 'status-pill';
    toggleBtn.innerText = 'Unlock wallet';
  }

  document.getElementById('lblOverviewAddress').innerText = addr || '—';
  document.getElementById('lblReceiveAddress').innerText = addr || '—';
  const blocks = chunkAddress4(addr);
  document.getElementById('lblReceiveChunks').innerText = blocks || '—';
  document.getElementById('lblModalAddressBlocks').innerText = blocks || '—';
  document.getElementById('lblModalAddressURI').innerText = buildBip21URI(addr);

  const followChk = document.getElementById('chkMinerFollowActive');
  if (followChk && followChk.checked) {
    document.getElementById('txtMinerAddress').value = addr || '';
  }
  refreshReceiveQR();
}

function refreshReceiveQR() {
  if (!currentAddress) return;
  const uri = buildBip21URI(currentAddress);
  document.getElementById('txtReceiveURI').value = uri;
  document.getElementById('lblModalAddressURI').innerText = uri;
  const img = document.getElementById('imgReceiveQR');
  if (img) {
    img.src = '/api/qr?content=' + encodeURIComponent(uri) + '&token=' + encodeURIComponent(GUI_SESSION_TOKEN);
  }
}

async function handleOverviewLockAction() {
  if (currentWalletUnlocked) {
    await lockAllWallets();
  } else {
    document.getElementById('txtSessionUnlockPass').value = '';
    openModal('unlockSessionModal');
    setTimeout(() => document.getElementById('txtSessionUnlockPass')?.focus(), 50);
  }
}

async function executeSessionUnlock() {
  const pass = document.getElementById('txtSessionUnlockPass').value;
  if (!pass) return;
  const res = await fetch('/api/wallets/unlock', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: currentWalletID, passphrase: pass })
  });
  if (!res.ok) {
    alert(await res.text());
    return;
  }
  document.getElementById('txtSessionUnlockPass').value = '';
  closeModal('unlockSessionModal');
  await loadWallets();
  await loadFeeEstimate();
}

async function loadWallets() {
  const res = await fetch('/api/wallets');
  if (!res.ok) return;
  const data = await res.json();
  managedWallets = data.wallets || [];
  currentWalletID = data.active_id || currentWalletID;

  const sel = document.getElementById('selQuickWallet');
  if (sel) {
    sel.innerHTML = managedWallets.map(w => {
      const mark = w.unlocked ? ' [Unlocked]' : '';
      return '<option value="' + escapeHTML(w.id) + '"' + (w.id === currentWalletID ? ' selected' : '') + '>' + escapeHTML(w.label) + mark + '</option>';
    }).join('');
  }

  const active = managedWallets.find(w => w.id === currentWalletID) || managedWallets[0];
  if (active) {
    applyActiveWalletIdentity(active.id, active.label, active.address, active.unlocked);
  }

  const autoSel = document.getElementById('selAutoLockMins');
  if (autoSel && typeof data.auto_lock_minutes === 'number') {
    autoSel.value = String(data.auto_lock_minutes);
  }
  renderWalletManagerTable();
}

function renderWalletManagerTable() {
  const wrap = document.getElementById('walletManagerList');
  if (!wrap) return;
  if (managedWallets.length === 0) {
    wrap.innerHTML = '<div style="color:var(--text-dim);">No managed wallets found.</div>';
    return;
  }
  wrap.innerHTML = '<div class="table-wrap"><table><thead><tr><th>Label</th><th>Address</th><th>State</th><th>Actions</th></tr></thead><tbody>' +
    managedWallets.map(w => {
      const shortA = w.address.slice(0, 10) + '...' + w.address.slice(-6);
      const statePill = w.unlocked ? '<span class="status-pill unlocked">Unlocked</span>' : '<span class="status-pill">Locked</span>';
      const switchBtn = w.is_active ? '<span class="status-pill">Active</span>' : '<button class="btn btn-sm" onclick="selectWallet1Click(\'' + escapeHTML(w.id) + '\')">Switch</button>';
      return '<tr><td><b>' + escapeHTML(w.label) + '</b></td><td class="mono">' + escapeHTML(shortA) + '</td><td>' + statePill + '</td>' +
        '<td style="display:flex;gap:6px;flex-wrap:wrap;">' + switchBtn +
        '<button class="btn btn-outline btn-sm" onclick="renameManagedWallet(\'' + escapeHTML(w.id) + '\',\'' + escapeHTML(w.label) + '\')">Rename</button>' +
        '<a class="btn btn-outline btn-sm" href="/api/wallets/backup?id=' + encodeURIComponent(w.id) + '&token=' + encodeURIComponent(GUI_SESSION_TOKEN) + '">Backup</a>' +
        '</td></tr>';
    }).join('') + '</tbody></table></div>';
}

function openWalletManager() {
  loadWallets();
  openModal('walletManagerModal');
}

async function selectWallet1Click(id) {
  if (!id) return;
  const res = await fetch('/api/wallets/select', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id })
  });
  if (!res.ok) { alert(await res.text()); return; }
  lastSyncedHeight = -1;
  currentTxRecords = [];
  await loadWallets();
  await loadFeeEstimate();
  await pollStatsAndBalance();
  await syncHistoryFromDB(false);
}

async function renameManagedWallet(id, oldLabel) {
  const newLabel = prompt('New label for wallet:', oldLabel);
  if (!newLabel || !newLabel.trim()) return;
  const res = await fetch('/api/wallets/rename', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, label: newLabel.trim() })
  });
  if (!res.ok) { alert(await res.text()); return; }
  await loadWallets();
}

async function createManagedWallet() {
  const label = document.getElementById('txtMgrCreateLabel').value.trim();
  const pass = document.getElementById('txtMgrCreatePass').value;
  const res = await fetch('/api/wallets/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ label, passphrase: pass })
  });
  if (!res.ok) { alert(await res.text()); return; }
  const out = await res.json();
  document.getElementById('txtMgrCreatePass').value = '';
  const box = document.getElementById('mgrMnemonicBox');
  box.innerText = 'BACKUP 24-WORD SEED ONCE:\n' + out.mnemonic;
  box.style.display = 'block';
  lastSyncedHeight = -1;
  await loadWallets();
  await pollStatsAndBalance();
  await syncHistoryFromDB(false);
}

async function importKeystoreWallet() {
  const label = document.getElementById('txtMgrImportLabel').value.trim();
  const fileEl = document.getElementById('fileMgrImportJSON');
  if (!fileEl.files || fileEl.files.length === 0) { alert('Select a keystore JSON file first.'); return; }
  const rawText = await fileEl.files[0].text();
  const res = await fetch('/api/wallets/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ label, keystore: rawText })
  });
  if (!res.ok) { alert(await res.text()); return; }
  lastSyncedHeight = -1;
  await loadWallets();
  await pollStatsAndBalance();
  await syncHistoryFromDB(false);
}

async function lockAllWallets() {
  await fetch('/api/wallets/lock', { method: 'POST' });
  await loadWallets();
}

async function setAutoLockMinutes(val) {
  await fetch('/api/wallets/autolock', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ minutes: parseInt(val, 10) || 0 })
  });
}

async function executeModalCreateWallet() {
  const label = (document.getElementById('txtModalCreateLabel')?.value || '').trim();
  const pass = document.getElementById('txtModalCreatePass').value;
  const confirmPass = document.getElementById('txtModalCreatePassConfirm').value;
  const box = document.getElementById('modalCreateMnemonicBox');
  const btn = document.getElementById('btnModalCreateWallet');
  if (!pass || pass.length < 12) { alert("Passphrase must be at least 12 characters."); return; }
  if (pass !== confirmPass) { alert("Passphrases do not match."); return; }
  btn.disabled = true;
  try {
    const res = await fetch('/api/wallet/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ label, passphrase: pass })
    });
    if (!res.ok) { alert(await res.text()); return; }
    const out = await res.json();
    box.innerHTML = '<b>WRITE DOWN YOUR 24-WORD RECOVERY SEED NOW:</b><br><br>' + escapeHTML(out.mnemonic) +
      '<div style="margin-top:12px;"><button class="btn btn-sm" type="button" onclick="closeModal(\'setupWalletModal\'); bootstrapApplication();">Continue to Wallet</button></div>';
    box.style.display = 'block';
  } finally {
    btn.disabled = false;
  }
}

async function executeModalRestoreWallet() {
  const label = (document.getElementById('txtModalRestoreLabel')?.value || '').trim();
  const mnemonic = document.getElementById('txtModalRestoreMnemonic').value.trim();
  const pass = document.getElementById('txtModalRestorePass').value;
  if (!mnemonic) { alert("Please enter your 12 or 24-word BIP-39 recovery phrase."); return; }
  if (!pass || pass.length < 12) { alert("Passphrase must be at least 12 characters."); return; }
  const res = await fetch('/api/wallet/restore', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ label, mnemonic, passphrase: pass })
  });
  if (!res.ok) { alert(await res.text()); return; }
  closeModal('restoreWalletModal');
  await bootstrapApplication();
}

async function showNodeStatusFallback(fallbackMsg) {
  const alertBox = document.getElementById('netMismatchAlert');
  if (!alertBox) return;
  const st = await pollNodeBootstrapStatus();
  if (st && st.state === 'starting') {
    alertBox.className = 'banner-alert';
    alertBox.innerText = 'Starting embedded Armaxcoin node: ' + (st.detail || 'Initializing...');
    alertBox.style.display = 'block';
    if (!nodeRetryScheduled) {
      nodeRetryScheduled = true;
      setTimeout(() => { nodeRetryScheduled = false; pollStatsAndBalance(); }, 1200);
    }
    return;
  }
  alertBox.className = 'banner-alert danger';
  if (st && st.state === 'error') {
    alertBox.innerHTML = 'Embedded node error: ' + escapeHTML(st.error || st.detail || fallbackMsg) +
      ' <button type="button" class="btn btn-outline btn-sm" style="margin-left:10px;" onclick="retryEmbeddedNode()">Retry Node</button>';
  } else {
    alertBox.innerText = fallbackMsg;
  }
  alertBox.style.display = 'block';
}

async function pollStatsAndBalance() {
  const alertBox = document.getElementById('netMismatchAlert');
  try {
    const sRes = await fetch('/api/stats');
    if (!sRes.ok) {
      await showNodeStatusFallback('Node connection warning: ' + await sRes.text());
      return;
    }
    alertBox.style.display = 'none';
    alertBox.className = 'banner-alert danger';
    const st = await sRes.json();

    if (st.ticker) applyDynamicTicker(st.ticker);
    if (st.coinbase_maturity > 0) currentMaturity = st.coinbase_maturity;

    const netPill = document.getElementById('lblNetPill');
    const netID = String(st.network_id || '');
    if (netID.indexOf('MAINNET') !== -1) {
      netPill.innerText = 'MAINNET';
      netPill.className = 'net-pill mainnet';
    } else if (netID.indexOf('REGTEST') !== -1) {
      netPill.innerText = 'REGTEST';
      netPill.className = 'net-pill regtest';
    } else {
      netPill.innerText = 'TESTNET';
      netPill.className = 'net-pill';
    }

    const h = Number(st.height || 0);
    const p = Number(st.peers_active || 0);
    const mp = Number(st.mempool_size || st.mempool || 0);
    document.getElementById('lblNetHeight').innerText = h.toLocaleString();
    document.getElementById('lblNetPeers').innerText = p + ' active';
    document.getElementById('lblNetDiff').innerText = (st.difficulty_formatted ? st.difficulty_formatted + ' · ' : '') + 'Diff ' + (st.difficulty || 0);
    document.getElementById('lblNetAlgo').innerText = st.daa_engine || 'Armax-ASERT-AW';
    document.getElementById('lblNetMempool').innerText = mp + ' txs';
    document.getElementById('lblNetMaturity').innerText = currentMaturity + ' blocks';
    document.getElementById('lblDiagSummary').innerText = 'Height: ' + h.toLocaleString() + ' · Peers: ' + p + ' active · Mempool: ' + mp + ' txs';
    document.getElementById('minerZeroPeerWarn').style.display = p === 0 ? 'block' : 'none';

    if (currentAddress) {
      const bRes = await fetch('/api/balance?address=' + encodeURIComponent(currentAddress));
      if (bRes.ok) {
        const bal = await bRes.json();
        const spendable = Number(bal.spendable_balance !== undefined ? bal.spendable_balance : bal.balance || 0);
        const confirmed = Number(bal.confirmed_balance !== undefined ? bal.confirmed_balance : bal.balance || 0);
        const immature = Number(bal.immature_balance || 0);
        const pending = Number(bal.pending_balance !== undefined ? bal.pending_balance : confirmed);
        currentSpendableArmoids = spendable;

        document.getElementById('lblHeroBalance').innerText = formatNum(spendable);
        if (immature > 0) {
          document.getElementById('lblHeroSub').innerText = 'Confirmed ' + formatNum(confirmed) + ' · Immature ' + formatNum(immature) + ' (<' + currentMaturity + ' blks) · Spendable ' + formatNum(spendable);
        } else {
          document.getElementById('lblHeroSub').innerText = 'Confirmed ' + formatNum(confirmed) + ' · 100% settled · Available ' + formatNum(pending);
        }
        document.getElementById('lblSendAvailableBadge').innerText = 'Available: ' + formatArmx(spendable);
      }
    }

    // Auto incremental delta sync when tip height or mempool count changes (ZERO manual Refresh needed)
    if (currentAddress && (h !== lastSyncedHeight || mp !== lastSyncedMempool)) {
      syncHistoryFromDB(false);
    }
  } catch (e) {
    await showNodeStatusFallback('Daemon unreachable: ' + e.message);
  }
}

async function syncHistoryFromDB(forceFull) {
  if (!currentAddress || historySyncInFlight) return;
  historySyncInFlight = true;
  const metaEl = document.getElementById('lblTxSyncMeta');
  if (forceFull && metaEl) metaEl.innerText = 'Performing full blockchain resync into local database...';
  try {
    const url = '/api/history?address=' + encodeURIComponent(currentAddress) + '&envelope=1' + (forceFull ? '&force=1' : '');
    const res = await fetch(url);
    if (!res.ok) return;
    const env = await res.json();
    currentTxRecords = env.records || [];
    if (env.maturity > 0) currentMaturity = env.maturity;
    lastSyncedHeight = typeof env.chain_height === 'number' ? env.chain_height : lastSyncedHeight;
    lastSyncedMempool = currentTxRecords.filter(r => r.block_index < 0).length;

    if (metaEl) {
      let status = 'Local DB synced to #' + (env.synced_height >= 0 ? env.synced_height : 0);
      if (env.reorg_reset) status += ' (reorg healed)';
      else if (env.cache_hit) status += ' (instant local cache)';
      else if (env.delta_blocks > 0) status += ' (+' + env.delta_blocks + ' delta blocks)';
      if (env.warning) status += ' · offline cache fallback';
      metaEl.innerText = status;
    }

    renderOverviewRecentTx();
    applyTxFilter();
  } finally {
    historySyncInFlight = false;
  }
}

function classifyRecord(rec) {
  const tx = rec.tx || {};
  if (tx.sender === 'SYSTEM' || tx.sender === 'COINBASE') return 'Mined (Coinbase)';
  if (currentAddress && String(tx.sender || '').toLowerCase() === currentAddress.toLowerCase()) return 'Sent';
  return 'Received';
}

function formatHeightBadge(rec, kind) {
  if (rec.block_index < 0) return '<span class="status-pill warn">Pending</span>';
  const tip = lastSyncedHeight >= rec.block_index ? lastSyncedHeight : rec.block_index;
  const confs = tip - rec.block_index + 1;
  if (kind === 'Mined (Coinbase)') {
    return confs < currentMaturity
      ? '#' + rec.block_index + ' (' + confs + '/' + currentMaturity + ' blks)'
      : '#' + rec.block_index + ' (Mature)';
  }
  return confs < 6 ? '#' + rec.block_index + ' (' + confs + '/6 confs)' : '#' + rec.block_index + ' (Finalized)';
}

function renderOverviewRecentTx() {
  const el = document.getElementById('recentTxContainer');
  if (!el) return;
  if (currentTxRecords.length === 0) {
    el.innerHTML = '<div style="color:var(--text-dim);">No transactions yet</div>';
    return;
  }
  const top = currentTxRecords.slice(0, 5);
  el.innerHTML = '<div class="table-wrap"><table><tbody>' + top.map(r => {
    const kind = classifyRecord(r);
    const tx = r.tx || {};
    const dt = new Date((r.timestamp || 0) * 1000).toLocaleString();
    return '<tr class="clickable" onclick="showTxDetailsByHash(\'' + escapeHTML(tx.hash || '') + '\')">' +
      '<td><b>' + escapeHTML(kind) + '</b></td>' +
      '<td class="mono">' + escapeHTML(formatArmx(tx.amount || 0)) + '</td>' +
      '<td>' + formatHeightBadge(r, kind) + '</td>' +
      '<td style="color:var(--text-dim); text-align:right;">' + escapeHTML(dt) + '</td></tr>';
  }).join('') + '</tbody></table></div>';
}

function applyTxFilter() {
  const q = (document.getElementById('txtTxSearch')?.value || '').trim().toLowerCase();
  const tFilter = document.getElementById('selTxType')?.value || 'All';
  const dFrom = document.getElementById('txtTxDateFrom')?.value || '';
  const dTo = document.getElementById('txtTxDateTo')?.value || '';

  filteredTxRecords = currentTxRecords.filter(r => {
    const kind = classifyRecord(r);
    if (tFilter !== 'All') {
      if (tFilter === 'Pending') { if (r.block_index >= 0) return false; }
      else if (kind !== tFilter) return false;
    }
    const isoDay = new Date((r.timestamp || 0) * 1000).toISOString().slice(0, 10);
    if (dFrom && isoDay < dFrom) return false;
    if (dTo && isoDay > dTo) return false;
    if (q) {
      const tx = r.tx || {};
      const hay = ((tx.hash || '') + ' ' + (tx.sender || '') + ' ' + (tx.recipient || '') + ' ' + kind).toLowerCase();
      if (hay.indexOf(q) === -1) return false;
    }
    return true;
  });
  renderTransactionsPage(currentTxPage);
}

function clearTxFilters() {
  document.getElementById('txtTxSearch').value = '';
  document.getElementById('selTxType').value = 'All';
  document.getElementById('txtTxDateFrom').value = '';
  document.getElementById('txtTxDateTo').value = '';
  applyTxFilter();
}

function setTxPage(p) { renderTransactionsPage(p); }

function renderTransactionsPage(page) {
  const pageSize = 25;
  const total = filteredTxRecords.length;
  const pages = Math.max(1, Math.ceil(total / pageSize));
  currentTxPage = Math.max(0, Math.min(page, pages - 1));
  const slice = filteredTxRecords.slice(currentTxPage * pageSize, (currentTxPage + 1) * pageSize);

  const tbody = document.getElementById('tbodyTransactions');
  if (slice.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" style="color:var(--text-dim);">No transactions match your filter.</td></tr>';
  } else {
    tbody.innerHTML = slice.map(r => {
      const kind = classifyRecord(r);
      const tx = r.tx || {};
      const dt = new Date((r.timestamp || 0) * 1000).toLocaleString();
      const shortID = (tx.hash || '').slice(0, 18) + '...';
      return '<tr class="clickable" onclick="showTxDetailsByHash(\'' + escapeHTML(tx.hash || '') + '\')">' +
        '<td><b>' + escapeHTML(kind) + '</b></td>' +
        '<td class="mono">' + escapeHTML(formatArmx(tx.amount || 0)) + '</td>' +
        '<td>' + formatHeightBadge(r, kind) + '</td>' +
        '<td class="mono">' + escapeHTML(shortID) + '</td>' +
        '<td>' + escapeHTML(dt) + '</td></tr>';
    }).join('');
  }
  document.getElementById('lblTxPageInfo').innerText = 'Page ' + (currentTxPage + 1) + ' / ' + pages + ' (' + total + ' transactions)';
}

function showTxDetailsByHash(hash) {
  const rec = currentTxRecords.find(r => (r.tx?.hash || '') === hash);
  if (!rec) return;
  const tx = rec.tx || {};
  document.getElementById('txDetailsBody').innerHTML =
    '<div class="kv-row"><span class="kv-key">TXID</span><span class="kv-val mono">' + escapeHTML(tx.hash || '') + '</span></div>' +
    '<div class="kv-row"><span class="kv-key">Sender</span><span class="kv-val mono">' + escapeHTML(tx.sender || '') + '</span></div>' +
    '<div class="kv-row"><span class="kv-key">Recipient</span><span class="kv-val mono">' + escapeHTML(tx.recipient || '') + '</span></div>' +
    '<div class="kv-row"><span class="kv-key">Amount</span><span class="kv-val mono">' + escapeHTML(formatArmx(tx.amount || 0)) + '</span></div>' +
    '<div class="kv-row"><span class="kv-key">Fee</span><span class="kv-val mono">' + escapeHTML(formatArmx(tx.fee || 0)) + '</span></div>' +
    '<div class="kv-row"><span class="kv-key">Height</span><span class="kv-val">' + (rec.block_index >= 0 ? '#' + rec.block_index : 'Pending') + '</span></div>';
  openModal('txDetailsModal');
}

function exportCSV() {
  window.location.href = '/api/export-csv?address=' + encodeURIComponent(currentAddress) + '&token=' + encodeURIComponent(GUI_SESSION_TOKEN);
}

async function loadFeeEstimate() {
  const res = await fetch('/api/fee-estimate');
  if (!res.ok) return;
  currentFeeEstimate = await res.json();
  document.getElementById('lblFeeRuleNote').innerText =
    formatNum(currentFeeEstimate.min_relay_fee_per_kb) + '/KB × ~' + Math.round((currentFeeEstimate.estimated_tx_bytes || 10800) / 1000) + ' KB ML-DSA-65 tx (Min: ' + currentFeeEstimate.min_fee_formatted + ')';
  document.getElementById('pillFeeEconomy').innerText = 'Economy (Min): ' + currentFeeEstimate.economy_formatted;
  document.getElementById('pillFeeStandard').innerText = 'Standard: ' + currentFeeEstimate.standard_formatted;
  document.getElementById('pillFeePriority').innerText = 'Priority: ' + currentFeeEstimate.priority_formatted;

  const feeInput = document.getElementById('txtSendFee');
  if (!feeInput.dataset.userEdited) {
    feeInput.value = currentFeeEstimate.economy_formatted;
  }
  updateSendPreview();
}

function selectFeeTier(tier, btn) {
  document.querySelectorAll('.fee-pill').forEach(p => p.classList.remove('active'));
  if (btn) btn.classList.add('active');
  const val = tier === 'priority' ? currentFeeEstimate.priority_formatted : (tier === 'standard' ? currentFeeEstimate.standard_formatted : currentFeeEstimate.economy_formatted);
  const feeInput = document.getElementById('txtSendFee');
  feeInput.value = val;
  feeInput.dataset.userEdited = '1';
  updateSendPreview();
}

function setSendMax() {
  const feeArmx = parseFloat(document.getElementById('txtSendFee').value || '0') || (currentFeeEstimate.min_fee_armoids / 1e8);
  const feeArmoids = Math.round(feeArmx * 1e8);
  if (currentSpendableArmoids > feeArmoids) {
    document.getElementById('txtSendAmount').value = ((currentSpendableArmoids - feeArmoids) / 1e8).toFixed(8);
    updateSendPreview();
  }
}

function updateSendPreview() {
  const amt = parseFloat(document.getElementById('txtSendAmount').value || '0') || 0;
  const fee = parseFloat(document.getElementById('txtSendFee').value || '0') || 0;
  document.getElementById('lblSendTotal').innerText = (amt + fee).toFixed(8) + ' ' + currentTicker;
  const warnEl = document.getElementById('lblFeeWarning');
  const minArmx = (currentFeeEstimate.min_fee_armoids || 11000) / 1e8;
  if (fee > 0 && fee + 1e-9 < minArmx) {
    warnEl.innerText = 'Fee below required minimum relay fee (' + currentFeeEstimate.min_fee_formatted + ' ' + currentTicker + '). Node will reject.';
    warnEl.style.display = 'block';
  } else {
    warnEl.style.display = 'none';
  }
}

async function submitSendTransaction() {
  const to = document.getElementById('txtSendTo').value.trim();
  const amount = document.getElementById('txtSendAmount').value.trim();
  const fee = document.getElementById('txtSendFee').value.trim();
  const passphrase = document.getElementById('txtSendPass').value;
  const box = document.getElementById('sendResultBox');
  const btn = document.getElementById('btnSendTx');
  btn.disabled = true;
  try {
    const res = await fetch('/api/send', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ to, amount, fee, passphrase })
    });
    const text = await res.text();
    if (!res.ok) {
      box.innerText = 'Rejected (HTTP ' + res.status + '):\n' + text;
      return;
    }
    const out = JSON.parse(text);
    box.innerText = 'Transaction Broadcasted!\nTXID: ' + out.txid + '\nNonce: #' + out.nonce + '\nFee: ' + formatArmx(out.fee);
    document.getElementById('txtSendTo').value = '';
    document.getElementById('txtSendAmount').value = '';
    document.getElementById('txtSendPass').value = '';
    await syncHistoryFromDB(false);
    await pollStatsAndBalance();
  } finally {
    btn.disabled = false;
  }
}

function downloadActiveBackup() {
  window.location.href = '/api/wallets/backup?id=' + encodeURIComponent(currentWalletID) + '&token=' + encodeURIComponent(GUI_SESSION_TOKEN);
}

function syncMinerAddrIfChecked() {
  if (document.getElementById('chkMinerFollowActive').checked) {
    document.getElementById('txtMinerAddress').value = currentAddress;
  }
}

async function toggleMiningFromOverview() {
  const st = (document.getElementById('lblOverviewMinerState').innerText || '').trim();
  if (st && st !== 'Idle') {
    await stopMining();
  } else {
    await startMining();
  }
}

let currentMinerNodeSynced = true;
let minerThreadsInitialized = false;

async function startMining() {
  const errBanner = document.getElementById('minerStartError');
  if (errBanner) {
    errBanner.style.display = 'none';
    errBanner.innerText = '';
  }
  const miner = (document.getElementById('txtMinerAddress').value || currentAddress).trim();
  const threads = parseInt(document.getElementById('txtMinerThreads').value || '0', 10);
  const delay = parseInt(document.getElementById('txtMinerDelay').value || '1000', 10);
  try {
    const res = await fetch('/api/mine/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ miner, threads, delay })
    });
    let data = {};
    try { data = await res.json(); } catch (_) {}
    if (!res.ok || data.status === 'error') {
      if (errBanner) {
        errBanner.innerText = (data && data.message) || ('Failed to start mining (HTTP ' + res.status + ')');
        errBanner.style.display = 'block';
      }
      return;
    }
    pollMinerStatus();
  } catch (err) {
    if (errBanner) {
      errBanner.innerText = 'Failed to start mining: ' + err.message;
      errBanner.style.display = 'block';
    }
  }
}

async function stopMining() {
  await fetch('/api/mine/stop', { method: 'POST' });
  pollMinerStatus();
}

async function pollMinerStatus() {
  const res = await fetch('/api/mine/status');
  if (!res.ok) return;
  const m = await res.json();
  if (!minerThreadsInitialized && m.optimal_threads > 0) {
    const thInput = document.getElementById('txtMinerThreads');
    if (thInput) thInput.value = String(m.optimal_threads);
    minerThreadsInitialized = true;
  }
  const nodeSynced = m.node_synced !== false;
  currentMinerNodeSynced = nodeSynced;
  const waitingSync = !!m.waiting_for_sync;

  const syncBanner = document.getElementById('minerSyncBanner');
  if (syncBanner) {
    if (!nodeSynced) {
      const lh = Number(m.local_height || 0);
      const bh = Number(m.best_peer_height || 0);
      const pct = typeof m.sync_progress === 'number' ? m.sync_progress.toFixed(1) : '0.0';
      const detail = m.sync_reason || ('Synchronizing blockchain (#' + lh.toLocaleString() + ' / #' + bh.toLocaleString() + ' · ' + pct + '%)');
      syncBanner.innerText = detail + (waitingSync ? ' — Queued: Waiting for Sync.' : ' — Mining will begin automatically once sync completes.');
      syncBanner.style.display = 'block';
    } else {
      syncBanner.style.display = 'none';
    }
  }

  const hpsStr = (m.active && !waitingSync) ? formatHashrate(m.hashrate || 0) : '— H/s';
  const stateLabel = m.status_label || (m.active ? (waitingSync ? 'Waiting for Sync' : 'CPU Mining') : 'Idle');
  document.getElementById('lblOverviewMinerState').innerText = stateLabel;
  document.getElementById('lblOverviewHashrate').innerText = 'Hashrate: ' + hpsStr;
  document.getElementById('btnOverviewMineToggle').innerText = m.active ? 'Stop mining' : (nodeSynced ? 'Start mining' : 'Waiting for Sync');

  const badgeEl = document.getElementById('lblMinerBadge');
  if (m.active && waitingSync) {
    badgeEl.innerText = 'WAITING FOR SYNC';
    badgeEl.className = 'status-pill warn';
  } else if (m.active) {
    badgeEl.innerText = 'ACTIVE';
    badgeEl.className = 'status-pill unlocked';
  } else {
    badgeEl.innerText = 'IDLE';
    badgeEl.className = 'status-pill';
  }

  const startBtn = document.getElementById('btnStartMining');
  startBtn.disabled = !!m.active;
  startBtn.innerText = (m.active ? (waitingSync ? 'Waiting for Sync' : 'Start Mining') : (nodeSynced ? 'Start Mining' : 'Waiting for Sync'));
  document.getElementById('btnStopMining').disabled = !m.active;
  document.getElementById('lblMinerHps').innerText = hpsStr;
  const s = m.elapsed_secs || 0;
  document.getElementById('lblMinerElapsed').innerText =
    String(Math.floor(s/3600)).padStart(2,'0') + ':' + String(Math.floor((s%3600)/60)).padStart(2,'0') + ':' + String(s%60).padStart(2,'0');
  document.getElementById('lblSessionBlocks').innerText = m.session_blocks || 0;
  document.getElementById('lblSessionRewards').innerText = formatArmx(m.session_reward || 0);
  document.getElementById('lblOrphanedBlocks').innerText = m.orphaned_blocks || 0;

  const blks = m.mined_blocks || [];
  const tb = document.getElementById('tbodyMinedBlocks');
  if (blks.length === 0) {
    tb.innerHTML = '<tr><td colspan="6" style="color:var(--text-dim);">No blocks mined in this session yet.</td></tr>';
  } else {
    tb.innerHTML = blks.map(b => {
      const pill = b.status === 'ORPHANED' ? 'status-pill danger' : (b.status === 'MATURE' ? 'status-pill unlocked' : 'status-pill warn');
      return '<tr><td>#' + b.height + '</td><td class="mono">' + escapeHTML(String(b.hash || '').slice(0, 16)) + '...</td>' +
        '<td class="mono">' + escapeHTML(formatArmx(b.reward || 0)) + '</td><td>' + (b.peers_at_mine || 0) + '</td>' +
        '<td>' + (b.confirmations || 0) + '/' + (b.maturity || currentMaturity) + '</td><td><span class="' + pill + '">' + escapeHTML(b.status) + '</span></td></tr>';
    }).join('');
  }
  if (m.logs && m.logs.length > 0) {
    const logEl = document.getElementById('minerLogOutput');
    logEl.innerText = m.logs.join('\n');
    logEl.scrollTop = logEl.scrollHeight;
  }
}

async function loadContacts() {
  const res = await fetch('/api/addressbook');
  if (!res.ok) return;
  contactsList = (await res.json()) || [];
  const tb = document.getElementById('tbodyContacts');
  if (contactsList.length === 0) {
    tb.innerHTML = '<tr><td colspan="3" style="color:var(--text-dim);">No contacts saved yet.</td></tr>';
    return;
  }
  tb.innerHTML = contactsList.map((c, idx) =>
    '<tr><td><b>' + escapeHTML(c.name) + '</b></td><td class="mono">' + escapeHTML(c.address) + '</td>' +
    '<td style="display:flex;gap:6px;"><button class="btn btn-sm" onclick="useContactForSend(\'' + escapeHTML(c.address) + '\')">Send</button>' +
    '<button class="btn btn-danger btn-sm" onclick="deleteContact(' + idx + ')">Delete</button></td></tr>'
  ).join('');
}

async function addContact() {
  const name = document.getElementById('txtContactName').value.trim();
  const addr = document.getElementById('txtContactAddr').value.trim();
  if (!name || !addr) return;
  contactsList.push({ name, address: addr, date_added: new Date().toISOString().slice(0, 10) });
  await fetch('/api/addressbook', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(contactsList) });
  document.getElementById('txtContactName').value = '';
  document.getElementById('txtContactAddr').value = '';
  loadContacts();
}

async function deleteContact(idx) {
  contactsList.splice(idx, 1);
  await fetch('/api/addressbook', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(contactsList) });
  loadContacts();
}

function useContactForSend(addr) {
  document.getElementById('txtSendTo').value = addr;
  switchTab('send');
}

function toggleConsoleExpand() {
  const el = document.getElementById('txtCLIOutput');
  const btn = document.getElementById('btnToggleConsoleSize');
  el.classList.toggle('expanded');
  btn.innerText = el.classList.contains('expanded') ? 'Compact Log (54vh)' : 'Expand Log (78vh)';
}

function executeConsoleInput() {
  const raw = document.getElementById('txtCLICommand').value.trim();
  if (!raw) return;
  const parts = raw.split(/\s+/);
  runCLI(parts[0].toLowerCase(), parts.slice(1).join(' '));
}

async function runCLI(cmd, arg) {
  const out = document.getElementById('txtCLIOutput');
  if (cmd === 'clear') { out.innerText = ''; return; }
  if (cmd === 'help') {
    out.innerText = 'Supported commands:\n  help\n  getnetworkinfo\n  getblockchaininfo\n  getpeerinfo\n  getmempoolinfo\n  getbalance [address]\n  gethistory [address]\n  clear';
    return;
  }
  const res = await fetch('/api/cli?command=' + encodeURIComponent(cmd) + (arg ? '&arg=' + encodeURIComponent(arg) : ''));
  const txt = await res.text();
  try {
    out.innerText = '$ ' + cmd + (arg ? ' ' + arg : '') + '\n' + JSON.stringify(JSON.parse(txt), null, 2);
  } catch (_) {
    out.innerText = '$ ' + cmd + '\n' + txt;
  }
}

async function fetchRawEndpoint(path) {
  const out = document.getElementById('txtCLIOutput');
  const res = await fetch(path);
  const txt = await res.text();
  try {
    out.innerText = '$ GET ' + path + '\n' + JSON.stringify(JSON.parse(txt), null, 2);
  } catch (_) {
    out.innerText = txt;
  }
}

async function loadPeerInspector() {
  const res = await fetch('/api/peers?filter=active');
  if (!res.ok) return;
  const data = await res.json();
  const tb = document.getElementById('tbodyPeerInspector');
  if (!Array.isArray(data) || data.length === 0) {
    tb.innerHTML = '<tr><td colspan="3" style="color:var(--text-dim);">No active public peers or DNS seeds connected.</td></tr>';
    return;
  }
  tb.innerHTML = data.map(item => {
    const ep = typeof item === 'string' ? item : (item.display_address || item.endpoint || item.addr || '');
    const role = (typeof item === 'object' && item.is_seed) || ep.indexOf('seed') !== -1 ? 'DNS Seed' : 'Public Node';
    return '<tr><td class="mono">' + escapeHTML(ep) + '</td><td>' + role + '</td><td><span class="status-pill unlocked">Active</span></td></tr>';
  }).join('');
}

if (document.readyState === 'loading') {
  window.addEventListener('DOMContentLoaded', bootstrapApplication);
} else {
  bootstrapApplication();
}
</script>
</body>
</html>`
