#!/usr/bin/env bash
# ============================================================
# buildx.sh — ensures Buildx is installed and configured
# ============================================================

set -euo pipefail

echo "[Buildx] Ensuring Docker Buildx and multi-arch builder..."

# Ensure docker is running
if ! docker info &>/dev/null; then
  echo "[Buildx] Docker daemon not running. Starting..."
  sudo systemctl start docker || true
fi

# Detect plugin dir
PLUGIN_DIR="/usr/libexec/docker/cli-plugins"
[[ -d $PLUGIN_DIR ]] || PLUGIN_DIR="/usr/local/lib/docker/cli-plugins"
sudo mkdir -p "$PLUGIN_DIR"

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac

# Install Buildx if missing
if ! docker buildx version &>/dev/null; then
  echo "[Buildx] Installing Buildx for $ARCH..."
  URL="https://github.com/docker/buildx/releases/latest/download/buildx.linux-${ARCH}"
  DEST="${PLUGIN_DIR}/docker-buildx"
  sudo curl -fsSL -o "$DEST" "$URL"
  sudo chmod +x "$DEST"
fi

# Ensure QEMU emulation
echo "[Buildx] Enabling QEMU multi-arch emulation..."
docker run --privileged --rm tonistiigi/binfmt --install all >/dev/null

# Ensure a containerized builder exists
if ! docker buildx inspect hardened-builder &>/dev/null; then
  echo "[Buildx] Creating containerized builder 'hardened-builder'..."
  docker buildx create --name hardened-builder --driver docker-container --use
  docker buildx inspect hardened-builder --bootstrap >/dev/null
else
  echo "[Buildx] Builder 'hardened-builder' already exists. Setting active..."
  docker buildx use hardened-builder
fi

# Verify
echo "[Buildx] Active builder:"
docker buildx inspect | grep -E "Name|Driver"

echo "[Buildx] Ready for multi-platform builds."
