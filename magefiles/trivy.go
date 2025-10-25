//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

// Trivy namespace handles installation and execution of the Trivy vulnerability scanner.
type Trivy mg.Namespace

// Verify checks that Trivy is installed and available in PATH.
func (Trivy) Verify() error {
	fmt.Println("Verifying Trivy installation...")
	if err := verifyTrivy(); err != nil {
		return err
	}
	fmt.Println("Trivy is correctly installed and available.")
	return nil
}

// Deps ensures that Trivy is installed, installing it if necessary.
func (Trivy) Deps() error {
	fmt.Println("Ensuring Trivy dependencies...")

	if err := (Trivy{}).Verify(); err == nil {
		fmt.Println("Trivy is already installed.")
		return nil
	}

	fmt.Println("Installing Trivy vulnerability scanner...")
	if err := installTrivy(); err != nil {
		return fmt.Errorf("failed to install Trivy: %w", err)
	}

	fmt.Println("Re-verifying Trivy installation...")
	if err := (Trivy{}).Verify(); err != nil {
		return fmt.Errorf("Trivy installation did not verify successfully: %w", err)
	}

	fmt.Println("Trivy successfully installed and verified.")
	return nil
}

// ImageScan runs a vulnerability scan on a specified container image using Trivy.
func (Trivy) ImageScan() error {
	image := "ghcr.io/henryhall897/factorio-hardened:latest"
	fmt.Printf("Scanning image %s for vulnerabilities...\n", image)

	cmd := exec.Command("trivy", "image", "--severity", "CRITICAL,HIGH", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Trivy scan failed: %w", err)
	}

	fmt.Println("Image vulnerability scan completed successfully.")
	return nil
}

// verifyTrivy checks if Trivy is installed and available in PATH.
func verifyTrivy() error {
	if _, err := exec.LookPath("trivy"); err != nil {
		return fmt.Errorf("Trivy not found in PATH")
	}
	return nil
}

// installTrivy installs Aqua Security’s Trivy vulnerability scanner if not present.
func installTrivy() error {
	if _, err := exec.LookPath("trivy"); err == nil {
		fmt.Println("Trivy is already installed.")
		return nil
	}

	fmt.Println("Installing Trivy vulnerability scanner...")
	return sh.RunV("sudo", "apt-get", "install", "-y", "trivy")
}
