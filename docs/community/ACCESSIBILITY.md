# Accessibility Statement for Armaxcoin

Armaxcoin is dedicated to ensuring digital and technical accessibility for all users, including people with disabilities, researchers, node operators, and users operating low-resource consumer hardware across the globe.

We strive to continually improve the user experience for everyone and apply relevant accessibility standards across our software and documentation.

---

## 1. Scope & Guidelines

This statement applies to:
- **Armax Desktop GUI (`armgui`)**, which uses platform-specific implementations.
- **Command-Line Interface (CLI) Tools & Daemons** (`armcoind`, `armminer`, `armsend`, `armwallet`).
- **Official Documentation & Technical Specifications** in this repository.

WCAG 2.1 Level AA is a reference for applicable interface work. The project has not completed a formal accessibility audit and does not claim WCAG conformance.

---

## 2. Desktop GUI Wallet Accessibility (`armgui`)

Accessibility support varies by platform and control. The following are goals for ongoing development, not verified guarantees:

- **Contrast and visual modes:** Contrast and theme behavior should be checked on each supported platform; no measured contrast-ratio report is currently published.
- **Keyboard and assistive technology:** Keyboard navigation and screen-reader behavior may vary by platform and have not been comprehensively tested.
- **Scaling and address display:** Display scaling, QR/address presentation, and form feedback should be verified with the target platform and release build.

---

## 3. Terminal & CLI Daemon Accessibility

CLI tools provide text output and command-line help. Compatibility with particular terminal screen readers and structured-output workflows has not been formally tested across all tools and platforms.

- **Plain-Text & Scriptable Output:** CLI tools output structured text without unrendered raw binary data, making output compatible with standard screen readers and Unix pipes (`grep`, `awk`, `jq`).
- **Command-Line Help:** Tools use Go's standard flag help where available; consult each tool's `-help` output for supported options.

---

## 4. Hardware & Resource Accessibility

True decentralization requires accessibility across economic and physical hardware boundaries:

- **Mining algorithms:** ArmaxHash v2 is active from testnet block 2500 and is not memory-hard. Argon2id is used by the historical v1 era (blocks 520..2499). Performance and resource use depend on hardware and workload; the project does not publish a general memory ceiling.

---

## 5. Documentation Accessibility

- **Semantic Markdown:** Documentation generally uses headings, lists, and tables; individual files may differ and are not covered by an accessibility conformance audit.
- **Plain Text Diagrams & Formats:** Complex consensus algorithms and architectural workflows are documented in textual code blocks and standard Mermaid diagrams rather than inaccessible flattened bitmap graphics.
- **Descriptive Hyperlinks:** Links use meaningful descriptive text rather than generic "click here" labels.

---

## 6. Feedback & Reporting Accessibility Barriers

We welcome feedback on the accessibility of Armaxcoin. If you encounter accessibility barriers, unexpected contrast issues, or screen-reader incompatibilities:

1. **Submit a Bug Report:** Open an issue on GitHub using our [Bug Report Template](https://github.com/acxxcxx/armax-coin/issues/new?template=bug_report.md) with the label `accessibility`.
2. **Private Feedback:** You may also contact repository maintainers via GitHub discussions or private vulnerability advisories if related to security or assistive tools.

We are committed to addressing reported accessibility issues in subsequent maintenance releases.
