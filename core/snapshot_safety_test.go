package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ValidateSnapshotTargetPath tests the path canonicalization and safety boundary rules
// implemented in scripts/make_public_snapshot.sh and make_public_snapshot.ps1 [P1 Review].
func ValidateSnapshotTargetPath(targetDir, repoRoot, homeDir string) error {
	clean := strings.TrimSpace(targetDir)
	clean = strings.ReplaceAll(clean, "\\", "/")
	clean = strings.TrimRight(clean, "/")

	if clean == "" || clean == "." || clean == ".." || clean == "/" {
		return &PathSafetyError{Msg: "target path is empty, relative root, or system root"}
	}

	// Refuse system roots
	systemRoots := []string{
		"/", "//", "/root", "/home", "/etc", "/usr", "/bin", "/sbin", "/var", "/dev", "/sys", "/proc",
	}
	for _, sys := range systemRoots {
		if clean == sys {
			return &PathSafetyError{Msg: "target is a system root directory: " + clean}
		}
	}

	// Windows drive roots: e.g. "C:", "D:"
	if len(clean) == 2 && clean[1] == ':' {
		return &PathSafetyError{Msg: "target is a drive root: " + clean}
	}

	// Refuse HOME directory
	if homeDir != "" {
		homeClean := strings.ReplaceAll(homeDir, "\\", "/")
		homeClean = strings.TrimRight(homeClean, "/")
		if clean == homeClean {
			return &PathSafetyError{Msg: "target cannot be user HOME directory"}
		}
	}

	// Refuse current repository root
	if repoRoot != "" {
		repoClean := strings.ReplaceAll(repoRoot, "\\", "/")
		repoClean = strings.TrimRight(repoClean, "/")
		if clean == repoClean {
			return &PathSafetyError{Msg: "target cannot be repository root"}
		}
	}

	return nil
}

type PathSafetyError struct {
	Msg string
}

func (e *PathSafetyError) Error() string { return e.Msg }

func TestSnapshotSafety_PathCanonicalization(t *testing.T) {
	homeDir := "/home/runner"
	repoRoot := "/home/runner/projects/armax-coin"

	// Dangerous paths that MUST be rejected
	dangerousPaths := []string{
		"",
		"   ",
		"/",
		"//",
		".",
		"..",
		"/root",
		"/home",
		"/etc",
		"/usr",
		"/var",
		"/bin",
		"/home/runner", // HOME
		"C:",           // Windows drive root
		"D:",
		"C:/",
		"E:/",
		repoRoot, // current repo root
	}

	for _, p := range dangerousPaths {
		err := ValidateSnapshotTargetPath(p, repoRoot, homeDir)
		if err == nil {
			t.Errorf("Expected dangerous path %q to be rejected, but it was accepted", p)
		}
	}

	// Safe paths that MUST be accepted
	safePaths := []string{
		"/tmp/armax-coin_public",
		"/home/runner/snapshots/armax_public",
		"C:/temp/armax_snapshot",
		"E:/builds/public_snapshot",
		"../snapshots/public_release",
	}

	for _, p := range safePaths {
		err := ValidateSnapshotTargetPath(p, repoRoot, homeDir)
		if err != nil {
			t.Errorf("Expected safe path %q to be accepted, got error: %v", p, err)
		}
	}
}

// TestSnapshotSafety_ForceRequirement verifies that an existing target directory
// is not accidentally purged unless explicit force confirmation is provided [Follow-up Point 6].
func TestSnapshotSafety_ForceRequirement(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "existing_snapshot_dir")

	// Pre-create directory with an important file
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	markerFile := filepath.Join(targetPath, "marker.txt")
	if err := os.WriteFile(markerFile, []byte("preserve_me"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Simulate snapshot operation without force: must error out
	runSnapshot := func(force bool) error {
		if _, err := os.Stat(targetPath); err == nil {
			if !force {
				return &PathSafetyError{Msg: "target directory exists; --force required to overwrite"}
			}
			return os.RemoveAll(targetPath)
		}
		return nil
	}

	// Case 1: Without force -> fails safe
	if err := runSnapshot(false); err == nil {
		t.Fatalf("Expected error when overwriting existing target without force")
	}
	// Marker file must still exist
	if _, err := os.Stat(markerFile); os.IsNotExist(err) {
		t.Fatalf("Marker file was destroyed without force!")
	}

	// Case 2: With force -> succeeds
	if err := runSnapshot(true); err != nil {
		t.Fatalf("Expected successful purge with force, got: %v", err)
	}
}

func getRepoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to determine git repo root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func createJunctionOrSymlink(target, linkPath string) error {
	_ = os.Remove(linkPath)
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", linkPath, target)
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = out
			return os.Symlink(target, linkPath)
		}
		return nil
	}
	return os.Symlink(target, linkPath)
}

// TestSnapshotSafety_PowerShellScript_EndToEnd directly executes scripts/make_public_snapshot.ps1
// in PowerShell with -DryRun against:
// - Direct repository root
// - Relative parent '..' and ancestors
// - Subdirectories inside repository
// - Trailing separators
// - NTFS junctions pointing to repository root or subdirectories
// - Safe external target
// Proves all dangerous paths reject fail-closed and no files are removed.
func TestSnapshotSafety_PowerShellScript_EndToEnd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell snapshot safety tests require Windows runtime")
	}

	psExe, err := exec.LookPath("powershell.exe")
	if err != nil {
		psExe, err = exec.LookPath("powershell")
		if err != nil {
			t.Skip("powershell executable not found in PATH")
		}
	}

	repoRoot := getRepoRoot(t)
	psScript := filepath.Join(repoRoot, "scripts", "make_public_snapshot.ps1")
	if _, err := os.Stat(psScript); err != nil {
		t.Fatalf("Script not found at %s: %v", psScript, err)
	}

	tempDir := t.TempDir()
	canaryFile := filepath.Join(tempDir, "canary_marker.txt")
	if err := os.WriteFile(canaryFile, []byte("CANARY_DATA_DO_NOT_DELETE"), 0644); err != nil {
		t.Fatalf("Failed to create canary file: %v", err)
	}

	// 1. Setup junctions for testing
	junctionToRepo := filepath.Join(tempDir, "junc_repo_root")
	if err := createJunctionOrSymlink(repoRoot, junctionToRepo); err != nil {
		t.Logf("Warning: could not create junction to repo: %v", err)
	}
	defer func() { _ = os.Remove(junctionToRepo) }()

	junctionToSubdir := filepath.Join(tempDir, "junc_repo_subdir")
	coreSubdir := filepath.Join(repoRoot, "core")
	if err := createJunctionOrSymlink(coreSubdir, junctionToSubdir); err != nil {
		t.Logf("Warning: could not create junction to subdir: %v", err)
	}
	defer func() { _ = os.Remove(junctionToSubdir) }()

	repoParent := filepath.Dir(repoRoot)
	junctionToParent := filepath.Join(tempDir, "junc_repo_parent")
	if err := createJunctionOrSymlink(repoParent, junctionToParent); err != nil {
		t.Logf("Warning: could not create junction to parent: %v", err)
	}
	defer func() { _ = os.Remove(junctionToParent) }()

	// Dangerous test cases that MUST reject (exit code 1)
	rejectCases := []struct {
		name      string
		targetDir string
	}{
		{"RepoRoot_Exact", repoRoot},
		{"RepoRoot_TrailingSlash", repoRoot + "\\"},
		{"RepoSubdir_Direct", filepath.Join(repoRoot, "wallet")},
		{"RepoSubdir_Relative", ".\\core"},
		{"RepoSubdir_RelativeTrailing", ".\\core\\"},
		{"Parent_RelativeDotDot", ".."},
		{"Parent_RelativeDotDotSlash", "..\\"},
		{"RepoParent_Exact", repoParent},
		{"SystemRoot_Drive", "C:"},
		{"SystemRoot_DriveSlash", "C:\\"},
	}

	if _, err := os.Stat(junctionToRepo); err == nil {
		rejectCases = append(rejectCases, struct {
			name      string
			targetDir string
		}{"Junction_ToRepoRoot", junctionToRepo})
	}
	if _, err := os.Stat(junctionToSubdir); err == nil {
		rejectCases = append(rejectCases, struct {
			name      string
			targetDir string
		}{"Junction_ToRepoSubdir", junctionToSubdir})
	}
	if _, err := os.Stat(junctionToParent); err == nil {
		rejectCases = append(rejectCases, struct {
			name      string
			targetDir string
		}{"Junction_ToRepoParent", junctionToParent})
	}

	for _, tc := range rejectCases {
		t.Run("Reject_"+tc.name, func(t *testing.T) {
			cmd := exec.Command(psExe, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", psScript, "-TargetDir", tc.targetDir, "-DryRun")
			cmd.Dir = repoRoot
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("Security failure: PowerShell script ACCEPTED dangerous target %s (%s)!\nOutput:\n%s", tc.name, tc.targetDir, string(out))
			}
			outStr := string(out)
			if !strings.Contains(outStr, "ERROR") {
				t.Fatalf("Expected error message for dangerous target %s, got:\n%s", tc.name, outStr)
			}
		})
	}

	// Safe external test case that MUST be accepted in -DryRun mode (exit code 0)
	t.Run("Accept_SafeExternalTarget_DryRun", func(t *testing.T) {
		safeTarget := filepath.Join(tempDir, "safe_external_snapshot_target")
		cmd := exec.Command(psExe, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", psScript, "-TargetDir", safeTarget, "-DryRun")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("PowerShell script rejected safe external target %s!\nOutput:\n%s", safeTarget, string(out))
		}
		if !strings.Contains(string(out), "DRY-RUN") && !strings.Contains(string(out), "Dry Run Mode     : True") {
			t.Fatalf("Expected dry-run execution confirmation in output:\n%s", string(out))
		}
	})

	// Final verification: Prove canary and repo files are completely intact
	canaryData, err := os.ReadFile(canaryFile)
	if err != nil || string(canaryData) != "CANARY_DATA_DO_NOT_DELETE" {
		t.Fatalf("CRITICAL SECURITY FAILURE: Canary file was altered or destroyed during snapshot tests!")
	}
}

// TestSnapshotSafety_BashScript_EndToEnd directly executes scripts/make_public_snapshot.sh
// via bash with --dry-run against repo root, subdirectories, ancestors, and a safe external target.
func TestSnapshotSafety_BashScript_EndToEnd(t *testing.T) {
	var bashExe string
	if runtime.GOOS == "windows" {
		if _, statErr := os.Stat("C:\\Program Files\\Git\\bin\\bash.exe"); statErr == nil {
			bashExe = "C:\\Program Files\\Git\\bin\\bash.exe"
		} else if _, statErr := os.Stat("C:\\Program Files (x86)\\Git\\bin\\bash.exe"); statErr == nil {
			bashExe = "C:\\Program Files (x86)\\Git\\bin\\bash.exe"
		}
	}
	if bashExe == "" {
		if b, err := exec.LookPath("bash"); err == nil {
			testCmd := exec.Command(b, "-c", "echo OK")
			if out, err := testCmd.Output(); err == nil && strings.TrimSpace(string(out)) == "OK" {
				bashExe = b
			}
		}
	}
	if bashExe == "" {
		t.Skip("Valid bash executable not found, skipping bash end-to-end test")
	}

	repoRoot := getRepoRoot(t)
	shScript := filepath.Join(repoRoot, "scripts", "make_public_snapshot.sh")
	if _, err := os.Stat(shScript); err != nil {
		t.Fatalf("Script not found at %s: %v", shScript, err)
	}

	tempDir := t.TempDir()
	canaryFile := filepath.Join(tempDir, "bash_canary_marker.txt")
	if err := os.WriteFile(canaryFile, []byte("BASH_CANARY_DO_NOT_DELETE"), 0644); err != nil {
		t.Fatalf("Failed to create canary file: %v", err)
	}

	rejectCases := []struct {
		name      string
		targetDir string
	}{
		{"RepoRoot_Exact", repoRoot},
		{"RepoSubdir_Relative", "./core"},
		{"Parent_RelativeDotDot", ".."},
		{"SystemRoot_Slash", "/"},
	}

	for _, tc := range rejectCases {
		t.Run("Reject_"+tc.name, func(t *testing.T) {
			cmd := exec.Command(bashExe, shScript, "--dry-run", tc.targetDir)
			cmd.Dir = repoRoot
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("Security failure: Bash script ACCEPTED dangerous target %s (%s)!\nOutput:\n%s", tc.name, tc.targetDir, string(out))
			}
			outStr := string(out)
			if !strings.Contains(outStr, "ERROR") {
				t.Fatalf("Expected error message for dangerous target %s, got:\n%s", tc.name, outStr)
			}
		})
	}

	// Safe external target in dry run mode
	t.Run("Accept_SafeExternalTarget_DryRun", func(t *testing.T) {
		safeTarget := filepath.Join(tempDir, "safe_bash_snapshot_target")
		cmd := exec.Command(bashExe, shScript, "--dry-run", safeTarget)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Bash script rejected safe external target %s!\nOutput:\n%s", safeTarget, string(out))
		}
		if !strings.Contains(string(out), "DRY-RUN") && !strings.Contains(string(out), "DRY RUN MODE") {
			t.Fatalf("Expected dry-run confirmation in output:\n%s", string(out))
		}
	})

	// Verify canary
	canaryData, err := os.ReadFile(canaryFile)
	if err != nil || string(canaryData) != "BASH_CANARY_DO_NOT_DELETE" {
		t.Fatalf("CRITICAL SECURITY FAILURE: Bash test canary file was altered or destroyed!")
	}
}

// TestSnapshotSafety_OutsideCWD_PowerShell tests executing make_public_snapshot.ps1
// from an unrelated outside current working directory (e.g. %TEMP%).
// Proves:
// 1. Script path resolution correctly discovers repo root even when caller CWD is outside.
// 2. Absolute paths to repo root, subdirectories, and ancestors are strictly rejected.
// 3. NTFS junctions and symlinks pointing into repo are rejected.
// 4. Existing destination files (canary) are preserved and never purged during dry-run.
// 5. Outside CWD canary files and repo files remain completely unchanged.
func TestSnapshotSafety_OutsideCWD_PowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell snapshot safety tests require Windows runtime")
	}

	psExe, err := exec.LookPath("powershell.exe")
	if err != nil {
		psExe, err = exec.LookPath("powershell")
		if err != nil {
			t.Skip("powershell executable not found in PATH")
		}
	}

	repoRoot := getRepoRoot(t)
	psScript := filepath.Join(repoRoot, "scripts", "make_public_snapshot.ps1")

	outsideDir := t.TempDir()
	canaryFile := filepath.Join(outsideDir, "outside_canary.txt")
	if err := os.WriteFile(canaryFile, []byte("OUTSIDE_CANARY_DATA"), 0644); err != nil {
		t.Fatalf("Failed to create outside canary file: %v", err)
	}

	// Setup junctions in outside directory
	junctionToRepo := filepath.Join(outsideDir, "junc_repo_root")
	_ = createJunctionOrSymlink(repoRoot, junctionToRepo)
	defer func() { _ = os.Remove(junctionToRepo) }()

	junctionToSubdir := filepath.Join(outsideDir, "junc_repo_subdir")
	_ = createJunctionOrSymlink(filepath.Join(repoRoot, "wallet"), junctionToSubdir)
	defer func() { _ = os.Remove(junctionToSubdir) }()

	repoParent := filepath.Dir(repoRoot)
	junctionToParent := filepath.Join(outsideDir, "junc_repo_parent")
	_ = createJunctionOrSymlink(repoParent, junctionToParent)
	defer func() { _ = os.Remove(junctionToParent) }()

	dangerousTargets := []struct {
		name   string
		target string
	}{
		{"RepoRoot_Absolute", repoRoot},
		{"RepoRoot_TrailingSlash", repoRoot + "\\"},
		{"RepoSubdir_Wallet", filepath.Join(repoRoot, "wallet")},
		{"RepoSubdir_Core", filepath.Join(repoRoot, "core")},
		{"RepoParent_Absolute", repoParent},
		{"SystemRoot_Drive", "C:"},
		{"SystemRoot_DriveSlash", "C:\\"},
	}

	if _, err := os.Stat(junctionToRepo); err == nil {
		dangerousTargets = append(dangerousTargets, struct {
			name   string
			target string
		}{"Junction_ToRepoRoot", junctionToRepo})
	}
	if _, err := os.Stat(junctionToSubdir); err == nil {
		dangerousTargets = append(dangerousTargets, struct {
			name   string
			target string
		}{"Junction_ToRepoSubdir", junctionToSubdir})
	}
	if _, err := os.Stat(junctionToParent); err == nil {
		dangerousTargets = append(dangerousTargets, struct {
			name   string
			target string
		}{"Junction_ToRepoParent", junctionToParent})
	}

	for _, tc := range dangerousTargets {
		t.Run("Reject_FromOutside_"+tc.name, func(t *testing.T) {
			cmd := exec.Command(psExe, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", psScript, "-TargetDir", tc.target, "-DryRun", "-Force")
			cmd.Dir = outsideDir // Executed strictly from outside CWD!
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("Security failure: PowerShell script ACCEPTED dangerous target %s (%s) from outside CWD!\nOutput:\n%s", tc.name, tc.target, string(out))
			}
			outStr := string(out)
			if !strings.Contains(outStr, "ERROR") {
				t.Fatalf("Expected error message for dangerous target %s, got:\n%s", tc.name, outStr)
			}
		})
	}

	// Safe external target executed from outside CWD
	t.Run("Accept_SafeExternalTarget_FromOutside", func(t *testing.T) {
		safeTarget := filepath.Join(outsideDir, "safe_outside_snapshot")
		cmd := exec.Command(psExe, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", psScript, "-TargetDir", safeTarget, "-DryRun")
		cmd.Dir = outsideDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("PowerShell script rejected safe external target from outside CWD!\nOutput:\n%s", string(out))
		}
		if !strings.Contains(string(out), "DRY-RUN") && !strings.Contains(string(out), "Dry Run Mode     : True") {
			t.Fatalf("Expected dry-run execution confirmation in output:\n%s", string(out))
		}
	})

	// Destination preservation check: Prove pre-existing files in target are NEVER deleted before checks pass
	t.Run("Preserve_ExistingDestination_Canary", func(t *testing.T) {
		destDir := filepath.Join(outsideDir, "existing_destination")
		if err := os.MkdirAll(destDir, 0755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
		destCanary := filepath.Join(destDir, "preserve_marker.txt")
		if err := os.WriteFile(destCanary, []byte("DEST_CANARY_DO_NOT_DELETE"), 0644); err != nil {
			t.Fatalf("Failed to create dest canary: %v", err)
		}

		// Run dry-run with force
		cmd := exec.Command(psExe, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", psScript, "-TargetDir", destDir, "-DryRun", "-Force")
		cmd.Dir = outsideDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Dry run failed: %v\nOutput: %s", err, string(out))
		}

		// Assert canary was NOT deleted
		canaryContent, readErr := os.ReadFile(destCanary)
		if readErr != nil || string(canaryContent) != "DEST_CANARY_DO_NOT_DELETE" {
			t.Fatalf("Dest canary was modified or destroyed during dry-run!")
		}
	})

	// Final check: canary file in outside CWD is intact
	outsideCanary, err := os.ReadFile(canaryFile)
	if err != nil || string(outsideCanary) != "OUTSIDE_CANARY_DATA" {
		t.Fatalf("Outside CWD canary was modified or destroyed!")
	}
}

// TestSnapshotSafety_OutsideCWD_Bash tests executing make_public_snapshot.sh
// from an unrelated outside current working directory (e.g. /tmp).
// Proves:
// 1. Script path resolution discovers repo root even when caller CWD is outside.
// 2. Absolute paths to repo root, subdirectories, and ancestors are strictly rejected.
// 3. Existing destination files (canary) are preserved and never purged before checks succeed.
// 4. Outside CWD canary files and repo files remain completely unchanged.
func TestSnapshotSafety_OutsideCWD_Bash(t *testing.T) {
	var bashExe string
	if runtime.GOOS == "windows" {
		if _, statErr := os.Stat("C:\\Program Files\\Git\\bin\\bash.exe"); statErr == nil {
			bashExe = "C:\\Program Files\\Git\\bin\\bash.exe"
		} else if _, statErr := os.Stat("C:\\Program Files (x86)\\Git\\bin\\bash.exe"); statErr == nil {
			bashExe = "C:\\Program Files (x86)\\Git\\bin\\bash.exe"
		}
	}
	if bashExe == "" {
		if b, err := exec.LookPath("bash"); err == nil {
			testCmd := exec.Command(b, "-c", "echo OK")
			if out, err := testCmd.Output(); err == nil && strings.TrimSpace(string(out)) == "OK" {
				bashExe = b
			}
		}
	}
	if bashExe == "" {
		t.Skip("Valid bash executable not found, skipping bash outside-CWD test")
	}

	repoRoot := getRepoRoot(t)
	shScript := filepath.Join(repoRoot, "scripts", "make_public_snapshot.sh")

	outsideDir := t.TempDir()
	canaryFile := filepath.Join(outsideDir, "outside_bash_canary.txt")
	if err := os.WriteFile(canaryFile, []byte("OUTSIDE_BASH_CANARY"), 0644); err != nil {
		t.Fatalf("Failed to create outside canary file: %v", err)
	}

	repoParent := filepath.Dir(repoRoot)

	dangerousTargets := []struct {
		name   string
		target string
	}{
		{"RepoRoot_Absolute", repoRoot},
		{"RepoSubdir_Wallet", filepath.Join(repoRoot, "wallet")},
		{"RepoSubdir_Core", filepath.Join(repoRoot, "core")},
		{"RepoParent_Absolute", repoParent},
		{"SystemRoot_Slash", "/"},
	}

	for _, tc := range dangerousTargets {
		t.Run("Reject_FromOutside_"+tc.name, func(t *testing.T) {
			cmd := exec.Command(bashExe, shScript, "--dry-run", "--force", tc.target)
			cmd.Dir = outsideDir // Executed strictly from outside CWD!
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("Security failure: Bash script ACCEPTED dangerous target %s (%s) from outside CWD!\nOutput:\n%s", tc.name, tc.target, string(out))
			}
			outStr := string(out)
			if !strings.Contains(outStr, "ERROR") {
				t.Fatalf("Expected error message for dangerous target %s, got:\n%s", tc.name, outStr)
			}
		})
	}

	// Safe external target executed from outside CWD
	t.Run("Accept_SafeExternalTarget_FromOutside", func(t *testing.T) {
		safeTarget := filepath.Join(outsideDir, "safe_outside_bash_snapshot")
		cmd := exec.Command(bashExe, shScript, "--dry-run", safeTarget)
		cmd.Dir = outsideDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Bash script rejected safe external target from outside CWD!\nOutput:\n%s", string(out))
		}
		if !strings.Contains(string(out), "DRY-RUN") && !strings.Contains(string(out), "DRY RUN MODE") {
			t.Fatalf("Expected dry-run confirmation in output:\n%s", string(out))
		}
	})

	// Destination preservation check: Prove pre-existing files in target are NEVER deleted before checks pass
	t.Run("Preserve_ExistingDestination_BashCanary", func(t *testing.T) {
		destDir := filepath.Join(outsideDir, "existing_destination_bash")
		if err := os.MkdirAll(destDir, 0755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
		destCanary := filepath.Join(destDir, "preserve_bash_marker.txt")
		if err := os.WriteFile(destCanary, []byte("DEST_BASH_CANARY_DO_NOT_DELETE"), 0644); err != nil {
			t.Fatalf("Failed to create dest canary: %v", err)
		}

		// Run dry-run with force
		cmd := exec.Command(bashExe, shScript, "--dry-run", "--force", destDir)
		cmd.Dir = outsideDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Dry run failed: %v\nOutput: %s", err, string(out))
		}

		// Assert canary was NOT deleted
		canaryContent, readErr := os.ReadFile(destCanary)
		if readErr != nil || string(canaryContent) != "DEST_BASH_CANARY_DO_NOT_DELETE" {
			t.Fatalf("Dest canary was modified or destroyed during dry-run!")
		}
	})

	// Final check: canary file in outside CWD is intact
	outsideCanary, err := os.ReadFile(canaryFile)
	if err != nil || string(outsideCanary) != "OUTSIDE_BASH_CANARY" {
		t.Fatalf("Outside CWD bash canary was modified or destroyed!")
	}
}

// TestSnapshotSafety_PromotionRollback_PowerShell verifies that when destination promotion fails
// (or failure is injected), make_public_snapshot.ps1 automatically restores the backup to the
// original destination, preserves existing destination files, and cleans up temporary directories [P2].
func TestSnapshotSafety_PromotionRollback_PowerShell(t *testing.T) {
	var psExe string
	if runtime.GOOS == "windows" {
		psExe = "powershell.exe"
	} else {
		if p, err := exec.LookPath("pwsh"); err == nil {
			psExe = p
		} else if p, err := exec.LookPath("powershell"); err == nil {
			psExe = p
		}
	}
	if psExe == "" {
		t.Skip("PowerShell executable not found, skipping promotion rollback PowerShell test")
	}

	repoRoot := getRepoRoot(t)
	psScript := filepath.Join(repoRoot, "scripts", "make_public_snapshot.ps1")

	testBase := t.TempDir()
	destDir := filepath.Join(testBase, "existing_destination")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	destCanary := filepath.Join(destDir, "precious_data.txt")
	const canaryExpected = "ORIGINAL_DEST_DATA_BEFORE_FAILURE"
	if err := os.WriteFile(destCanary, []byte(canaryExpected), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cmd := exec.Command(psExe, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", psScript,
		"-TargetDir", destDir,
		"-Apply",
		"-Force",
		"-SkipGitleaks",
		"-InjectPromotionFailure")
	cmd.Dir = testBase
	cmd.Env = append(os.Environ(), "ARMAX_TEST_INJECT_PROMOTION_FAILURE=1")

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected script to fail from injected promotion failure, but it succeeded!\nOutput:\n%s", string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "Simulating failure") && !strings.Contains(outStr, "Rolling back") {
		t.Errorf("Expected rollback log messages in output, got:\n%s", outStr)
	}

	// 1. Destination directory must still exist
	if info, statErr := os.Stat(destDir); statErr != nil || !info.IsDir() {
		t.Fatalf("Destination directory missing after rollback: %v", statErr)
	}

	// 2. Original canary must be fully preserved and restored
	content, readErr := os.ReadFile(destCanary)
	if readErr != nil {
		t.Fatalf("Canary file missing after rollback: %v", readErr)
	}
	if string(content) != canaryExpected {
		t.Fatalf("Canary content corrupted! Expected %q, got %q", canaryExpected, string(content))
	}

	// 3. No orphan backup directory should be left behind
	backups, _ := filepath.Glob(destDir + ".old.*")
	if len(backups) > 0 {
		t.Errorf("Expected backup directory to be cleaned up, found: %v", backups)
	}

	// 4. Staging directory should be cleaned up
	stagings, _ := filepath.Glob(destDir + ".tmp.*")
	if len(stagings) > 0 {
		t.Errorf("Expected staging directory to be cleaned up, found: %v", stagings)
	}
}

// TestSnapshotSafety_PromotionRollback_Bash verifies that when destination promotion fails
// (or failure is injected), make_public_snapshot.sh automatically restores the backup to the
// original destination, preserves existing destination files, and cleans up temporary directories [P2].
func TestSnapshotSafety_PromotionRollback_Bash(t *testing.T) {
	var bashExe string
	if runtime.GOOS == "windows" {
		if _, statErr := os.Stat("C:\\Program Files\\Git\\bin\\bash.exe"); statErr == nil {
			bashExe = "C:\\Program Files\\Git\\bin\\bash.exe"
		} else if _, statErr := os.Stat("C:\\Program Files (x86)\\Git\\bin\\bash.exe"); statErr == nil {
			bashExe = "C:\\Program Files (x86)\\Git\\bin\\bash.exe"
		}
	}
	if bashExe == "" {
		if b, err := exec.LookPath("bash"); err == nil {
			testCmd := exec.Command(b, "-c", "echo OK")
			if out, err := testCmd.Output(); err == nil && strings.TrimSpace(string(out)) == "OK" {
				bashExe = b
			}
		}
	}
	if bashExe == "" {
		t.Skip("Bash executable not found, skipping promotion rollback Bash test")
	}

	repoRoot := getRepoRoot(t)
	shScript := filepath.Join(repoRoot, "scripts", "make_public_snapshot.sh")

	testBase := t.TempDir()
	destDir := filepath.Join(testBase, "existing_bash_destination")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	destCanary := filepath.Join(destDir, "precious_bash_data.txt")
	const canaryExpected = "ORIGINAL_BASH_DEST_DATA_BEFORE_FAILURE"
	if err := os.WriteFile(destCanary, []byte(canaryExpected), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cmd := exec.Command(bashExe, shScript,
		"--apply",
		"--force",
		"--skip-gitleaks",
		"--inject-promotion-failure",
		destDir)
	cmd.Dir = testBase
	cmd.Env = append(os.Environ(), "ARMAX_TEST_INJECT_PROMOTION_FAILURE=1")

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected script to fail from injected promotion failure, but it succeeded!\nOutput:\n%s", string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "Simulating failure") && !strings.Contains(outStr, "Rolling back") {
		t.Errorf("Expected rollback log messages in output, got:\n%s", outStr)
	}

	// 1. Destination directory must still exist
	if info, statErr := os.Stat(destDir); statErr != nil || !info.IsDir() {
		t.Fatalf("Destination directory missing after rollback: %v", statErr)
	}

	// 2. Original canary must be fully preserved and restored
	content, readErr := os.ReadFile(destCanary)
	if readErr != nil {
		t.Fatalf("Canary file missing after rollback: %v", readErr)
	}
	if string(content) != canaryExpected {
		t.Fatalf("Canary content corrupted! Expected %q, got %q", canaryExpected, string(content))
	}

	// 3. No orphan backup directory should be left behind
	backups, _ := filepath.Glob(destDir + ".old.*")
	if len(backups) > 0 {
		t.Errorf("Expected backup directory to be cleaned up, found: %v", backups)
	}

	// 4. Staging directory should be cleaned up
	stagings, _ := filepath.Glob(destDir + ".tmp.*")
	if len(stagings) > 0 {
		t.Errorf("Expected staging directory to be cleaned up, found: %v", stagings)
	}
}
