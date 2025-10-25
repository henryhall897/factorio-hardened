//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

// Lint namespace handles golangci-lint installation and linting checks.
type Lint mg.Namespace

// Verify checks that golangci-lint is installed and compatible with the current Go version.
func (Lint) Verify() error {
	fmt.Println("Verifying golangci-lint installation...")
	if err := verifyLinter(); err != nil {
		return err
	}
	fmt.Println("golangci-lint is correctly installed and compatible.")
	return nil
}

// Deps ensures that golangci-lint is installed and built with the current Go version.
func (Lint) Deps() error {
	fmt.Println("Ensuring golangci-lint dependencies...")

	if err := (Lint{}).Verify(); err == nil {
		return nil
	}

	fmt.Println("Installing or rebuilding golangci-lint...")
	if err := installLinter(); err != nil {
		return fmt.Errorf("failed to install golangci-lint: %w", err)
	}

	fmt.Println("Re-verifying golangci-lint installation...")
	if err := (Lint{}).Verify(); err != nil {
		return fmt.Errorf("golangci-lint installation did not verify successfully: %w", err)
	}

	fmt.Println("golangci-lint successfully installed and verified.")
	return nil
}

// Run executes golangci-lint using the project configuration.
func (Lint) Run() error {
	fmt.Println("Running golangci-lint checks...")

	cmd := exec.Command("golangci-lint", "run")
	out, err := cmd.CombinedOutput()
	output := string(out)

	if strings.Contains(output, "no go files to analyze") {
		fmt.Println("No Go packages found — skipping lint.")
		return nil
	}

	fmt.Print(output)
	if err != nil {
		return fmt.Errorf("linting failed: %w", err)
	}

	fmt.Println("No lint issues found.")
	return nil
}

// verifyLinter checks whether golangci-lint is installed and compatible with the current Go version.
func verifyLinter() error {
	currentGo := strings.TrimPrefix(runtime.Version(), "go")
	path, err := exec.LookPath("golangci-lint")
	if err != nil {
		return fmt.Errorf("golangci-lint not found in PATH")
	}

	out, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute golangci-lint: %w", err)
	}

	outStr := string(out)
	if !strings.Contains(outStr, "built with go1.") {
		fmt.Println("golangci-lint found, but build version information is unavailable — rebuilding recommended.")
		return fmt.Errorf("unable to determine golangci-lint build version")
	}

	fields := strings.Fields(outStr)
	for _, f := range fields {
		if strings.HasPrefix(f, "go1.") {
			buildGo := strings.TrimPrefix(f, "go")
			if buildGo < currentGo {
				return fmt.Errorf("golangci-lint built with Go %s < current %s", buildGo, currentGo)
			}
			return nil
		}
	}

	return fmt.Errorf("unable to determine golangci-lint build version")
}

// installLinter installs or rebuilds golangci-lint to match the current Go version.
func installLinter() error {
	fmt.Println("Installing golangci-lint using current Go toolchain...")
	if err := sh.RunV("go", "install", "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"); err != nil {
		return fmt.Errorf("failed to install golangci-lint: %w", err)
	}

	addGoBinToPath()
	return nil
}

// addGoBinToPath ensures that the Go bin directory is included in PATH for the current session.
func addGoBinToPath() {
	goBin := fmt.Sprintf("%s/go/bin", os.Getenv("HOME"))
	path := os.Getenv("PATH")
	if !strings.Contains(path, goBin) {
		os.Setenv("PATH", fmt.Sprintf("%s:%s", goBin, path))
		fmt.Println("Added Go bin directory to PATH for this session.")
	}
}
