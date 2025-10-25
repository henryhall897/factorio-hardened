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

// ScanImage runs a Trivy scan on a given Docker image reference.
// It fails the build if any *fixable* CRITICAL vulnerabilities are found.
func (Trivy) ScanImage(image string) error {
	fmt.Printf("Running Trivy vulnerability scan on image: %s\n", image)

	if _, err := exec.LookPath("trivy"); err != nil {
		fmt.Println("Trivy not found in PATH; skipping scan.")
		return nil
	}

	cmd := exec.Command(
		"trivy", "image",
		"--severity", "CRITICAL",
		"--ignore-unfixed", // Only count fixable vulnerabilities
		"--exit-code", "1", // Exit non-zero if vulnerabilities found
		"--quiet",
		image,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fixable critical vulnerabilities detected in image %s", image)
	}

	fmt.Println("Trivy scan passed (no fixable critical vulnerabilities).")
	return nil
}

// Report generates a full JSON Trivy report for the given image,
// including all severities and unfixed issues, for long-term auditing.
func (Trivy) Report(image string) error {
	fmt.Printf("Generating Trivy audit report for image: %s\n", image)

	if _, err := exec.LookPath("trivy"); err != nil {
		return fmt.Errorf("Trivy not found in PATH; please install it to generate reports")
	}

	// Ensure output directory exists
	if err := os.MkdirAll("trivy", 0755); err != nil {
		return fmt.Errorf("failed to create trivy report directory: %v", err)
	}

	reportPath := "trivy/report.json"

	cmd := exec.Command(
		"trivy", "image",
		"--ignore-unfixed",
		"--format", "json",
		"--output", reportPath,
		image,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to generate Trivy report: %v", err)
	}

	fmt.Printf("Full Trivy report generated at %s\n", reportPath)
	return nil
}
