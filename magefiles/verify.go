//go:build mage

package main

import (
	"fmt"

	"github.com/magefile/mage/mg"
)

// Verify namespace coordinates non-mutating environment and dependency validation
// across all namespaces (System, Go, Docker, Github, Lint, Trivy).
// It performs comprehensive read-only checks to confirm the environment
// is ready for secure, reproducible builds without altering host state.
type Verify mg.Namespace

// All runs all verification checks in sequence.
// Unlike Deps.All, this does not install or modify anything.
// It is intended for CI pipelines or post-installation validation.
func (Verify) All() error {
	fmt.Println("Running full environment verification for Factorio-Hardened...")

	steps := []struct {
		name string
		fn   func() error
	}{
		{"System tools and permissions", func() error { return (System{}).Verify() }},
		{"Go toolchain", func() error { return (Go{}).Verify() }},
		{"Docker installation and authentication", func() error { return (Docker{}).Verify() }},
		{"GolangCI-Lint installation", func() error { return (Lint{}).Verify() }},
		{"Trivy vulnerability scanner", func() error { return (Trivy{}).Verify() }},
		{"GitHub authentication and token scopes", func() error { return (Github{}).ValidateAll() }},
	}

	for _, step := range steps {
		fmt.Printf("Starting: %s...\n", step.name)
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s failed: %w", step.name, err)
		}
		fmt.Printf("Completed: %s verified successfully.\n\n", step.name)
	}

	fmt.Println("All environment verification checks completed successfully.")
	return nil
}

// Summary provides a high-level health report of the development environment.
// It performs quick checks on each subsystem without detailed validation output.
func (Verify) Summary() error {
	fmt.Println("Factorio-Hardened Environment Summary")

	systems := []struct {
		name string
		fn   func() error
	}{
		{"System", func() error { return (System{}).Verify() }},
		{"Go", func() error { return (Go{}).Verify() }},
		{"Docker", func() error { return (Docker{}).Verify() }},
		{"Lint", func() error { return (Lint{}).Verify() }},
		{"Trivy", func() error { return (Trivy{}).Verify() }},
		{"GitHub", func() error { return (Github{}).Verify() }},
	}

	okSymbol := "Healthy"
	failSymbol := "Unhealthy"

	allPassed := true
	for _, s := range systems {
		if err := s.fn(); err != nil {
			fmt.Printf("%-20s %s %v\n", s.name, failSymbol, err)
			allPassed = false
		} else {
			fmt.Printf("%-20s %s\n", s.name, okSymbol)
		}
	}

	fmt.Println()
	if allPassed {
		fmt.Println("All systems are healthy and ready for builds.")
		return nil
	}

	return fmt.Errorf("one or more systems reported issues — run 'mage verify:all' for detailed output")
}
