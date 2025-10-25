//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/magefile/mage/mg"
)

// System namespace handles installation and verification of base system tools
// and host-level configuration.
type System mg.Namespace

// Verify checks that required system tools are installed and that file permissions are secure.
func (System) Verify() error {
	fmt.Println("Verifying system tools and configuration...")

	if err := verifySystemTools(); err != nil {
		return err
	}

	if err := (System{}).Permissions(); err != nil {
		return err
	}

	fmt.Println("System verification completed successfully.")
	return nil
}

// Deps ensures that required system tools are installed and that file permissions are secure.
// It explicitly checks for insecure permissions even when Verify() passes, allowing self-healing.
func (System) Deps() error {
	fmt.Println("Ensuring system dependencies...")

	// Step 1: Verify required tools
	verifyErr := (System{}).Verify()
	if verifyErr != nil {
		fmt.Println("Detected missing system tools. Attempting repair...")
		if err := installSystemTools(); err != nil {
			return fmt.Errorf("failed to install system tools: %w", err)
		}
	}

	// Step 2: Always check permissions (even if Verify succeeded)
	if permErr := (System{}).Permissions(); permErr != nil {
		fmt.Println("Attempting to fix file permissions...")
		if fixErr := (System{}).FixPermissions(); fixErr != nil {
			return fmt.Errorf("failed to fix file permissions: %w", fixErr)
		}
	}

	// Step 3: Re-verify configuration
	fmt.Println("Re-verifying system configuration...")
	if err := (System{}).Verify(); err != nil {
		return fmt.Errorf("system verification failed after repair: %w", err)
	}

	fmt.Println("System dependencies and configuration verified successfully.")
	return nil
}

// verifySystemTools checks that all required system binaries are available in PATH.
func verifySystemTools() error {
	required := []string{"curl", "git"}
	for _, bin := range required {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s not found in PATH", bin)
		}
	}
	return nil
}

// installSystemTools installs required base tools using the system package manager.
func installSystemTools() error {
	cmds := [][]string{
		{"sudo", "apt-get", "update", "-y"},
		{"sudo", "apt-get", "install", "-y", "curl", "git"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed running %v: %w", args, err)
		}
	}

	fmt.Println("Base system tools installed successfully.")
	return nil
}

// Permissions checks sensitive files for overly permissive modes.
// It returns an error if any monitored file has insecure permissions.
func (System) Permissions() error {
	files := []string{"Dockerfile", ".env", "magefiles/ghcr.go"}
	insecure := false

	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		mode := info.Mode().Perm()
		if mode&0o022 != 0 {
			insecure = true
			fmt.Printf("Warning: %s has overly permissive permissions (%#o)\n", f, mode)
		}
	}

	if insecure {
		return fmt.Errorf("one or more files have insecure permissions")
	}

	fmt.Println("All monitored file permissions are secure.")
	return nil
}

// FixPermissions corrects overly permissive file modes for sensitive files.
// It removes world and group write bits but preserves ownership and other flags.
func (System) FixPermissions() error {
	files := []string{"Dockerfile", ".env", "magefiles/ghcr.go"}
	dryRun := os.Getenv("DRY_RUN") != ""

	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}

		mode := info.Mode().Perm()
		if mode&0o022 == 0 {
			continue
		}

		newMode := mode &^ 0o022
		if dryRun {
			fmt.Printf("[dry-run] Would fix permissions for %s (%#o → %#o)\n", f, mode, newMode)
			continue
		}

		if err := os.Chmod(f, newMode); err != nil {
			fmt.Printf("Failed to fix permissions for %s: %v\n", f, err)
		} else {
			fmt.Printf("Fixed permissions for %s (%#o → %#o)\n", f, mode, newMode)
		}
	}

	fmt.Println("Permission correction process completed.")
	return nil
}
