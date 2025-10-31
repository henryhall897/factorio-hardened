#!/bin/bash

# Default save file path (can be overridden)
DEFAULT_SAVE_PATH="/factorio/saves/server-world.zip"

# Check if a map is provided through the command line argument, otherwise use the default
SAVE_GAME_PATH="${1:-$DEFAULT_SAVE_PATH}"

# Optionally allow server to load the latest save file after the initial load
if [ "$2" == "server-load-latest" ]; then
  echo "[FACTORIO-HARDENED] Loading the latest save game..."
  # Logic to load the latest save file
  # You can add more logic here if you want to dynamically load the latest world from your save directory
  LATEST_SAVE=$(ls -t /factorio/saves/*.zip | head -n 1)  # This gets the most recent save file
  SAVE_GAME_PATH="$LATEST_SAVE"
fi

# Run the Factorio server with the provided save file or the default one
exec /opt/factorio/bin/x64/factorio \
  --start-server "$SAVE_GAME_PATH" \
  --console-log /factorio/logs/factorio-current.log
