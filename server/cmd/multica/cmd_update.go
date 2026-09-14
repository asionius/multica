package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const officialInstallScriptURL = "https://raw.githubusercontent.com/asionius/multica/main/scripts/install.sh"

var updateDownloadTimeout time.Duration = cli.DefaultUpdateDownloadTimeout

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update multica to the latest version",
	Long:  "Install the latest CLI via the official install script, then restart the local daemon while preserving its device name.",
	RunE:  runUpdate,
}

func init() {
	updateCmd.Flags().DurationVar(&updateDownloadTimeout, "download-timeout", cli.DefaultUpdateDownloadTimeout, "Maximum time to wait for the release archive download (Windows)")
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	if updateDownloadTimeout <= 0 {
		return fmt.Errorf("download timeout must be greater than zero")
	}

	fmt.Fprintf(os.Stderr, "Current version: %s (commit: %s, built: %s)\n", version, commit, date)

	latest, err := cli.FetchLatestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not check latest version: %v\n", err)
	} else {
		latestVer := strings.TrimPrefix(latest.TagName, "v")
		currentVer := strings.TrimPrefix(version, "v")
		if currentVer == latestVer {
			fmt.Fprintln(os.Stderr, "Already up to date.")
			return nil
		}
		fmt.Fprintf(os.Stderr, "Latest version:  %s\n\n", latest.TagName)
	}

	// Capture device_name from the running daemon before replacing the binary.
	// Same source as `multica daemon status --output json | jq .device_name`.
	daemonRunning, deviceName := lookupDaemonDeviceName(cmd)

	if runtime.GOOS == "windows" {
		if err := runUpdateWindows(latest); err != nil {
			return err
		}
	} else if err := runOfficialInstallScript(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	if !daemonRunning {
		fmt.Fprintln(os.Stderr, "Daemon is not running; skipping restart.")
		return nil
	}

	return restartUpdatedDaemon(cmd, deviceName)
}

func lookupDaemonDeviceName(cmd *cobra.Command) (bool, string) {
	profile := resolveProfile(cmd)
	healthPort := resolveHealthPort(0, profile)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health := checkDaemonHealthOnPort(ctx, healthPort)
	return health["status"] == "running", deviceNameFromHealth(health)
}

func deviceNameFromHealth(health map[string]any) string {
	name, _ := health["device_name"].(string)
	return strings.TrimSpace(name)
}

func buildPostUpdateRestartArgs(deviceName, profile string) []string {
	args := []string{"daemon", "restart"}
	if deviceName != "" {
		args = append(args, "--device-name", deviceName)
	}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	return args
}

func runOfficialInstallScript() error {
	if _, err := exec.LookPath("curl"); err != nil {
		return fmt.Errorf("curl is required to run the Multica install script: %w", err)
	}
	if _, err := exec.LookPath("bash"); err != nil {
		return fmt.Errorf("bash is required to run the Multica install script: %w", err)
	}

	script := "curl -fsSL " + officialInstallScriptURL + " | bash"
	c := exec.Command("bash", "-c", script)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("install script failed: %w", err)
	}
	return nil
}

func restartUpdatedDaemon(cmd *cobra.Command, deviceName string) error {
	// Look up `multica` on PATH so we exec the binary the install script just
	// wrote. os.Executable() on Linux follows /proc/self/exe, which still
	// points at the replaced inode of this process.
	exe, err := exec.LookPath("multica")
	if err != nil {
		hint := "multica daemon restart"
		if deviceName != "" {
			hint = fmt.Sprintf("multica daemon restart --device-name %q", deviceName)
		}
		return fmt.Errorf("find updated multica binary: %w\nThe CLI was updated; start the daemon with: %s", err, hint)
	}

	args := buildPostUpdateRestartArgs(deviceName, resolveProfile(cmd))
	if deviceName != "" {
		fmt.Fprintf(os.Stderr, "Restarting daemon with --device-name %q\n", deviceName)
	} else {
		fmt.Fprintln(os.Stderr, "Restarting daemon...")
	}

	c := exec.Command(exe, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("daemon restart failed: %w", err)
	}
	return nil
}

func runUpdateWindows(latest *cli.GitHubRelease) error {
	if cli.IsBrewInstall() {
		fmt.Fprintln(os.Stderr, "Updating via Homebrew...")
		output, err := cli.UpdateViaBrew()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", output)
			return fmt.Errorf("brew upgrade failed: %w\nYou can try manually: brew upgrade multica-ai/tap/multica", err)
		}
		fmt.Fprintln(os.Stderr, "Update complete.")
		return nil
	}

	if latest == nil {
		return fmt.Errorf("could not determine latest version; check https://github.com/asionius/multica/releases/latest")
	}
	targetVersion := latest.TagName
	fmt.Fprintf(os.Stderr, "Downloading %s from GitHub Releases...\n", targetVersion)
	output, err := cli.UpdateViaDownloadWithTimeout(targetVersion, updateDownloadTimeout)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s\nUpdate complete.\n", output)
	return nil
}
