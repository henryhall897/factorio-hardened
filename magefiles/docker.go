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

// Deps ensures Docker, Buildx, and GHCR authentication are configured and functional.
func (Docker) Deps() error {
	fmt.Println("Ensuring Docker dependencies...")

	// 1. Ensure Docker itself is installed
	if err := (Docker{}).Verify(); err != nil {
		fmt.Println("Docker not detected or unhealthy. Installing official Docker Engine...")
		if err := installDocker(); err != nil {
			return fmt.Errorf("failed to install Docker: %w", err)
		}

		fmt.Println("Re-verifying Docker installation...")
		if err := (Docker{}).Verify(); err != nil {
			return fmt.Errorf("Docker installation did not verify successfully: %w", err)
		}
	}

	// 2. Ensure Buildx is available and configured
	fmt.Println("Verifying Docker Buildx availability...")
	if err := verifyBuildx(); err != nil {
		fmt.Println("Docker Buildx not detected or misconfigured. Attempting to set up...")
		if err := ensureBuildx(); err != nil {
			return fmt.Errorf("failed to configure Docker Buildx: %w", err)
		}
	}

	// 3. Verify Docker GHCR authentication
	fmt.Println("Verifying Docker authentication for GHCR...")
	if err := (Docker{}).VerifyAuth(); err != nil {
		fmt.Println("Docker GHCR authentication is missing or invalid. Configuring credentials...")
		if err := ensureDockerAuth(); err != nil {
			return fmt.Errorf("failed to configure Docker authentication: %w", err)
		}
	}

	fmt.Println("Docker successfully installed, Buildx configured, and GHCR authentication verified.")
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

	version := strings.TrimSpace(string(out))
	fmt.Printf("Docker is installed and running (version: %s)\n", version)
	return nil
}

// verifyBuildx checks whether Docker Buildx is installed and properly configured
// with a containerized builder (driver = docker-container).
func verifyBuildx() error {
	// Check plugin availability
	if err := exec.Command("docker", "buildx", "version").Run(); err != nil {
		return fmt.Errorf("docker buildx not installed: %w", err)
	}

	// Inspect active builder
	cmd := exec.Command("docker", "buildx", "inspect")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to inspect buildx builder: %w", err)
	}

	output := strings.ReplaceAll(string(out), "\r", "")
	output = strings.ReplaceAll(output, "\t", " ")
	output = strings.TrimSpace(output)

	// Normalize spaces and check driver
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Driver:") && strings.Contains(line, "docker-container") {
			fmt.Println("Docker Buildx is available and correctly configured.")
			return nil
		}
	}

	// If we got here, it's installed but wrong driver
	fmt.Println("Docker Buildx found, but using docker driver instead of docker-container.")
	return fmt.Errorf("buildx not using docker-container driver (reconfiguration required)")
}

// ensureBuildx ensures that Docker Buildx is installed and configured.
// Delegates actual setup to scripts/buildx.sh for reproducible host-level behavior.
func ensureBuildx() error {
	fmt.Println("Configuring Docker Buildx...")

	// Determine script path relative to project root
	scriptPath := filepath.Join("scripts", "buildx.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("missing helper script: %s", scriptPath)
	}

	// Run the setup script
	cmd := exec.Command("bash", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "HOME="+os.Getenv("HOME"))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to configure Buildx via %s: %w", scriptPath, err)
	}

	// Verify final Buildx status
	verify := exec.Command("docker", "buildx", "inspect")
	verify.Env = append(os.Environ(), "HOME="+os.Getenv("HOME"))
	out, err := verify.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to verify Buildx configuration: %w", err)
	}

	fmt.Println(string(out))
	fmt.Println("Docker Buildx successfully configured for multi-platform builds.")
	return nil
}

// VerifyAuth validates Docker authentication for GHCR (GitHub Container Registry).
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

// installDocker ensures the official Docker Engine (with Buildx and Compose) is installed.
// If the system is using Ubuntu's legacy `docker.io` package, it will be replaced.
func installDocker() error {
	fmt.Println("Installing official Docker Engine and Buildx plugin...")

	// Detect whether the current docker binary is the Ubuntu version
	cmd := exec.Command("docker", "--version")
	out, err := cmd.CombinedOutput()
	if err == nil && strings.Contains(string(out), "Ubuntu") {
		fmt.Println("Detected Ubuntu-provided Docker package (docker.io). Removing it before installing official Docker...")
		remove := exec.Command("sudo", "apt-get", "remove", "-y", "docker.io", "docker-doc", "podman-docker", "containerd", "runc")
		remove.Stdout, remove.Stderr = os.Stdout, os.Stderr
		if err := remove.Run(); err != nil {
			return fmt.Errorf("failed to remove legacy Docker packages: %w", err)
		}
	}

	cmds := [][]string{
		{"sudo", "apt-get", "update", "-y"},
		{"sudo", "apt-get", "install", "-y", "ca-certificates", "curl", "gnupg"},
		{"sudo", "install", "-m", "0755", "-d", "/etc/apt/keyrings"},
		{"bash", "-c", "curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg"},
		{"bash", "-c", "echo \"deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable\" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null"},
		{"sudo", "apt-get", "update", "-y"},
		{"sudo", "apt-get", "install", "-y", "docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin", "qemu-user-static"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed running %v: %w", args, err)
		}
	}

	cmd = exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker installation failed or daemon not reachable: %w", err)
	}

	fmt.Println("Official Docker Engine and Buildx successfully installed.")
	return nil
}
