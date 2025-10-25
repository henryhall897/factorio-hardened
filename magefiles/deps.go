//go:build mage

package main

import (
	"fmt"

	"github.com/magefile/mage/mg"
)

// Deps namespace coordinates installation and configuration of all dependencies
// required for building, testing, and publishing the Factorio-Hardened project.
// It delegates to subsystem namespaces (System, Go, Docker, Github, Lint, Trivy)
// and ensures the environment is self-healing and reproducible.
type Deps mg.Namespace

// All runs all dependency checks and installation routines in sequence.
// It ensures the host environment is fully configured before builds or CI runs.
func (Deps) All() error {
	fmt.Println("Ensuring all dependencies for Factorio-Hardened are installed and verified...")

	steps := []struct {
		name string
		fn   func() error
	}{
		{"System tools and permissions", func() error { return (System{}).Deps() }},
		{"Go toolchain", func() error { return (Go{}).Deps() }},
		{"Docker engine and GHCR authentication", func() error { return (Docker{}).Deps() }},
		{"GolangCI-Lint installation", func() error { return (Lint{}).Deps() }},
		{"Trivy vulnerability scanner", func() error { return (Trivy{}).Deps() }},
		{"GitHub authentication and token scopes", func() error { return (Github{}).Deps() }},
	}

	for _, step := range steps {
		fmt.Printf("Starting: %s...\n", step.name)
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s failed: %w", step.name, err)
		}
		fmt.Printf("Completed: %s verified successfully.\n\n", step.name)
	}

	fmt.Println("All dependencies are installed, configured, and verified successfully.")
	return nil
}
