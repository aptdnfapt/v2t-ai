#!/bin/bash

set -e

echo "Installing VoiceAI TUI v1.0.1..."

# Configuration
REPO="aptdnfapt/v2t-ai"
INSTALL_DIR="$HOME/.local/bin"

# Create installation directory
mkdir -p "$INSTALL_DIR"

# Download latest binary
echo "Downloading TUI binary..."
curl -L "https://github.com/$REPO/releases/latest/download/voiceai-tui" -o "$INSTALL_DIR/voiceai-tui"

# Make binary executable
chmod +x "$INSTALL_DIR/voiceai-tui"

# Download Python scripts and requirements to same directory
echo "Downloading Python scripts and dependencies..."
cd /tmp
curl -L "https://raw.githubusercontent.com/$REPO/main/voiceai.gemini.live.fast.py" -o "voiceai.gemini.live.fast.py"
curl -L "https://raw.githubusercontent.com/$REPO/main/requirements.txt" -o "requirements.txt"
curl -L "https://raw.githubusercontent.com/$REPO/main/.env.example" -o ".env.example"

# Copy Python scripts to install directory
cp voiceai.gemini.live.fast.py "$INSTALL_DIR/"

# Make Python scripts executable
chmod +x "$INSTALL_DIR/voiceai.gemini.live.fast.py"

# Install Python dependencies
echo "Installing Python dependencies..."
pip3 install --user -r requirements.txt

# Create .voiceai_history directory
HISTORY_DIR="$HOME/.voiceai_history"
mkdir -p "$HISTORY_DIR"

# Create default .env file from .env.example
echo "Creating default .env file..."
cp -rf .env.example "$HISTORY_DIR/.env"

echo "Default configuration saved in $HISTORY_DIR/.env"
echo "IMPORTANT: You must edit this file to add your Google Gemini API key."
echo "Run 'nano ~/.voiceai_history/.env' or 'vim ~/.voiceai_history/.env' to edit it."

echo "Installation complete!"
echo "Please edit ~/.voiceai_history/.env to add your Google Gemini API key."
echo "Then run 'voiceai-tui' to start the application"
