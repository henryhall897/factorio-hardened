#!/usr/bin/env bash
# ============================================================
# init.sh — Factorio-Hardened bootstrap script
# ============================================================
# This script prepares a fresh machine or VM for development.
# It installs Go, Docker, Mage, and runs mage deps:all.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/henryhall897/factorio-hardened/main/init.sh | bash
# or:
#   ./init.sh
# ============================================================

set -euo pipefail

# --- Colors ---
BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() { echo -e "${BLUE}[$(date +'%H:%M:%S')]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
ok() { echo -e "${GREEN}[OK]${NC} $*"; }

# --- Requirements check ---
if [[ $EUID -ne 0 ]]; then
  warn "You are not running as root. Some installations may prompt for sudo."
fi

# --- System package installs ---
log "Updating system packages..."
sudo apt-get update -y

log "Installing required packages (curl, git, make, docker, Go)..."
sudo apt-get install -y curl git make docker.io golang

# --- Ensure Docker service is running ---
if ! sudo systemctl is-active --quiet docker; then
  log "Starting Docker service..."
  sudo systemctl enable docker
  sudo systemctl start docker
fi

# --- Ensure Mage is installed ---
if ! command -v mage &>/dev/null; then
  log "Installing Mage build tool..."
  go install github.com/magefile/mage@latest
  # Ensure GOPATH/bin is on PATH for current session
  export PATH="$(go env GOPATH)/bin:$PATH"
else
  ok "Mage already installed."
fi

# --- Clone repo if not already in it ---
REPO_URL="https://github.com/henryhall897/factorio-hardened.git"
if [[ ! -d ".git" ]]; then
  log "Cloning repository..."
  git clone "$REPO_URL"
  cd factorio-hardened
else
  ok "Repository already present."
fi

# --- Verify Go environment ---
if ! go version >/dev/null 2>&1; then
  warn "Go installation not detected on PATH. Please restart your shell or source ~/.bashrc"
else
  ok "Go detected: $(go version)"
fi

# --- Run Mage bootstrap ---
if [ -f "magefile.go" ] || [ -d "magefiles" ]; then
  log "Bootstrapping project dependencies with Mage..."
  mage deps:all || {
    warn "mage deps:all failed — check logs above."
    exit 1
  }
  ok "Mage dependencies installed successfully."
else
  warn "No Mage build files found; skipping mage deps:all"
fi

ok "Factorio-Hardened environment ready!"
log "You can now run: mage docker:build"
