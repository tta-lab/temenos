package daemon

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const daemonPlistLabel = "io.guion.temenos.daemon"

// Install installs the launchd plist for the temenos daemon.
func Install() error {
	temenosBin, err := exec.LookPath("temenos")
	if err != nil {
		return fmt.Errorf("temenos not found in PATH — install with: make install")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dataDir := filepath.Join(home, ".ttal")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	return installDaemonPlist(home, temenosBin, dataDir)
}

// Uninstall removes the launchd plist and cleans up daemon files.
func Uninstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", daemonPlistLabel+".plist")

	if _, err := os.Stat(plistPath); err != nil {
		fmt.Println("Daemon plist: not installed")
	} else {
		uid := os.Getuid()
		cmd := exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, daemonPlistLabel))
		cmd.Run() //nolint:errcheck

		os.Remove(plistPath) //nolint:errcheck
		fmt.Printf("Removed daemon plist: %s\n", plistPath)
	}

	sockPath := DefaultSocketPath()
	os.Remove(sockPath) //nolint:errcheck

	fmt.Println("Daemon uninstalled. Logs preserved.")
	return nil
}

// Status prints whether the temenos daemon is running.
func Status() error {
	label := daemonPlistLabel
	uid := os.Getuid()
	cmd := exec.Command("launchctl", "list", label)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Daemon not running (label: %s, uid: %d)\n", label, uid)
		return nil
	}
	fmt.Printf("Daemon running:\n%s\n", strings.TrimSpace(string(out)))
	return nil
}

// Start boots the daemon via launchd.
func Start() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", daemonPlistLabel+".plist")
	if _, err := os.Stat(plistPath); err != nil {
		return fmt.Errorf("daemon not installed (run: temenos daemon install)")
	}

	uid := os.Getuid()
	cmd := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		outStr := strings.TrimSpace(string(out))
		if strings.Contains(outStr, "already bootstrapped") || strings.Contains(outStr, "36:") {
			fmt.Println("Daemon already running")
			return nil
		}
		return fmt.Errorf("launchctl bootstrap failed: %w: %s", err, outStr)
	}

	fmt.Println("Daemon started")
	return nil
}

// Stop stops the daemon via launchd.
func Stop() error {
	uid := os.Getuid()
	cmd := exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, daemonPlistLabel))
	if out, err := cmd.CombinedOutput(); err != nil {
		outStr := strings.TrimSpace(string(out))
		if strings.Contains(outStr, "No such process") || strings.Contains(outStr, "3:") {
			fmt.Println("Daemon not running")
			return nil
		}
		return fmt.Errorf("launchctl bootout failed: %w: %s", err, outStr)
	}

	fmt.Println("Daemon stopped")
	return nil
}

// Restart performs an atomic restart using launchctl kickstart -k.
func Restart() error {
	uid := os.Getuid()
	target := fmt.Sprintf("gui/%d/%s", uid, daemonPlistLabel)
	cmd := exec.Command("launchctl", "kickstart", "-k", target)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kickstart -k failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func installDaemonPlist(home, temenosBin, dataDir string) error {
	plistPath := filepath.Join(home, "Library", "LaunchAgents", daemonPlistLabel+".plist")

	uid := os.Getuid()
	// Remove existing service before reinstalling
	cmd := exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, daemonPlistLabel))
	cmd.Run() //nolint:errcheck

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>

    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
    </array>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>

    <key>StandardOutPath</key>
    <string>%s/temenos.stdout.log</string>

    <key>StandardErrorPath</key>
    <string>%s/temenos.stderr.log</string>

    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin:%s/.local/bin:%s/go/bin:%s/.cargo/bin</string>
    </dict>
</dict>
</plist>
`, daemonPlistLabel, xmlEscape(temenosBin), dataDir, dataDir, home, home, home)

	if err := os.WriteFile(plistPath, []byte(plist), 0o600); err != nil {
		return err
	}

	cmd = exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	fmt.Printf("Daemon plist installed: %s\n", plistPath)
	fmt.Printf("Logs: %s/temenos.stdout.log\n", dataDir)
	return nil
}

// xmlEscape escapes a string for safe embedding in XML/plist content.
func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}
