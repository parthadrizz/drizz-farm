package cmd

// Shared prerequisite check + auto-install pipeline.
//
// Used by both `drizz-farm setup` (first-run install) and `drizz-farm
// start` (self-heal if the user deleted openjdk, broke brew, moved the
// SDK, etc.). The idea: every run detects. Stored paths in config.yaml
// are a cache, never the source of truth — if anything's off, we fix
// it before proceeding.
//
// autoInstall=true  → missing required deps trigger a brew install
//                     automatically, with live streamed output.
// autoInstall=false → we just report status and return; caller decides.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// EnsurePrereqs runs the full prereq sequence. Returns true if every
// required prereq is verified working by the end (including after any
// auto-install attempts).
func EnsurePrereqs(autoInstall bool) bool {
	printStatus := func(label, mark, detail string) {
		fmt.Printf("  %s %-28s %s\n", mark, label, detail)
	}
	ensure := func(label string, fn func() checkResult, optional bool) checkResult {
		fmt.Printf("  → %-28s checking...", label)
		os.Stdout.Sync()
		c := fn()
		fmt.Printf("\r\033[K")

		if c.ok {
			printStatus(label, "✓", c.detail)
			return c
		}
		if !autoInstall || c.fixCmd == "" {
			mark := "✗"
			extra := ""
			if optional {
				mark = "○"
				extra = " (optional — skipping)"
			}
			printStatus(label, mark, c.detail+extra)
			return c
		}

		printStatus(label, "✗", c.detail)
		fixCmd := c.fixCmd
		if strings.HasPrefix(fixCmd, "brew ") {
			for _, brewPath := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
				if _, err := os.Stat(brewPath); err == nil {
					fixCmd = brewPath + strings.TrimPrefix(fixCmd, "brew")
					break
				}
			}
		}
		fmt.Printf("    → installing: %s\n", fixCmd)
		installCmd := exec.Command("sh", "-c", fixCmd)
		installCmd.Env = append(os.Environ(),
			"PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
			"HOMEBREW_NO_AUTO_UPDATE=1",
		)
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			printStatus(label, "✗", fmt.Sprintf("install failed: %v", err))
			return c
		}

		// After JDK install, ask brew where it went and export JAVA_HOME
		// in our process so every later findBinary call picks it up.
		if label == "Java JDK" {
			exportJavaHomeFromBrew()
		}

		fmt.Printf("  → %-28s verifying...", label)
		os.Stdout.Sync()
		c2 := fn()
		fmt.Printf("\r\033[K")
		if c2.ok {
			printStatus(label, "✓", c2.detail+" (installed)")
			return c2
		}
		printStatus(label, "✗", "install completed but verification failed: "+c2.detail)
		return c2
	}

	brew := ensure("Package manager", checkPkgMgr, false)
	jdk := ensure("Java JDK", checkJDK, false)
	// Export JAVA_HOME if we already have a working JDK from a prior
	// setup — otherwise sdkmanager/avdmanager will fail even when
	// brew isn't touched this run.
	if jdk.ok {
		exportJavaHomeFromBrew()
	}
	sdk := ensure("Android SDK", checkAndroidSDK, false)
	cmdline := ensure("Android cmdline-tools", checkAndroidCmdlineTools, false)
	adb := ensure("Android platform-tools", checkAndroidPlatformTools, false)
	emu := ensure("Android Emulator", checkAndroidEmulator, false)

	if jdk.ok {
		_ = ensure("Android system images", checkAndroidSystemImages, true)
	} else {
		printStatus("Android system images", "○", "skipped (needs working JDK)")
	}
	if runtime.GOOS == "darwin" {
		_ = ensure("Xcode CLI Tools", checkXcodeCLI, true)
	}

	// ── Appium toolchain. Required for /wd/hub drop-in compatibility.
	// Checked after the Android stack so setup shows a coherent
	// progression (device → JDK → SDK → tools → Appium → mitmproxy).
	node := ensure("Node.js", checkNode, false)
	var appium checkResult
	if node.ok {
		appium = ensure("Appium", checkAppium, false)
		if appium.ok {
			_ = ensure("Appium uiautomator2 driver", checkAppiumDriverUIA2, false)
		} else {
			printStatus("Appium uiautomator2 driver", "○", "skipped (needs Appium)")
		}
	} else {
		printStatus("Appium", "○", "skipped (needs Node)")
		printStatus("Appium uiautomator2 driver", "○", "skipped (needs Node)")
	}

	// mitmproxy is optional — only sessions that set
	// drizz:capture_network=true require it. We still try to install
	// it when autoInstall=true so the feature Just Works when users
	// flip the capability later.
	_ = ensure("mitmproxy", checkMitmproxy, true)

	allOK := true
	for _, c := range []checkResult{brew, jdk, sdk, cmdline, adb, emu, node} {
		if !c.ok {
			allOK = false
		}
	}
	if node.ok && !appium.ok {
		allOK = false
	}
	return allOK
}

// exportJavaHomeFromBrew asks `brew --prefix` for openjdk@17 / 21 / any,
// picks the first one whose javac exists, and sets JAVA_HOME on the
// current process. No-op if no openjdk is installed.
func exportJavaHomeFromBrew() {
	for _, brewBin := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if _, err := os.Stat(brewBin); err != nil {
			continue
		}
		for _, formula := range []string{"openjdk@17", "openjdk@21", "openjdk"} {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			out, err := exec.CommandContext(ctx, brewBin, "--prefix", formula).Output()
			cancel()
			if err != nil {
				continue
			}
			prefix := strings.TrimSpace(string(out))
			if prefix == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(prefix, "bin", "javac")); err == nil {
				os.Setenv("JAVA_HOME", prefix)
				return
			}
		}
		return
	}
}
