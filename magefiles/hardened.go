//go:build mage

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/magefile/mage/mg"
)

// Hardened defines the namespace for building and verifying
// the derived Factorio-Hardened Docker image. It relies on the
// baseline.yaml file produced by SrcDigest:Sync.
type Hardened mg.Namespace

// Constants and configuration defaults.
const (
	hardenedDockerfile = "docker/hardened.Dockerfile"
	outputDockerfile   = "docker/hardened.pinned.Dockerfile"
	imageRepo          = "ghcr.io/henryhall897/factorio-hardened"
)

// BuildMetadata holds contextual info for reproducible image builds.
type BuildMetadata struct {
	BaseDigest string
	Arch       string
	Version    string
	Tag        string
	BuiltAt    time.Time
}

// All runs the complete hardened image pipeline: prepare → build → verify → promote → clean.
func (Hardened) All() error {
	start := time.Now()
	fmt.Println("Running full hardened image pipeline...")

	if err := (Hardened{}.Prepare()); err != nil {
		return fmt.Errorf("prepare stage failed: %v", err)
	}
	if err := (Hardened{}.Build()); err != nil {
		return fmt.Errorf("build stage failed: %v", err)
	}
	if err := (Hardened{}.Verify()); err != nil {
		return fmt.Errorf("verification stage failed: %v", err)
	}
	if err := (Hardened{}.Promote()); err != nil {
		return fmt.Errorf("promotion stage failed: %v", err)
	}
	if err := (Hardened{}.Clean()); err != nil {
		fmt.Printf("cleanup stage warning: %v\n", err)
	}

	fmt.Printf("Hardened image pipeline completed successfully in %s\n", time.Since(start).Round(time.Second))
	return nil
}

// Test builds and verifies the hardened image without pushing.
// Use this for local testing or pre-promotion validation.
func (Hardened) Test() error {
	start := time.Now()
	fmt.Println("Running hardened image build and verification...")

	if err := (Hardened{}.Prepare()); err != nil {
		return fmt.Errorf("prepare stage failed: %v", err)
	}
	if err := (testBuild()); err != nil {
		return fmt.Errorf("build stage failed: %v", err)
	}
	if err := (Hardened{}.Verify()); err != nil {
		return fmt.Errorf("verification stage failed: %v", err)
	}

	fmt.Printf("Hardened image build and verification completed in %s\n", time.Since(start).Round(time.Second))
	return nil
}

// Promote pushes the most recently verified image to GHCR.
func (Hardened) Promote() error {
	fmt.Println("Promoting verified image to GHCR...")

	version := os.Getenv("VERSION")
	if version == "" {
		version = "dev"
	}
	tag := fmt.Sprintf("%s:%s", imageRepo, version)

	cmd := exec.Command("docker", "push", tag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("promotion failed: %v", err)
	}

	fmt.Println("Image promoted successfully to GHCR.")
	return nil
}

// Prepare reads the pinned digest for the current architecture from baseline.yaml,
// replaces the FROM line in hardened.Dockerfile with a pinned digest reference,
// ensures an init-config stage exists, and writes a reproducible Dockerfile.
func (Hardened) Prepare() error {
	fmt.Println("Preparing hardened Dockerfile...")

	data, err := os.ReadFile(baselineFile)
	if err != nil {
		return fmt.Errorf("failed to read baseline file: %v", err)
	}

	var meta struct {
		Repository string            `json:"repository"`
		Digests    map[string]string `json:"digests"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("failed to parse baseline: %v", err)
	}

	arch := runtime.GOARCH
	digest, ok := meta.Digests[arch]
	if !ok {
		return fmt.Errorf("no digest found for architecture %s", arch)
	}

	baseRef := fmt.Sprintf("%s@%s", meta.Repository, digest)
	version, verErr := getFactorioVersion(baseRef)
	if verErr != nil {
		fmt.Printf("Warning: could not detect Factorio version automatically: %v\n", verErr)
		version = "unknown"
	}

	content, err := os.ReadFile(hardenedDockerfile)
	if err != nil {
		return fmt.Errorf("failed to read template Dockerfile: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "FROM factoriotools/factorio") {
			lines[i] = fmt.Sprintf("FROM %s@%s AS base", meta.Repository, digest)
		}
	}
	newContent := strings.Join(lines, "\n")

	// Ensure init-config stage exists
	if !strings.Contains(newContent, "AS init-config") {
		initStage := `
# Init stage: prepares default Factorio configuration files
FROM busybox:1.36 AS init-config
WORKDIR /defaults/config
RUN set -eux; \
    mkdir -p /defaults/config && \
    echo "[path]" > /defaults/config/config.ini && \
    echo "read-data=/opt/factorio/data" >> /defaults/config/config.ini && \
    echo "write-data=/factorio" >> /defaults/config/config.ini
`
		insertPoint := strings.Index(newContent, "COPY --from=init-config")
		if insertPoint > 0 {
			newContent = newContent[:insertPoint] + initStage + "\n" + newContent[insertPoint:]
			fmt.Println("Inserted missing init-config stage into Dockerfile.")
		} else {
			fmt.Println("Warning: could not locate insertion point for init-config stage.")
		}
	}

	if err := os.WriteFile(outputDockerfile, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write pinned Dockerfile: %v", err)
	}

	// Record metadata for downstream tasks (promote, verify)
	buildMeta := BuildMetadata{
		BaseDigest: digest,
		Arch:       arch,
		Version:    version,
		Tag:        fmt.Sprintf("%s:%s", imageRepo, version),
		BuiltAt:    time.Now(),
	}
	metaBytes, _ := json.MarshalIndent(buildMeta, "", "  ")
	_ = os.WriteFile("buildmeta.json", metaBytes, 0644)

	fmt.Printf("Pinned Dockerfile created for %s → %s (Factorio %s)\n", arch, digest, version)
	return nil
}

// Build constructs the hardened image using the pinned Dockerfile.
// It supports flexible build modes (local load, push, or cached multi-arch).
func (Hardened) Build() error {
	version := os.Getenv("VERSION")
	if version == "" {
		version = "dev"
	}
	tag := fmt.Sprintf("%s:%s", imageRepo, version)

	fmt.Println("Building Factorio-Hardened image (multi-arch, cached)...")

	cmd := exec.Command(
		"docker", "buildx", "build",
		"--file", outputDockerfile,
		"--platform", "linux/amd64,linux/arm64",
		"--tag", tag,
		".",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build image: %v", err)
	}

	fmt.Printf("Build complete: %s\n", tag)
	return nil
}

// testBuild builds the hardened image for local testing using --load.
func testBuild() error {
	version := os.Getenv("VERSION")
	if version == "" {
		version = "dev"
	}
	tag := fmt.Sprintf("%s:%s", imageRepo, version)

	fmt.Println("Building Factorio-Hardened image (local dev, single-arch)...")

	cmd := exec.Command(
		"docker", "buildx", "build",
		"--file", outputDockerfile,
		"--platform", "linux/amd64",
		"--tag", tag,
		"--load",
		".",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build image: %v", err)
	}

	fmt.Printf("Local test build complete: %s\n", tag)
	return nil
}

// Verify orchestrates all post-build validation checks for hardened images.
func (Hardened) Verify() error {
	fmt.Println("Verifying hardened image...")

	version := os.Getenv("VERSION")
	if version == "" {
		version = "dev"
	}
	tag := fmt.Sprintf("%s:%s", imageRepo, version)

	if err := checkNonRoot(tag); err != nil {
		return err
	}
	if err := trivyScan(tag); err != nil {
		return err
	}
	if err := checkReadOnlyRuntime(tag); err != nil {
		return err
	}
	if strings.ToLower(os.Getenv("KYVERNO_TEST")) == "true" {
		if err := verifyKyvernoCompliance(); err != nil {
			return err
		}
	}

	fmt.Println("Verification complete — all checks passed.")
	return nil
}

// checkNonRoot ensures the image does not run as UID 0.
func checkNonRoot(tag string) error {
	fmt.Println("Checking non-root user...")
	cmd := exec.Command("docker", "inspect", "--format", "{{.Config.User}}", tag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to inspect image user: %v", err)
	}
	user := strings.TrimSpace(string(out))
	if user == "" || user == "root" || user == "0" {
		return fmt.Errorf("image runs as root — must be non-root user")
	}
	fmt.Printf("User check passed: %s\n", user)
	return nil
}

// trivyScan runs a vulnerability scan using Trivy.
func trivyScan(tag string) error {
	reportMode := strings.ToLower(os.Getenv("REPORT"))
	if reportMode == "true" {
		fmt.Println("Generating full Trivy vulnerability report...")
		return (Trivy{}.Report(tag))
	}
	fmt.Println("Running Trivy quick vulnerability scan...")
	return (Trivy{}.ScanImage(tag))
}

// checkReadOnlyRuntime validates that the image runs successfully under a read-only root filesystem.
func checkReadOnlyRuntime(tag string) error {
	fmt.Println("Validating read-only runtime compatibility...")
	cmd := exec.Command(
		"docker", "run", "--rm", "--read-only",
		"--tmpfs", "/tmp:rw",
		"-v", "factorio-config:/factorio/config",
		"-v", "factorio-mods:/factorio/mods",
		"-v", "factorio-saves:/factorio/saves",
		"-v", "factorio-scenarios:/factorio/scenarios",
		"-v", "factorio-output:/factorio/script-output",
		tag, "--version",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("container failed to start in read-only mode: %v", err)
	}
	fmt.Println("Read-only runtime check passed.")
	return nil
}

// verifyKyvernoCompliance performs a dry-run test to confirm that the image
// passes the Kyverno restricted policy on the active K3s cluster.
func verifyKyvernoCompliance() error {
	fmt.Println("Validating Kyverno restricted policy compliance...")

	testManifest := "test/pod-readonly.yaml"
	cmd := exec.Command("kubectl", "apply", "-f", testManifest, "--dry-run=server")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Kyverno restricted policy check failed: %v", err)
	}

	fmt.Println("Kyverno compliance check passed.")
	return nil
}

// getFactorioVersion extracts the Factorio version string (e.g. "2.0.72")
// from the upstream base image using `docker run --version`.
func getFactorioVersion(baseRef string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx,
		"docker", "run", "--rm",
		"--entrypoint", "/opt/factorio/bin/x64/factorio",
		baseRef, "--version",
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timeout while getting Factorio version")
	}
	if err != nil {
		return "", fmt.Errorf("failed to extract Factorio version from %s: %v\nOutput:\n%s", baseRef, err, string(out))
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Version:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return strings.TrimSpace(fields[1]), nil
			}
		}
	}
	return "", fmt.Errorf("Factorio version not found in output: %s", string(out))
}

// Clean removes generated Dockerfiles and temporary build artifacts.
func (Hardened) Clean() error {
	files := []string{outputDockerfile}
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			if err := os.Remove(f); err != nil {
				fmt.Printf("Failed to remove %s: %v\n", f, err)
			} else {
				fmt.Printf("Removed %s\n", f)
			}
		}
	}
	return nil
}
