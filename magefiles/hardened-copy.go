//go:build mage

package main

/*import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	/*if err := (Hardened{}.Verify()); err != nil {
		return fmt.Errorf("verification stage failed: %v", err)
	}

	fmt.Printf("Hardened image build and verification completed in %s\n", time.Since(start).Round(time.Second))
	return nil
}

// Prepare reads the pinned digest for the current architecture from baseline.yaml,
// replaces the FROM line in hardened.Dockerfile with a dynamic digest reference
// (via ${BASE_IMAGE_DIGEST}), ensures an init-config stage exists,
// and writes a reproducible Dockerfile ready for mage Hardened:Build.
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
	if strings.Contains(meta.Repository, ":") {
		meta.Repository = strings.Split(meta.Repository, ":")[0]
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
			lines[i] = fmt.Sprintf(
				"# Base digest pinned for reproducibility: %s\nARG BASE_IMAGE_DIGEST=%s\nFROM %s@${BASE_IMAGE_DIGEST} AS base",
				digest, digest, meta.Repository,
			)
			// Remove any redundant ARG that might precede this line
			if i > 0 && strings.Contains(lines[i-1], "ARG BASE_IMAGE_DIGEST") {
				lines[i-1] = ""
			}
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

	// Record metadata for downstream tasks
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

// Build performs a production-grade multi-arch build and pushes it to GHCR.
// It runs a Trivy scan on the amd64 variant before pushing the final manifest list.
func (Hardened) Build() error {
	var meta BuildMetadata
	data, err := os.ReadFile("buildmeta.json")
	if err != nil {
		return fmt.Errorf("failed to read buildmeta.json: %v", err)
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("failed to parse buildmeta.json: %v", err)
	}

	// --- Detect Factorio version from upstream base image ---
	version, err := getFactorioVersion(baseRef)
	if err != nil {
		return fmt.Errorf("could not determine Factorio version: %w", err)
	}

	// Allow manual override for CI testing (optional)
	if envVer := os.Getenv("VERSION"); envVer != "" {
		version = envVer
	}

	tag := fmt.Sprintf("%s:%s", imageRepo, version)

	fmt.Println("🏗️  Building Factorio-Hardened image (multi-arch, CI mode)...")

	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %v", err)
	}
	if filepath.Base(repoRoot) == "magefiles" {
		repoRoot = filepath.Dir(repoRoot)
	}

	dockerfilePath := filepath.Join(repoRoot, outputDockerfile)
	if _, err := os.Stat(dockerfilePath); err != nil {
		dockerfilePath = filepath.Join(repoRoot, hardenedDockerfile)
	}

	// --- Step 1: Build amd64 variant locally for scanning ---
	fmt.Println("🧱 Building amd64 variant for Trivy scan...")
	amd64Tag := tag + "-amd64"
	buildCmd := exec.Command(
		"docker", "buildx", "build",
		"--platform", "linux/amd64",
		"--tag", amd64Tag,
		"--build-arg", fmt.Sprintf("BASE_IMAGE_DIGEST=%s", meta.BaseDigest),
		"--load",
		"--file", dockerfilePath,
		repoRoot,
	)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("amd64 build failed: %v", err)
	}

	fmt.Println("🔍 Running Trivy vulnerability scan (HIGH/CRITICAL)...")
	scan := exec.Command("trivy", "image", "--severity", "HIGH,CRITICAL", "--exit-code", "1", amd64Tag)
	scan.Stdout = os.Stdout
	scan.Stderr = os.Stderr
	if err := scan.Run(); err != nil {
		return fmt.Errorf("Trivy scan failed: %v", err)
	}
	fmt.Println("✅ Trivy scan passed.")

	// --- Step 2: Multi-arch build and push ---
	fmt.Println("🚀 Building and pushing multi-arch image (linux/amd64, linux/arm64)...")
	pushCmd := exec.Command(
		"docker", "buildx", "build",
		"--no-cache",
		"--platform", "linux/amd64,linux/arm64",
		"--tag", tag,
		"--build-arg", fmt.Sprintf("BASE_IMAGE_DIGEST=%s", meta.BaseDigest),
		"--push",
		"--file", dockerfilePath,
		repoRoot,
	)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("multi-arch build/push failed: %v", err)
	}

	fmt.Printf("✅ Multi-arch image pushed successfully: %s\n", tag)
	return nil
}

// testBuild builds the hardened image locally and runs a temporary server container
// to verify that it launches successfully. It stops and removes the container afterward.
func testBuild() error {
	// --- Load metadata from buildmeta.json ---
	var meta BuildMetadata
	data, err := os.ReadFile("buildmeta.json")
	if err != nil {
		return fmt.Errorf("failed to read buildmeta.json: %v", err)
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("failed to parse buildmeta.json: %v", err)
	}

	// Use digest directly from metadata
	digest := meta.BaseDigest
	if digest == "" {
		return fmt.Errorf("no base digest found in buildmeta.json")
	}

	// --- Resolve version and tag ---
	version := os.Getenv("VERSION")
	if version == "" {
		version = meta.Version
		if version == "" {
			version = "dev"
		}
	}
	tag := fmt.Sprintf("%s:%s", imageRepo, version)
	containerName := "factorio-hardened-test"

	fmt.Println("🛠️  Building Factorio-Hardened image (local dev, single-arch, no push)...")

	// --- Build hardened image ---
	build := exec.Command(
		"docker", "buildx", "build",
		"--file", outputDockerfile,
		"--platform", "linux/amd64",
		"--tag", tag,
		"--load",
		"--debug",
		"--no-cache",
		"--build-arg", fmt.Sprintf("BASE_IMAGE_DIGEST=%s", digest),
		".",
	)
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("failed to build image: %v", err)
	}

	fmt.Printf("✅ Local test build complete: %s\n", tag)

	// --- Cleanup any old test container ---
	_ = exec.Command("docker", "rm", "-f", containerName).Run()

	// --- Prepare temp directory ---
	tmpDir := filepath.Join(os.TempDir(), "factorio-testdata")
	_ = os.MkdirAll(tmpDir, 0755)

	fmt.Println("🚀 Running test container for 10 seconds...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	run := exec.CommandContext(
		ctx,
		"docker", "run",
		"--rm",
		"--name", containerName,
		"--read-only",
		"--mount", "type=bind,source=./pvc/factorio,destination=/factorio",
		"--user", "1000:1000",
		"-p", "34197:34197/udp",
		"-p", "27015:27015/tcp",
		tag,
		"--start-server", "/factorio/saves/world-live.zip",
	)
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr

	if err := run.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Println("🕒 Test run timed out — container appears healthy.")
		} else {
			return fmt.Errorf("container test run failed: %v", err)
		}
	} else {
		fmt.Println("✅ Container launched and exited cleanly.")
	}

	fmt.Println("🧹 Cleaning up temporary files...")
	_ = os.RemoveAll(tmpDir)

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
		"--entrypoint", "/opt/factorio/bin/x64/factorio",
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
*/
