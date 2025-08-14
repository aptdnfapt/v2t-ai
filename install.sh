#!/bin/bash

set -e

echo "Installing VoiceAI TUI v1.0.0..."

# Configuration
REPO="aptdnfapt/v2t-ai"
INSTALL_DIR="$HOME/.local/bin"

# Create install directory
mkdir -p "$INSTALL_DIR"

# Download latest binary
echo "Downloading latest version..."
curl -L "https://github.com/$REPO/releases/latest/download/voiceai-tui" -o "$INSTALL_DIR/voiceai-tui"

# Make executable
chmod +x "$INSTALL_DIR/voiceai-tui"

# Check if in PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo "Adding $INSTALL_DIR to PATH..."
    echo "export PATH=\"\$PATH:$INSTALL_DIR\"" >> "$HOME/.bashrc"
    echo "Please run: source ~/.bashrc"
fi

echo "Installation complete!"
echo "Run 'voiceai-tui' to start the application"