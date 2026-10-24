# Accessibility Statement for Armaxcoin

Armaxcoin is dedicated to ensuring digital and technical accessibility for all users, including people with disabilities, researchers, node operators, and users operating low-resource consumer hardware across the globe.

We strive to continually improve the user experience for everyone and apply relevant accessibility standards across our software and documentation.

---

## 1. Scope & Guidelines

This accessibility policy applies to:
- **Armax Desktop GUI Wallet (`armgui`)** built with the Fyne UI toolkit.
- **Command-Line Interface (CLI) Tools & Daemons** (`armcoind`, `armminer`, `armsend`, `armwallet`).
- **Official Documentation & Technical Specifications** in this repository.

Where applicable, we align our graphical interface and technical content with the **Web Content Accessibility Guidelines (WCAG) 2.1 Level AA** standards.

---

## 2. Desktop GUI Wallet Accessibility (`armgui`)

The Armaxcoin Desktop Wallet implements the following design principles:

- **High-Contrast Dark Theme:** The user interface uses a tailored dark theme with WCAG 2.1 AA compliant contrast ratios between text, interactive controls, and background panels, reducing eye strain and enhancing readability.
- **Keyboard Navigation:** All primary actions (tab navigation, buttons, text inputs, transaction fee selectors) can be navigated sequentially via keyboard (`Tab`, `Shift+Tab`, `Enter`, `Space`).
- **Redundant Feedback (Not Color-Alone):** Status indicators (such as sync progress, connection health, and transaction states) use explicit textual labels and distinct icons, rather than relying exclusively on color.
- **Screen Scaling & DPI Independence:** Built using vector and resolution-independent layout containers that respect operating system display scaling factors (e.g., 125%, 150%, 200% high-DPI scaling on Windows and Linux).
- **QR Code & Fallback Text:** All receive addresses displayed as QR codes are accompanied by the full 63-character Bech32 string and a single-click "Copy Address" button with immediate visual confirmation.
- **Form Validation & Error Messaging:** Missing inputs, invalid addresses, or insufficient funds produce clear, non-transient, descriptive warning dialogs with actionable guidance.

---

## 3. Terminal & CLI Daemon Accessibility

For users utilizing terminal screen readers (such as Orca on Linux or NVDA/JAWS with terminal emulators):

- **Plain-Text & Scriptable Output:** CLI tools output structured text without unrendered raw binary data, making output compatible with standard screen readers and Unix pipes (`grep`, `awk`, `jq`).
- **Comprehensive Command-Line Help:** Every tool supports `-help` with clear parameter descriptions, default values, and operational flags.
- **Configurable Logging:** Daemon output can be redirected, written to file, or adjusted via configuration options to minimize audio clutter for screen reader users.

---

## 4. Hardware & Resource Accessibility

True decentralization requires accessibility across economic and physical hardware boundaries:

- **Consumer CPU & SBC Optimization:** The consensus mining algorithm (ArmaxHash: Argon2id 2 MB) is specifically parameterized to run efficiently on commodity multicore CPUs and single-board computers (ARM64, Raspberry Pi 4/5, Orange Pi), prioritizing accessible multi-core hardware participation.
- **Lightweight Memory Footprint:** Node daemons and CLI tools maintain modest RAM footprints (under 500 MB under normal testnet operation), allowing operation on entry-level machines.

---

## 5. Documentation Accessibility

- **Semantic Markdown:** All project documentation adheres to strict semantic heading hierarchies (`#`, `##`, `###`), structured lists, and accessible markdown tables.
- **Plain Text Diagrams & Formats:** Complex consensus algorithms and architectural workflows are documented in textual code blocks and standard Mermaid diagrams rather than inaccessible flattened bitmap graphics.
- **Descriptive Hyperlinks:** Links use meaningful descriptive text rather than generic "click here" labels.

---

## 6. Feedback & Reporting Accessibility Barriers

We welcome feedback on the accessibility of Armaxcoin. If you encounter accessibility barriers, unexpected contrast issues, or screen-reader incompatibilities:

1. **Submit a Bug Report:** Open an issue on GitHub using our [Bug Report Template](https://github.com/acxxcxx/armax-coin/issues/new?template=bug_report.md) with the label `accessibility`.
2. **Private Feedback:** You may also contact repository maintainers via GitHub discussions or private vulnerability advisories if related to security or assistive tools.

We are committed to addressing reported accessibility issues in subsequent maintenance releases.
