//go:build mage

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/magefile/mage/mg"
)

const (
	GithubRepo        = "henryhall897/factorio-hardened"
	GithubHTTPTimeout = 10 * time.Second
)

// Github namespace handles GitHub-related tasks such as GHCR token validation and API access checks.
type Github mg.Namespace

// Verify checks that a valid GitHub Personal Access Token (PAT) is available
// and that it has not expired.
func (Github) Verify() error {
	fmt.Println("Verifying GitHub authentication token...")

	token, err := loadGhcrToken()
	if err != nil {
		return fmt.Errorf("failed to load GHCR token: %w", err)
	}

	if err := verifyGhcrToken(token); err != nil {
		return err
	}

	fmt.Println("GitHub token verification completed successfully.")
	return nil
}

// Deps ensures that a valid GitHub PAT exists and is usable for GHCR operations.
func (Github) Deps() error {
	fmt.Println("Ensuring GitHub authentication dependencies...")

	// Ensure Docker credentials exist before checking GitHub.
	if err := (Docker{}).Deps(); err != nil {
		return fmt.Errorf("docker authentication prerequisite failed: %w", err)
	}

	// Verify token validity.
	if err := (Github{}).Verify(); err != nil {
		fmt.Println("GitHub token invalid or missing. Attempting reconfiguration...")
		if err := (Docker{}).VerifyAuth(); err != nil {
			if err := ensureDockerAuth(); err != nil {
				return fmt.Errorf("failed to reconfigure GHCR credentials: %w", err)
			}
		}
		if err := (Github{}).Verify(); err != nil {
			return fmt.Errorf("GitHub token verification failed after reconfiguration: %w", err)
		}
	}

	// Validate scopes and access.
	if err := (Github{}).EnsurePATScopes(); err != nil {
		return fmt.Errorf("token scope verification failed: %w", err)
	}

	if err := (Github{}).VerifyRepoAccess(); err != nil {
		return fmt.Errorf("repository access verification failed: %w", err)
	}

	if err := (Github{}).Whoami(); err == nil {
		fmt.Println("GitHub authentication context verified successfully.")
	}

	return nil
}

// ValidateAll runs all GitHub checks (Verify, PAT scopes, Repo access, Whoami)
// without reconfiguration or mutation.
func (Github) ValidateAll() error {
	fmt.Println("Running full GitHub validation suite...")

	if err := (Github{}).Verify(); err != nil {
		return err
	}
	if err := (Github{}).EnsurePATScopes(); err != nil {
		return err
	}
	if err := (Github{}).VerifyRepoAccess(); err != nil {
		return err
	}
	if err := (Github{}).Whoami(); err != nil {
		return err
	}

	fmt.Println("All GitHub checks completed successfully.")
	return nil
}

// VerifyRepoAccess checks that the configured GitHub token can access the expected repository.
func (Github) VerifyRepoAccess() error {
	token, err := loadGhcrToken()
	if err != nil {
		return fmt.Errorf("failed to load GHCR token: %w", err)
	}

	req, _ := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s", GithubRepo), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: GithubHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "no such host") {
			return fmt.Errorf("GitHub API unreachable — are you offline?")
		}
		return fmt.Errorf("failed to check GitHub repository access: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("GitHub repository access verified.")
		return nil
	}

	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("token lacks permissions to access repository (HTTP 403)")
	}

	return fmt.Errorf("failed to verify repository access (status: %s)", resp.Status)
}

// EnsurePATScopes validates that the current token has the required GHCR scopes:
// read:packages, write:packages, and optionally delete:packages.
func (Github) EnsurePATScopes() error {
	token, err := loadGhcrToken()
	if err != nil {
		return fmt.Errorf("failed to load GHCR token: %w", err)
	}

	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: GithubHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "no such host") {
			fmt.Println("Skipping scope check (offline environment detected).")
			return nil
		}
		return fmt.Errorf("failed to query GitHub API for token scopes: %w", err)
	}
	defer resp.Body.Close()

	scopes := resp.Header.Get("X-OAuth-Scopes")
	if scopes == "" {
		fmt.Println("Warning: GitHub did not return any scope metadata. This may indicate an older or classic token.")
		return nil
	}

	required := []string{"write:packages"}
	optional := []string{"read:packages"}
	missing := []string{}

	for _, r := range required {
		if !strings.Contains(scopes, r) {
			missing = append(missing, r)
		}
	}

	// Accept that write:packages implies read:packages
	hasWrite := strings.Contains(scopes, "write:packages")
	if !hasWrite {
		for _, r := range optional {
			if !strings.Contains(scopes, r) {
				missing = append(missing, r)
			}
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("GitHub token missing required or implied scopes: %s", strings.Join(missing, ", "))
	}

	fmt.Println("GitHub token scopes are sufficient for GHCR operations.")
	return nil
}

// Whoami prints information about the GitHub user associated with the current token.
func (Github) Whoami() error {
	token, err := loadGhcrToken()
	if err != nil {
		return fmt.Errorf("failed to load GHCR token: %w", err)
	}

	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: GithubHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "no such host") {
			fmt.Println("Skipping user lookup (offline environment detected).")
			return nil
		}
		return fmt.Errorf("failed to query GitHub API: %w", err)
	}
	defer resp.Body.Close()

	var user struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return fmt.Errorf("failed to parse user information: %w", err)
	}

	fmt.Printf("Authenticated as GitHub user: %s (%s)\n", user.Login, user.Name)
	return nil
}

// loadGhcrToken retrieves the GitHub PAT for GHCR operations.
// It first checks the GHCR_TOKEN environment variable, then falls back
// to reading the Docker configuration (~/.docker/config.json).
func loadGhcrToken() (string, error) {
	token := os.Getenv("GHCR_TOKEN")
	if token != "" {
		return token, nil
	}

	configPath := fmt.Sprintf("%s/.docker/config.json", os.Getenv("HOME"))
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("unable to read Docker config: %w", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("invalid Docker config JSON: %w", err)
	}

	auths, ok := cfg["auths"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("no 'auths' section found in Docker config")
	}

	entry, ok := auths["ghcr.io"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("no GHCR entry found in Docker config")
	}

	authStr, ok := entry["auth"].(string)
	if !ok {
		return "", fmt.Errorf("missing 'auth' field in GHCR config")
	}

	decoded, err := base64.StdEncoding.DecodeString(authStr)
	if err != nil {
		return "", fmt.Errorf("invalid base64 encoding in GHCR auth: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid GHCR auth format")
	}

	return parts[1], nil // second part is the token
}

// verifyGhcrToken validates the expiration and validity of a GHCR token.
func verifyGhcrToken(token string) error {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: GithubHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "no such host") {
			fmt.Println("Skipping GitHub token verification (offline environment detected).")
			return nil
		}
		return fmt.Errorf("failed to query GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("GitHub token is invalid or expired (HTTP 401)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected GitHub API response: %s", resp.Status)
	}

	expiryHeader := resp.Header.Get("GitHub-Authentication-Token-Expiration")
	if expiryHeader == "" {
		fmt.Println("No expiration metadata found. Token may be classic or non-expiring.")
		return nil
	}

	var expiry time.Time
	var parseErr error
	layouts := []string{
		"2006-01-02 15:04:05 MST",
		time.RFC3339,
		time.RFC1123,
	}
	for _, layout := range layouts {
		expiry, parseErr = time.Parse(layout, expiryHeader)
		if parseErr == nil {
			break
		}
	}
	if parseErr != nil {
		return fmt.Errorf("failed to parse expiration date (%s): %w", expiryHeader, parseErr)
	}

	daysLeft := int(time.Until(expiry).Hours() / 24)
	fmt.Printf("GitHub PAT expiration date: %s (%d days remaining)\n", expiry.Format(time.RFC1123), daysLeft)

	const warnThreshold = 30
	switch {
	case daysLeft <= 0:
		return fmt.Errorf("GitHub PAT has expired on %s — generate a new token immediately", expiry.Format("2006-01-02"))
	case daysLeft <= warnThreshold:
		fmt.Printf("Warning: GitHub PAT will expire in %d days. Consider renewing soon.\n", daysLeft)
	default:
		fmt.Println("GitHub PAT is valid and not near expiration.")
	}

	return nil
}
