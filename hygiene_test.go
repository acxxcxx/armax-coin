package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ============================================================================
// REPOSITORY HYGIENE & SECRET DETECTION TEST SUITE
// ============================================================================
//
// Git Coverage & Architectural Boundaries:
// - Tracked Files (Index / HEAD): Enumerated strictly via `git ls-files -z`
//   to avoid splitting bugs on whitespace or Unicode paths.
// - Working Tree vs. Git History:
//   IMPORTANT: In-tree Go tests (including this suite) inspect files present
//   in the checked-out tree. They DO NOT scan historical commits or orphaned
//   blobs in `.git/objects`. Complete historical DAG verification requires
//   a separate audit tool (e.g. `gitleaks detect --source=. --verbose`).
// ============================================================================

// RuleCategory classifies detected hygiene or secret violations.
type RuleCategory string

const (
	CatForbiddenFile RuleCategory = "FORBIDDEN_SENSITIVE_FILE"
	CatPersonalEmail RuleCategory = "PERSONAL_EMAIL"
	CatLocalPath     RuleCategory = "LOCAL_PATH"
	CatPrivateKey    RuleCategory = "PRIVATE_KEY"
	CatPlatformToken RuleCategory = "PLATFORM_TOKEN"
	CatCryptoSecret  RuleCategory = "CRYPTO_SECRET"
)

// HygieneViolation encapsulates a single detected repository issue.
type HygieneViolation struct {
	File        string
	Line        int
	Category    RuleCategory
	Description string
	RedactedVal string
}

func (v HygieneViolation) String() string {
	if v.Line > 0 {
		return fmt.Sprintf("%s:%d: [%s] %s: %s", v.File, v.Line, v.Category, v.Description, v.RedactedVal)
	}
	return fmt.Sprintf("%s: [%s] %s", v.File, v.Category, v.Description)
}

// redactSecret masks sensitive strings so test failure logs never publish cleartext credentials.
func redactSecret(val string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return "[EMPTY]"
	}
	n := len(val)
	if n <= 4 {
		return strings.Repeat("*", n)
	}
	if n <= 8 {
		return val[:2] + strings.Repeat("*", n-4) + val[n-2:]
	}
	// For longer credentials, mask the entire body while retaining prefix, suffix, and length
	return fmt.Sprintf("%s...%s (len %d)", val[:4], val[n-4:], n)
}

// isForbiddenFileName checks if a file path matches forbidden naming conventions for sensitive files.
func isForbiddenFileName(path string) (bool, string) {
	norm := filepath.ToSlash(path)
	base := strings.ToLower(filepath.Base(norm))

	// .env files (strictly allow only .env.example, .env.template)
	if base == ".env" || (strings.HasPrefix(base, ".env.") && base != ".env.example" && base != ".env.template") {
		return true, "Environment configuration file containing possible credentials (use .env.example instead)"
	}

	// Keystore / archive files
	ext := strings.ToLower(filepath.Ext(base))
	if ext == ".pfx" || ext == ".p12" || ext == ".pkcs12" || ext == ".jks" || ext == ".keystore" || ext == ".kdbx" {
		return true, "Encrypted keystore or certificate archive file"
	}

	// SSH private or personal identity keys
	if base == "id_rsa" || base == "id_ed25519" || base == "id_ecdsa" || base == "id_dsa" ||
		base == "id_rsa.pub" || base == "id_ed25519.pub" || base == "id_ecdsa.pub" || base == "id_dsa.pub" {
		return true, "SSH private or personal identity key file"
	}

	// Runtime daemon RPC authentication cookies
	if base == "armax.cookie" || ext == ".cookie" {
		return true, "Runtime daemon RPC authentication cookie"
	}

	// Runtime peer discovery cache
	if base == "peers.json" {
		return true, "Runtime peer discovery cache database"
	}

	// Wallet databases
	if base == "wallet.json" {
		return true, "Unencrypted or encrypted wallet credentials database"
	}

	// Blockchain databases and backup files
	if ext == ".db" || strings.HasPrefix(ext, ".db.") || strings.Contains(base, "blockchain.db") {
		return true, "Blockchain database or backup artifact"
	}

	return false, ""
}

// contentRules defines regex patterns evaluated against text file lines.
var (
	// Personal email addresses across major personal webmail and privacy providers
	emailRegex = regexp.MustCompile(`(?i)[a-zA-Z0-9_.+-]+@(gmail\.com|googlemail\.com|yahoo\.[a-z.]+|outlook\.com|hotmail\.com|live\.com|msn\.com|icloud\.com|me\.com|mac\.com|proton\.me|protonmail\.com|pm\.me|zoho\.com|yandex\.[a-z.]+|gmx\.[a-z.]+|tutanota\.com|tuta\.io|mail\.com)`)

	// Local filesystem paths
	localPathWindowsRegex  = regexp.MustCompile(`(?i)[a-z]:[/\\](?:users|documents and settings)[/\\][a-zA-Z0-9_\.\-]+`)
	localPathUnixHomeRegex = regexp.MustCompile(`(?i)/(?:home|root)/([a-zA-Z0-9_\.\-]+)`)
	localPathMacHomeRegex  = regexp.MustCompile(`(?i)/Users/([a-zA-Z0-9_\.\-]+)`)

	// Cryptographic & Secret patterns
	pgpPrivateKeyRegex = regexp.MustCompile(`-----BEGIN PGP PRIVATE KEY BLOCK-----`)
	pemPrivateKeyRegex = regexp.MustCompile(`-----BEGIN (?:RSA|DSA|EC|OPENSSH|ENCRYPTED)? ?PRIVATE KEY-----`)
	puttyKeyRegex      = regexp.MustCompile(`PuTTY-User-Key-File-[23]:`)

	// Cloud & Platform tokens
	githubTokenRegex  = regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b|\bgithub_pat_[A-Za-z0-9_]{82}\b`)
	awsAccessKeyRegex = regexp.MustCompile(`\b(?:AKIA|ABIA|ACCA|ASIA)[0-9A-Z]{16}\b`)
	awsSecretKeyRegex = regexp.MustCompile(`(?i)aws_secret_access_key\s*(?::=|[:=])\s*["']?([A-Za-z0-9/+=]{40})["']?`)

	// Raw Hex Private Key Assignment (64-character hexadecimal keys)
	hexPrivateKeyAssignmentRegex = regexp.MustCompile(`(?i)(?:private[_-]?key|privkey|secret[_-]?seed|secret[_-]?key)\s*(?::=|[:=])\s*["']([0-9a-fA-F]{64})["']`)
)

// isDummyOrAllowedSecret filters known non-sensitive documentation placeholders and test dummies.
func isDummyOrAllowedSecret(val string) bool {
	lower := strings.ToLower(strings.TrimSpace(val))
	switch lower {
	case "0000000000000000000000000000000000000000000000000000000000000000",
		"1111111111111111111111111111111111111111111111111111111111111111",
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"dummy", "password", "testpassword", "your_secret_key", "your_api_key":
		return true
	}
	return false
}

// scanFileForHygiene inspects a single file path and its contents against all hygiene rules.
func scanFileForHygiene(relPath string, content []byte) ([]HygieneViolation, error) {
	var violations []HygieneViolation

	// 1. File Path / Naming Rules
	if forbidden, desc := isForbiddenFileName(relPath); forbidden {
		violations = append(violations, HygieneViolation{
			File:        relPath,
			Category:    CatForbiddenFile,
			Description: desc,
			RedactedVal: relPath,
		})
	}

	// Skip binary content: check file extension or null byte in first 1024 bytes
	binaryExts := map[string]bool{
		".syso": true,
		".ico":  true,
		".exe":  true,
		".zip":  true,
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".cer":  true,
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	if binaryExts[ext] {
		return violations, nil
	}

	headerLen := len(content)
	if headerLen > 1024 {
		headerLen = 1024
	}
	if bytes.IndexByte(content[:headerLen], 0) != -1 {
		return violations, nil
	}

	// 2. Line-by-Line Content Rules
	lines := strings.Split(string(content), "\n")
	for lineIdx, line := range lines {
		lineNum := lineIdx + 1

		// PEM / PGP / SSH Private Keys
		if pgpPrivateKeyRegex.MatchString(line) {
			violations = append(violations, HygieneViolation{
				File:        relPath,
				Line:        lineNum,
				Category:    CatPrivateKey,
				Description: "PGP Private Key block detected",
				RedactedVal: "[REDACTED_PGP_PRIVATE_KEY_BLOCK]",
			})
		} else if pemPrivateKeyRegex.MatchString(line) {
			violations = append(violations, HygieneViolation{
				File:        relPath,
				Line:        lineNum,
				Category:    CatPrivateKey,
				Description: "PEM Private Key header detected",
				RedactedVal: "[REDACTED_PEM_PRIVATE_KEY_HEADER]",
			})
		}
		if puttyKeyRegex.MatchString(line) {
			violations = append(violations, HygieneViolation{
				File:        relPath,
				Line:        lineNum,
				Category:    CatPrivateKey,
				Description: "PuTTY Private Key header detected",
				RedactedVal: "[REDACTED_PUTTY_PRIVATE_KEY_HEADER]",
			})
		}

		// Cloud & Platform Tokens
		if m := githubTokenRegex.FindString(line); m != "" {
			violations = append(violations, HygieneViolation{
				File:        relPath,
				Line:        lineNum,
				Category:    CatPlatformToken,
				Description: "GitHub Personal Access Token or credential detected",
				RedactedVal: redactSecret(m),
			})
		}
		if m := awsAccessKeyRegex.FindString(line); m != "" {
			violations = append(violations, HygieneViolation{
				File:        relPath,
				Line:        lineNum,
				Category:    CatPlatformToken,
				Description: "AWS Access Key ID detected",
				RedactedVal: redactSecret(m),
			})
		}
		if sub := awsSecretKeyRegex.FindStringSubmatch(line); len(sub) > 1 {
			if !isDummyOrAllowedSecret(sub[1]) {
				violations = append(violations, HygieneViolation{
					File:        relPath,
					Line:        lineNum,
					Category:    CatPlatformToken,
					Description: "AWS Secret Access Key assignment detected",
					RedactedVal: redactSecret(sub[1]),
				})
			}
		}

		// Cryptographic Raw Private Keys
		if sub := hexPrivateKeyAssignmentRegex.FindStringSubmatch(line); len(sub) > 1 {
			if !isDummyOrAllowedSecret(sub[1]) {
				violations = append(violations, HygieneViolation{
					File:        relPath,
					Line:        lineNum,
					Category:    CatCryptoSecret,
					Description: "Hardcoded 256-bit hexadecimal private key or seed assignment detected",
					RedactedVal: redactSecret(sub[1]),
				})
			}
		}

		// Personal Email Addresses
		if m := emailRegex.FindString(line); m != "" {
			violations = append(violations, HygieneViolation{
				File:        relPath,
				Line:        lineNum,
				Category:    CatPersonalEmail,
				Description: "Forbidden personal email address detected",
				RedactedVal: redactSecret(m),
			})
		}

		// Windows Local User Paths
		if m := localPathWindowsRegex.FindString(line); m != "" {
			violations = append(violations, HygieneViolation{
				File:        relPath,
				Line:        lineNum,
				Category:    CatLocalPath,
				Description: "Local Windows user filesystem path detected",
				RedactedVal: redactSecret(m),
			})
		}

		// Unix / Linux Local Home Paths (excluding CI runner 'runner')
		if matches := localPathUnixHomeRegex.FindAllStringSubmatch(line, -1); len(matches) > 0 {
			for _, match := range matches {
				if len(match) > 1 {
					username := strings.ToLower(match[1])
					if username != "runner" {
						violations = append(violations, HygieneViolation{
							File:        relPath,
							Line:        lineNum,
							Category:    CatLocalPath,
							Description: "Local Unix user home path detected",
							RedactedVal: redactSecret(match[0]),
						})
					}
				}
			}
		}

		// macOS Local User Paths
		if matches := localPathMacHomeRegex.FindAllStringSubmatch(line, -1); len(matches) > 0 {
			for _, match := range matches {
				if len(match) > 1 {
					username := strings.ToLower(match[1])
					if username != "runner" && username != "shared" {
						violations = append(violations, HygieneViolation{
							File:        relPath,
							Line:        lineNum,
							Category:    CatLocalPath,
							Description: "Local macOS user home path detected",
							RedactedVal: redactSecret(match[0]),
						})
					}
				}
			}
		}
	}

	return violations, nil
}

// TestNoPersonalDataInTree scans all tracked Git files for personal data, local filesystem paths,
// and secret credentials using safe NUL-delimited enumeration [L4-10-b].
func TestNoPersonalDataInTree(t *testing.T) {
	// 1. Get all tracked files safely via git ls-files -z
	cmd := exec.Command("git", "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to execute git ls-files -z: %v", err)
	}

	rawEntries := bytes.Split(out, []byte{0})
	var trackedFiles []string
	for _, entry := range rawEntries {
		clean := strings.TrimSpace(string(entry))
		if clean != "" {
			trackedFiles = append(trackedFiles, clean)
		}
	}

	if len(trackedFiles) == 0 {
		t.Fatalf("git ls-files returned empty list")
	}

	var allViolations []HygieneViolation

	for _, relPath := range trackedFiles {
		// Skip hygiene_test.go itself so pattern literals and test vectors do not self-match
		if filepath.Base(relPath) == "hygiene_test.go" {
			continue
		}

		content, err := os.ReadFile(relPath)
		if err != nil {
			// Missing or unreadable tracked files represent a repository integrity failure
			t.Errorf("Tracked file unreadable or missing from filesystem: %s (%v)", relPath, err)
			continue
		}

		violations, err := scanFileForHygiene(relPath, content)
		if err != nil {
			t.Errorf("Failed to scan %s: %v", relPath, err)
			continue
		}
		allViolations = append(allViolations, violations...)
	}

	if len(allViolations) > 0 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("[L4-10-b] Repository hygiene and secret audit failed with %d violation(s):\n", len(allViolations)))
		for _, v := range allViolations {
			sb.WriteString(fmt.Sprintf("  - %s\n", v.String()))
		}
		t.Fatal(sb.String())
	}
}

// TestHygieneRules_DetectionAndRedaction validates that the hygiene scanner accurately
// detects forbidden files, PII, and cryptographic secrets, and strictly redacts sensitive output.
func TestHygieneRules_DetectionAndRedaction(t *testing.T) {
	tests := []struct {
		name              string
		path              string
		content           string
		expectViolation   bool
		expectedCategory  RuleCategory
		expectedRedaction string
	}{
		{
			name:              "Forbidden .env file",
			path:              ".env",
			content:           "PORT=8080\n",
			expectViolation:   true,
			expectedCategory:  CatForbiddenFile,
			expectedRedaction: ".env",
		},
		{
			name:              "Forbidden .env.local file",
			path:              ".env.local",
			content:           "PORT=8080\n",
			expectViolation:   true,
			expectedCategory:  CatForbiddenFile,
			expectedRedaction: ".env.local",
		},
		{
			name:              "Allowed .env.example file",
			path:              ".env.example",
			content:           "PORT=8080\n",
			expectViolation:   false,
		},
		{
			name:              "Forbidden wallet.json file",
			path:              "wallet.json",
			content:           "{}",
			expectViolation:   true,
			expectedCategory:  CatForbiddenFile,
		},
		{
			name:              "PEM RSA Private Key header",
			path:              "test.txt",
			content:           "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA...\n",
			expectViolation:   true,
			expectedCategory:  CatPrivateKey,
			expectedRedaction: "[REDACTED_PEM_PRIVATE_KEY_HEADER]",
		},
		{
			name:              "PGP Private Key header",
			path:              "test.txt",
			content:           "-----BEGIN PGP PRIVATE KEY BLOCK-----\nVersion: BCPG v1.68\n",
			expectViolation:   true,
			expectedCategory:  CatPrivateKey,
			expectedRedaction: "[REDACTED_PGP_PRIVATE_KEY_BLOCK]",
		},
		{
			name:              "PuTTY Key header",
			path:              "test.ppk",
			content:           "PuTTY-User-Key-File-2: ssh-rsa\nEncryption: none\n",
			expectViolation:   true,
			expectedCategory:  CatPrivateKey,
			expectedRedaction: "[REDACTED_PUTTY_PRIVATE_KEY_HEADER]",
		},
		{
			name:              "GitHub Personal Access Token (classic)",
			path:              "ci.sh",
			content:           "export GITHUB_TOKEN=ghp_123456789012345678901234567890123456\n",
			expectViolation:   true,
			expectedCategory:  CatPlatformToken,
			expectedRedaction: "ghp_...3456 (len 40)",
		},
		{
			name:              "AWS Access Key ID",
			path:              "aws.conf",
			content:           "aws_access_key_id = AKIAIOSFODNN7EXAMPLE\n",
			expectViolation:   true,
			expectedCategory:  CatPlatformToken,
			expectedRedaction: "AKIA...MPLE (len 20)",
		},
		{
			name:              "Hardcoded Hex Private Key Assignment",
			path:              "key.go",
			content:           `privkey := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"` + "\n",
			expectViolation:   true,
			expectedCategory:  CatCryptoSecret,
			expectedRedaction: "e3b0...b855 (len 64)",
		},
		{
			name:              "Allowed dummy zero key",
			path:              "key.go",
			content:           `privkey := "0000000000000000000000000000000000000000000000000000000000000000"` + "\n",
			expectViolation:   false,
		},
		{
			name:              "Personal Gmail email",
			path:              "contact.txt",
			content:           "Contact developer at john.doe.test@gmail.com for issues.\n",
			expectViolation:   true,
			expectedCategory:  CatPersonalEmail,
			expectedRedaction: "john....com (len 23)",
		},
		{
			name:              "Allowed RFC example email",
			path:              "docs.md",
			content:           "Send requests to alice@example.com for demonstrations.\n",
			expectViolation:   false,
		},
		{
			name:              "Windows personal directory path",
			path:              "config.txt",
			content:           `database_path = "C:\Users\JohnDoe\armax_coin\data"` + "\n",
			expectViolation:   true,
			expectedCategory:  CatLocalPath,
		},
		{
			name:              "Unix personal directory path",
			path:              "config.txt",
			content:           `datadir=/home/johndoe/armax_coin/data` + "\n",
			expectViolation:   true,
			expectedCategory:  CatLocalPath,
		},
		{
			name:              "Allowed CI runner path",
			path:              "ci.sh",
			content:           `export HOME=/home/runner/work/armax` + "\n",
			expectViolation:   false,
		},
		{
			name:              "macOS personal directory path",
			path:              "config.txt",
			content:           `datadir=/Users/johndoe/armax_coin/data` + "\n",
			expectViolation:   true,
			expectedCategory:  CatLocalPath,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := scanFileForHygiene(tc.path, []byte(tc.content))
			if err != nil {
				t.Fatalf("scanFileForHygiene error: %v", err)
			}

			if tc.expectViolation {
				if len(violations) == 0 {
					t.Fatalf("Expected violation for test '%s', but none was returned", tc.name)
				}
				v := violations[0]
				if tc.expectedCategory != "" && v.Category != tc.expectedCategory {
					t.Errorf("Expected category %s, got %s", tc.expectedCategory, v.Category)
				}
				if tc.expectedRedaction != "" && v.RedactedVal != tc.expectedRedaction {
					t.Errorf("Expected redaction '%s', got '%s'", tc.expectedRedaction, v.RedactedVal)
				}
				// Crucial safety check: ensure the cleartext secret payload is never printed in String()
				if strings.Contains(v.String(), "123456789012345678901234567890123456") {
					t.Errorf("Security check failed: unredacted secret found in violation output string: %s", v.String())
				}
			} else {
				if len(violations) > 0 {
					t.Fatalf("Expected no violations for test '%s', got: %v", tc.name, violations)
				}
			}
		})
	}
}
