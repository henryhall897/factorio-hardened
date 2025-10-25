//go:build mage

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/magefile/mage/mg"
)

// Docker namespace groups all Docker-related tasks.
type Docker mg.Namespace

// Verify checks that Docker is installed and the daemon is reachable.
func (Docker) Verify() error {
	fmt.Println("Verifying Docker installation...")
	if err := verifyDockerInstallation(); err != nil {
		return err
	}
	fmt.Println("Docker installation verified successfully.")
	return nil
}

// Deps ensures Docker is installed and authenticated for GHCR.
func (Docker) Deps() error {
	fmt.Println("Ensuring Docker dependencies...")

	// 1. Ensure Docker itself is installed
	if err := (Docker{}).Verify(); err != nil {
		fmt.Println("Docker not detected or unhealthy. Installing via apt...")
		if err := installDocker(); err != nil {
			return fmt.Errorf("failed to install Docker: %w", err)
		}

		fmt.Println("Re-verifying Docker installation...")
		if err := (Docker{}).Verify(); err != nil {
			return fmt.Errorf("Docker installation did not verify successfully: %w", err)
		}
	}

	// 2. Verify Docker GHCR authentication
	fmt.Println("Verifying Docker authentication for GHCR...")
	if err := (Docker{}).VerifyAuth(); err != nil {
		fmt.Println("Docker GHCR authentication is missing or invalid. Configuring credentials...")
		if err := ensureDockerAuth(); err != nil {
			return fmt.Errorf("failed to configure Docker authentication: %w", err)
		}
	}

	fmt.Println("Docker successfully installed and authenticated.")
	return nil
}

// verifyDockerInstallation checks that the Docker CLI and daemon are functional.
func verifyDockerInstallation() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker binary not found in PATH")
	}

	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker daemon unreachable or not running: %w", err)
	}

	fmt.Printf("Docker is installed and running (version: %s)\n", strings.TrimSpace(string(out)))
	return nil
}

// VerifyAuth validates Docker authentication for GHCR (GitHub Container Registry).
// It ensures that:
//  1. The Docker configuration file exists and is valid JSON
//  2. No global or per-registry credential helpers (e.g., "pass") are active
//  3. The GHCR registry contains a valid base64-encoded authentication entry
func (Docker) VerifyAuth() error {
	const configPath = ".docker/config.json"
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("unable to determine home directory: %w", err)
	}
	path := fmt.Sprintf("%s/%s", homeDir, configPath)

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("docker config not found at %s: %w", path, err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("invalid Docker config JSON: %w", err)
	}

	if v, ok := cfg["credsStore"]; ok && v != "" {
		return fmt.Errorf("global credsStore (%v) is active — remove this for reproducible builds", v)
	}

	if ch, ok := cfg["credHelpers"]; ok {
		if helpers, ok := ch.(map[string]interface{}); ok {
			if gh, ok := helpers["ghcr.io"]; ok && gh != "" {
				return fmt.Errorf("per-registry helper for ghcr.io (%v) is active — should be an empty string", gh)
			}
		}
	}

	auths, _ := cfg["auths"].(map[string]interface{})
	if auths == nil {
		return fmt.Errorf("no 'auths' section found in Docker configuration: %s", path)
	}

	ghcrEntry, ok := auths["ghcr.io"].(map[string]interface{})
	if !ok || ghcrEntry["auth"] == nil {
		return fmt.Errorf("missing authentication for ghcr.io — run: echo $GHCR_TOKEN | docker login ghcr.io -u henryhall897 --password-stdin")
	}

	authB64, _ := ghcrEntry["auth"].(string)
	decoded, err := base64.StdEncoding.DecodeString(authB64)
	if err != nil {
		return fmt.Errorf("malformed base64 string in 'auth' field: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || parts[0] == "" || len(parts[1]) < 10 {
		return fmt.Errorf("ghcr.io credentials appear invalid or incomplete; please re-authenticate")
	}

	fmt.Println("Docker GHCR authentication verification complete.")
	fmt.Printf("  User: %s\n", parts[0])
	fmt.Println("  Credential helper: disabled (expected configuration)")
	fmt.Println("  Note: GitHub PATs for GHCR typically expire every 90 days. Renew before expiration to avoid disruptions.")

	return nil
}

// ensureDockerAuth ensures that GHCR authentication exists in the Docker configuration file.
func ensureDockerAuth() error {
	configPath := fmt.Sprintf("%s/.docker/config.json", os.Getenv("HOME"))

	// Ensure Docker config exists
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		fmt.Println("Docker configuration not found. Creating ~/.docker/config.json ...")
		if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
			return fmt.Errorf("failed to create Docker config directory: %w", err)
		}
		data = []byte(`{"auths":{}}`)
		if err := os.WriteFile(configPath, data, 0600); err != nil {
			return fmt.Errorf("failed to create Docker config: %w", err)
		}
	}

	var cfg map[string]interface{}
	_ = json.Unmarshal(data, &cfg)

	auths, _ := cfg["auths"].(map[string]interface{})
	if auths == nil || auths["ghcr.io"] == nil {
		fmt.Println("\nGHCR credentials not found in Docker config.")
		fmt.Println("To push or pull images, you need a GitHub Personal Access Token (classic) with `read:packages` and `write:packages` scopes.")
		fmt.Println("1. Visit: https://github.com/settings/tokens")
		fmt.Println("2. Generate a new token with those scopes.")

		fmt.Print("Paste your new token here: ")
		var token string
		fmt.Scanln(&token)

		if token == "" {
			return fmt.Errorf("no token provided; cannot configure GHCR access")
		}

		auth := base64.StdEncoding.EncodeToString([]byte("henryhall897:" + token))
		if auths == nil {
			auths = make(map[string]interface{})
			cfg["auths"] = auths
		}
		auths["ghcr.io"] = map[string]interface{}{"auth": auth}

		updated, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(configPath, updated, 0600); err != nil {
			return fmt.Errorf("failed to write Docker config: %w", err)
		}

		fmt.Println("\nDocker GHCR authentication configured successfully.")
	} else {
		fmt.Println("Docker GHCR credentials already exist.")
	}

	return nil
}

// installDocker installs Docker using the system package manager.
func installDocker() error {
	cmds := [][]string{
		{"sudo", "apt-get", "update", "-y"},
		{"sudo", "apt-get", "install", "-y", "docker.io"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed running %v: %w", args, err)
		}
	}

	return nil
}
