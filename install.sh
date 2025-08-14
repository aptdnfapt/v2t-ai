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

# Prompt for API key and create .env file
echo "Please enter your Google Gemini API key (required):"
read -r GEMINI_API_KEY

if [ -z "$GEMINI_API_KEY" ]; then
    echo "ERROR: API key is required for the application to work."
    echo "Please obtain an API key from Google AI Studio and run this script again."
    exit 1
fi

# Prompt for model selection with default
echo "Please enter the Gemini model you want to use (default: gemini-2.0-flash):"
read -r GEMINI_MODEL

if [ -z "$GEMINI_MODEL" ]; then
    GEMINI_MODEL="gemini-2.0-flash"
fi

echo "Creating .env file with your configuration..."
echo "# Google Gemini API Configuration" > "$HISTORY_DIR/.env"
echo "GEMINI_API_KEY=$GEMINI_API_KEY" >> "$HISTORY_DIR/.env"
echo "GEMINI_MODEL_NAME=$GEMINI_MODEL" >> "$HISTORY_DIR/.env"
echo "Configuration saved successfully in $HISTORY_DIR/.env"

echo "Installation complete!"
echo "Run 'voiceai-tui' to start the application"