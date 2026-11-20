//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

func verifyNodeNetwork(url string, expectedNetID string) error {
	return verifyNodeNetworkErr(url, expectedNetID)
}

func findUnixAppBrowser() string {
	candidates := []string{"chromium", "chromium-browser", "google-chrome", "microsoft-edge", "brave-browser"}
	for _, bin := range candidates {
		if path, err := exec.LookPath(bin); err == nil && strings.TrimSpace(path) != "" {
			return path
		}
	}
	return ""
}

func desktopUsesEmbeddedWindow() bool {
	return false
}

func isWSLEnv() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "" || strings.TrimSpace(os.Getenv("WSL_INTEROP")) != "" {
		return true
	}
	if data, err := os.ReadFile("/proc/version"); err == nil {
		ver := strings.ToLower(string(data))
		if strings.Contains(ver, "microsoft") || strings.Contains(ver, "wsl") {
			return true
		}
	}
	return false
}

func findWindowsInteropExe(name string, fallbacks ...string) string {
	if p, err := exec.LookPath(name); err == nil && strings.TrimSpace(p) != "" {
		return p
	}
	for _, fb := range fallbacks {
		if fi, err := os.Stat(fb); err == nil && !fi.IsDir() {
			return fb
		}
	}
	return ""
}

func launchWSLBrowser(launchURL string) (string, bool) {
	type wslLauncher struct {
		label string
		bin   string
		args  []string
	}
	var candidates []wslLauncher
	if rundll := findWindowsInteropExe("rundll32.exe", "/mnt/c/Windows/System32/rundll32.exe"); rundll != "" {
		candidates = append(candidates, wslLauncher{
			label: "rundll32.exe",
			bin:   rundll,
			args:  []string{"url.dll,FileProtocolHandler", launchURL},
		})
	}
	if wslview, err := exec.LookPath("wslview"); err == nil && strings.TrimSpace(wslview) != "" {
		candidates = append(candidates, wslLauncher{
			label: "wslview",
			bin:   wslview,
			args:  []string{launchURL},
		})
	}
	if cmdExe := findWindowsInteropExe("cmd.exe", "/mnt/c/Windows/System32/cmd.exe"); cmdExe != "" {
		candidates = append(candidates, wslLauncher{
			label: "cmd.exe",
			bin:   cmdExe,
			args:  []string{"/c", "start", "", launchURL},
		})
	}
	if explorer := findWindowsInteropExe("explorer.exe", "/mnt/c/Windows/explorer.exe"); explorer != "" {
		candidates = append(candidates, wslLauncher{
			label: "explorer.exe",
			bin:   explorer,
			args:  []string{launchURL},
		})
	}

	for _, cand := range candidates {
		c := exec.Command(cand.bin, cand.args...)
		if err := c.Start(); err == nil {
			go func(child *exec.Cmd) {
				_ = child.Wait()
			}(c)
			return cand.label, true
		}
	}
	return "", false
}

func runDesktopWindow(guiURL string, launchSecret string, windowTitle string, exitCh <-chan struct{}, onClosed func()) error {
	_ = windowTitle
	var mu sync.Mutex
	var cmd *exec.Cmd
	var profileDir string

	cleanup := func() {
		mu.Lock()
		c := cmd
		dir := profileDir
		cmd = nil
		profileDir = ""
		mu.Unlock()

		if c != nil && c.Process != nil {
			_ = c.Process.Kill()
		}
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}
	defer cleanup()

	launchURL := strings.TrimRight(guiURL, "/") + "/auth/launch?ticket=" + launchSecret
	launched := false

	if runtime.GOOS == "darwin" {
		fmt.Println("[*] Launching macOS desktop browser window...")
		c := exec.Command("open", launchURL)
		if err := c.Start(); err == nil {
			launched = true
			go func(child *exec.Cmd) {
				_ = child.Wait()
			}(c)
		}
	}

	hasLinuxDisplay := strings.TrimSpace(os.Getenv("DISPLAY")) != "" || strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != ""

	if !launched && runtime.GOOS != "darwin" && hasLinuxDisplay {
		if appBrowser := findUnixAppBrowser(); appBrowser != "" {
			tmpDir, err := os.MkdirTemp("", "armax-gui-profile-*")
			if err == nil {
				c := exec.Command(appBrowser,
					"--app="+launchURL,
					"--window-size=1240,860",
					"--user-data-dir="+tmpDir,
					"--no-first-run",
					"--no-default-browser-check",
				)
				startedAt := time.Now()
				if c.Start() == nil {
					mu.Lock()
					cmd = c
					profileDir = tmpDir
					mu.Unlock()
					launched = true
					fmt.Printf("[*] Launched Armaxcoin desktop window (%s)\n", appBrowser)
					go func(child *exec.Cmd, started time.Time) {
						_ = child.Wait()
						// Only treat window exit as user close if the standalone app stayed alive >= 3s;
						// fast <3s exits mean an existing browser process adopted the tab/window.
						if time.Since(started) >= 3*time.Second {
							if onClosed != nil {
								onClosed()
							}
						}
					}(c, startedAt)
				} else {
					_ = os.RemoveAll(tmpDir)
				}
			}
		}
	}

	if !launched && isWSLEnv() {
		if launcher, ok := launchWSLBrowser(launchURL); ok {
			launched = true
			fmt.Printf("[*] Launching Windows browser from WSL (%s)...\n", launcher)
		}
	}

	if !launched && runtime.GOOS != "darwin" && hasLinuxDisplay {
		if xdgPath, err := exec.LookPath("xdg-open"); err == nil && strings.TrimSpace(xdgPath) != "" {
			c := exec.Command(xdgPath, launchURL)
			if err := c.Start(); err == nil {
				launched = true
				fmt.Println("[*] Launching system default browser...")
				go func(child *exec.Cmd) {
					_ = child.Wait()
				}(c)
			}
		}
	}

	if !launched {
		fmt.Printf("[!] Could not auto-open browser. Open this Launch URL in your browser:\n    %s\n", launchURL)
	}

	if exitCh != nil {
		<-exitCh
	}
	return nil
}

func main() {
	runGUIServerMain()
}
