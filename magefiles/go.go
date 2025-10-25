//go:build mage

package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

// TargetGoVersion defines the pinned Go version used for reproducible builds.
const TargetGoVersion = "1.25.3"

// Go namespace groups all Go-related tasks.
type Go mg.Namespace

// Verify checks that the Go toolchain is installed and matches the target version.
func (Go) Verify() error {
	fmt.Println("Verifying Go installation...")
	if err := verifyGoVersion(); err != nil {
		return err
	}
	fmt.Println("Go toolchain is correctly installed and verified.")
	return nil
}

// Deps ensures that the correct Go version is installed, installing it if necessary.
func (Go) Deps() error {
	fmt.Println("Ensuring Go dependencies...")

	if err := (Go{}).Verify(); err == nil {
		fmt.Println("Go is already installed and up to date.")
		return nil
	}

	fmt.Printf("Installing Go %s...\n", TargetGoVersion)
	if err := installGoVersion(TargetGoVersion); err != nil {
		return fmt.Errorf("failed to install Go %s: %w", TargetGoVersion, err)
	}

	fmt.Println("Re-verifying Go installation...")
	if err := (Go{}).Verify(); err != nil {
		return fmt.Errorf("Go installation did not verify successfully: %w", err)
	}

	fmt.Println("Go successfully installed and verified.")
	return nil
}

// verifyGoVersion checks that the installed Go version matches the target version.
func verifyGoVersion() error {
	fmt.Printf("Target Go version: %s\n", TargetGoVersion)

	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return fmt.Errorf("go binary not found in PATH")
	}

	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return fmt.Errorf("unexpected output from 'go version': %s", string(out))
	}

	current := strings.TrimPrefix(fields[2], "go")
	if current != TargetGoVersion {
		return fmt.Errorf("Go version mismatch: found %s, expected %s", current, TargetGoVersion)
	}

	// Inform the user if their pinned version is outdated
	checkGoVersionLatest()
	return nil
}

// checkGoVersionLatest queries the official Go site for the latest release
// and warns if the pinned version is behind. If the system is offline or the
// version check cannot be completed, it prints a notice and continues silently.
func checkGoVersionLatest() {
	out, err := exec.Command("curl", "-s", "https://go.dev/VERSION?m=text").Output()
	if err != nil {
		fmt.Println("Skipping Go version update check (network unavailable or offline).")
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		fmt.Println("Unable to parse Go version information from remote source.")
		return
	}

	latest := strings.TrimPrefix(strings.TrimSpace(lines[0]), "go")
	if latest == "" {
		fmt.Println("Unable to parse Go version information from remote source.")
		return
	}

	if latest != TargetGoVersion {
		fmt.Printf("Note: a newer Go version is available (%s). You are pinned to %s.\n", latest, TargetGoVersion)
	}
}

// installGoVersion downloads and installs the specified Go version, verifying its checksum if available.
func installGoVersion(version string) error {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	var goOS, goArch string
	switch osName {
	case "linux":
		goOS = "linux"
	case "darwin":
		goOS = "darwin"
	default:
		return fmt.Errorf("unsupported OS: %s", osName)
	}

	switch arch {
	case "amd64":
		goArch = "amd64"
	case "arm64":
		goArch = "arm64"
	default:
		return fmt.Errorf("unsupported architecture: %s", arch)
	}

	url := fmt.Sprintf("https://go.dev/dl/go%s.%s-%s.tar.gz", version, goOS, goArch)
	tmpFile := fmt.Sprintf("/tmp/go%s.%s-%s.tar.gz", version, goOS, goArch)

	fmt.Printf("Downloading Go %s for %s/%s...\n", version, goOS, goArch)
	if err := sh.RunV("curl", "-L", "-o", tmpFile, url); err != nil {
		return fmt.Errorf("failed to download Go: %w", err)
	}

	// Attempt checksum verification if available
	checksumURL := url + ".sha256"
	checksumFile := tmpFile + ".sha256"
	if err := sh.RunV("curl", "-s", "-L", "-o", checksumFile, checksumURL); err == nil {
		fmt.Println("Verifying checksum...")
		if err := sh.RunV("sha256sum", "-c", checksumFile); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	fmt.Println("Extracting Go to /usr/local/go (requires sudo)...")
	if err := sh.RunV("sudo", "rm", "-rf", "/usr/local/go"); err != nil {
		return err
	}
	if err := sh.RunV("sudo", "tar", "-C", "/usr/local", "-xzf", tmpFile); err != nil {
		return err
	}

	fmt.Println("Verifying installation...")
	if err := sh.RunV("/usr/local/go/bin/go", "version"); err != nil {
		return err
	}

	// Check PATH visibility
	if _, err := exec.LookPath("go"); err != nil {
		fmt.Println("Note: /usr/local/go/bin may not be in your PATH. You may need to update your shell configuration.")
	}

	return nil
}
