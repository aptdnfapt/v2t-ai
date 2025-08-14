#!/bin/bash

set -e

echo "Installing VoiceAI TUI v1.0.0..."

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

# Download Python scripts to same directory
echo "Downloading Python scripts..."
cd /tmp
curl -L "https://raw.githubusercontent.com/$REPO/main/voiceai.gemini.live.fast.py" -o "voiceai.gemini.live.fast.py"

# Copy Python scripts to install directory
cp voiceai.gemini.live.fast.py "$INSTALL_DIR/"

# Make Python scripts executable
chmod +x "$INSTALL_DIR/voiceai.gemini.live.fast.py"

# Create .voiceai_history directory
HISTORY_DIR="$HOME/.voiceai_history"
mkdir -p "$HISTORY_DIR"

# Prompt for API key and create .env file
echo "Please enter your Google Gemini API key:"
read -r GEMINI_API_KEY

if [ -n "$GEMINI_API_KEY" ]; then
    echo "Creating .env file with your API key..."
    echo "# Google Gemini API Configuration" > "$HISTORY_DIR/.env"
    echo "GEMINI_API_KEY=$GEMINI_API_KEY" >> "$HISTORY_DIR/.env"
    echo "API key saved successfully in $HISTORY_DIR/.env"
else
    echo "No API key provided. Creating example .env file..."
    echo "# Google Gemini API Configuration" > "$HISTORY_DIR/.env"
    echo "GEMINI_API_KEY=your_api_key_here" >> "$HISTORY_DIR/.env"
    echo "Please edit $HISTORY_DIR/.env with your actual API key later"
fi

echo "Installation complete!"
echo "Run 'voiceai-tui' to start the application"