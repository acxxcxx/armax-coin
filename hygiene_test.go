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

// TestNoPersonalDataInTree scans all tracked text files for personal emails and local paths [L4-10-b]
func TestNoPersonalDataInTree(t *testing.T) {
	// 1. Get all tracked files via git ls-files
	cmd := exec.Command("git", "ls-files")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to execute git ls-files: %v", err)
	}

	trackedFiles := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(trackedFiles) == 0 || trackedFiles[0] == "" {
		t.Fatalf("git ls-files returned empty list")
	}

	// 2. Define regex patterns
	// Non-noreply personal email domains
	emailRegex := regexp.MustCompile(`(?i)[a-zA-Z0-9_.+-]+@(gmail\.com|yahoo\.com|outlook\.com|hotmail\.com|proton\.me|protonmail\.com)`)

	// Local filesystem paths
	localPathWindowsRegex := regexp.MustCompile(`(?i)(c:[/\\]users|d:[/\\])`)
	localPathUnixHomeRegex := regexp.MustCompile(`(?i)/home/([a-zA-Z0-9_\.\-]+)`)

	// Binary file extensions to skip
	binaryExts := map[string]bool{
		".syso": true,
		".ico":  true,
		".exe":  true,
		".zip":  true,
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".db":   true,
	}

	var violations []string

	for _, relPath := range trackedFiles {
		relPath = strings.TrimSpace(relPath)
		if relPath == "" {
			continue
		}

		// Skip this test file itself to avoid self-matching pattern literals
		if filepath.Base(relPath) == "hygiene_test.go" {
			continue
		}

		ext := strings.ToLower(filepath.Ext(relPath))
		if binaryExts[ext] {
			continue
		}

		content, err := os.ReadFile(relPath)
		if err != nil {
			t.Logf("Warning: could not read tracked file %s: %v", relPath, err)
			continue
		}

		// Skip binary files if any null byte found in first 1024 bytes
		headerLen := 1024
		if len(content) < headerLen {
			headerLen = len(content)
		}
		if bytes.IndexByte(content[:headerLen], 0) != -1 {
			continue
		}

		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			// Check personal email pattern
			if m := emailRegex.FindString(line); m != "" {
				violations = append(violations, fmt.Sprintf("%s:%d: forbidden personal email found: %s", relPath, lineNum+1, m))
			}

			// Check Windows local paths (C:\Users or D:\)
			if m := localPathWindowsRegex.FindString(line); m != "" {
				violations = append(violations, fmt.Sprintf("%s:%d: forbidden local path found: %s", relPath, lineNum+1, m))
			}

			// Check Unix /home/<nama> (exclude /home/runner for GitHub Actions CI runner)
			if matches := localPathUnixHomeRegex.FindAllStringSubmatch(line, -1); len(matches) > 0 {
				for _, match := range matches {
					if len(match) > 1 {
						username := strings.ToLower(match[1])
						if username != "runner" {
							violations = append(violations, fmt.Sprintf("%s:%d: forbidden local home path found: %s", relPath, lineNum+1, match[0]))
						}
					}
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("[L4-10-b] Personal data hygiene check failed with %d violation(s):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
