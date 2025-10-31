//go:build mage

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/magefile/mage/mg"
)

// SrcDigest defines the namespace for managing the upstream Factorio source image digests.
// It handles pulling, comparing, and syncing image digests across multiple architectures
// to ensure reproducible builds before the hardened image is created.
type SrcDigest mg.Namespace

// Constants and configuration defaults.
const (
	upstreamImage = "factoriotools/factorio"
	// Change this tag whenever you want to baseline a new version.
	factorioTag = "2.0.69"
)

// isValidArch returns true if the provided architecture should be included
// in the multi-arch baseline. This enforces an immutable architecture policy.
func isValidArch(arch string) bool {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "amd64", "arm64":
		return true
	default:
		return false
	}
}

// MultiArchMetadata represents stored metadata for all architectures
// and the top-level manifest list digest.
type MultiArchMetadata struct {
	Repository   string            `json:"repository"`
	Tag          string            `json:"tag"`
	ManifestList string            `json:"manifest_list"` // top-level digest (multi-arch index)
	Digests      map[string]string `json:"digests"`       // key = arch, value = digest
	UpdatedAt    time.Time         `json:"updated_at"`
}

// getLocalArch returns the current GOARCH (normalized for Docker naming).
func getLocalArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	case "amd64":
		return "amd64"
	default:
		return runtime.GOARCH
	}
}

// ensureDockerAvailable verifies that Docker is installed and accessible.
func ensureDockerAvailable() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH: %v", err)
	}
	return nil
}

// getLocalManifestListDigest retrieves the multi-arch manifest list digest.
func getLocalManifestListDigest(image string) (string, error) {
	cmd := exec.Command("docker", "inspect", "--format", "{{index .RepoDigests 0}}", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to inspect manifest list digest: %v\n%s", err, string(output))
	}
	parts := strings.SplitN(strings.TrimSpace(string(output)), "@", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected digest format: %s", string(output))
	}
	return parts[1], nil // only the sha256:... part
}

// getLocalArchDigest retrieves the architecture-specific digest for the current platform.
func getLocalArchDigest(image string) (string, error) {
	cmd := exec.Command("docker", "manifest", "inspect", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to inspect manifest: %v\n%s", err, string(output))
	}

	var manifest struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				Architecture string `json:"architecture"`
				OS           string `json:"os"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(output, &manifest); err != nil {
		return "", fmt.Errorf("failed to parse manifest JSON: %v", err)
	}

	localArch := getLocalArch()
	for _, m := range manifest.Manifests {
		if strings.EqualFold(m.Platform.Architecture, localArch) {
			return m.Digest, nil
		}
	}
	return "", fmt.Errorf("no digest found for architecture %s", localArch)
}

// All runs the full source digest maintenance workflow.
func (SrcDigest) All() error {
	fmt.Println("Running SrcDigest:All workflow...")

	if err := (SrcDigest{}.Show()); err != nil {
		fmt.Printf("Show step failed: %v\n", err)
	}

	err := (SrcDigest{}.Compare())

	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no baseline") {
		fmt.Println("Baseline missing. Performing initial sync...")
		if syncErr := (SrcDigest{}.Sync()); syncErr != nil {
			return fmt.Errorf("initial sync failed: %v", syncErr)
		}
		fmt.Println("Baseline initialized successfully.")
		return nil
	}

	if err == nil {
		fmt.Println("Baseline is already up to date. No sync required.")
		return nil
	}

	fmt.Printf("Change detected: %v\n", err)
	fmt.Println("Synchronizing to target version...")
	if syncErr := (SrcDigest{}.Sync()); syncErr != nil {
		return fmt.Errorf("sync failed: %v", syncErr)
	}

	fmt.Println("SrcDigest:All completed successfully.")
	return nil
}

// Show prints the current digest entry for the local architecture.
func (SrcDigest) Show() error {
	localArch := getLocalArch()
	fmt.Printf("Fetching Factorio digests for architecture: %s\n", localArch)

	data, err := os.ReadFile(baselineFile)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("No baseline found.")
		return nil
	}

	var meta MultiArchMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("failed to parse baseline file: %v", err)
	}

	fmt.Printf("Manifest list digest: %s\n", meta.ManifestList)
	if digest, ok := meta.Digests[localArch]; ok {
		fmt.Printf("Stored digest for %s: %s\n", localArch, digest)
	} else {
		fmt.Printf("No digest found for %s in baseline.\n", localArch)
	}
	return nil
}

// Compare checks whether the current manifest list or architecture digest differs from baseline.
func (SrcDigest) Compare() error {
	localArch := getLocalArch()
	fullImage := fmt.Sprintf("%s:%s", upstreamImage, factorioTag)
	fmt.Printf("Comparing digests for %s (%s)\n", localArch, fullImage)

	if err := ensureDockerAvailable(); err != nil {
		return err
	}

	currentList, err := getLocalManifestListDigest(fullImage)
	if err != nil {
		return err
	}
	currentArch, err := getLocalArchDigest(fullImage)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(baselineFile)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no baseline file found")
	}
	if err != nil {
		return fmt.Errorf("cannot read baseline file: %v", err)
	}

	var meta MultiArchMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("failed to parse baseline: %v", err)
	}

	if meta.ManifestList != currentList {
		fmt.Printf("Manifest list updated.\nOld: %s\nNew: %s\n", meta.ManifestList, currentList)
		return fmt.Errorf("manifest list digest changed")
	}

	if oldArchDigest, ok := meta.Digests[localArch]; ok {
		if oldArchDigest != currentArch {
			fmt.Printf("Architecture digest updated for %s.\nOld: %s\nNew: %s\n", localArch, oldArchDigest, currentArch)
			return fmt.Errorf("digest changed for %s", localArch)
		}
	} else {
		fmt.Printf("No digest found for %s in baseline.\nCurrent digest: %s\n", localArch, currentArch)
		return fmt.Errorf("missing digest for %s", localArch)
	}

	fmt.Println("Baseline is up to date.")
	return nil
}

// Sync pulls the Factorio image for the configured tag and updates (or creates) baseline.yaml.
func (SrcDigest) Sync() error {
	_ = os.MkdirAll("builddata", 0755)
	localArch := getLocalArch()
	fullImage := fmt.Sprintf("%s:%s", upstreamImage, factorioTag)

	fmt.Printf("Syncing Factorio image %s for architecture: %s\n", fullImage, localArch)

	if err := ensureDockerAvailable(); err != nil {
		return err
	}

	cmd := exec.Command("docker", "pull", fullImage)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull upstream image: %v", err)
	}

	listDigest, err := getLocalManifestListDigest(fullImage)
	if err != nil {
		return err
	}

	cmd = exec.Command("docker", "manifest", "inspect", fullImage)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to inspect manifest: %v\n%s", err, string(output))
	}

	var manifest struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				Architecture string `json:"architecture"`
				OS           string `json:"os"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(output, &manifest); err != nil {
		return fmt.Errorf("failed to parse manifest: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	meta := MultiArchMetadata{
		Repository:   upstreamImage,
		Tag:          factorioTag,
		ManifestList: listDigest,
		Digests:      make(map[string]string),
		UpdatedAt:    now,
	}
	if data, err := os.ReadFile(baselineFile); err == nil {
		var existing MultiArchMetadata
		if err := json.Unmarshal(data, &existing); err == nil {
			for k, v := range existing.Digests {
				meta.Digests[k] = v
			}
		}
	}

	for _, m := range manifest.Manifests {
		arch := strings.TrimSpace(strings.ToLower(m.Platform.Architecture))
		if !isValidArch(arch) {
			fmt.Printf("Skipping unsupported arch %q (%s)\n", arch, m.Digest)
			continue
		}
		meta.Digests[arch] = m.Digest
	}

	file, err := os.Create(baselineFile)
	if err != nil {
		return fmt.Errorf("failed to write baseline file: %v", err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(meta); err != nil {
		return fmt.Errorf("failed to encode baseline metadata: %v", err)
	}

	fmt.Printf("Baseline updated for Factorio %s with manifest list %s and %d architectures.\n",
		factorioTag, meta.ManifestList, len(meta.Digests))
	for arch, digest := range meta.Digests {
		fmt.Printf("  %s: %s\n", arch, digest)
	}
	return nil
}
