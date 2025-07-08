#!/bin/bash

# Configuration
REPO="aptdnfapt/v2t-ai"
INSTALL_DIR="$HOME/voice-to-text"
BINARY_NAME="main-rest"
RUNNER_SCRIPT="$HOME/voice.sh"

# --- Helper Functions ---
print_info() {
    echo -e "\033[34m[INFO]\033[0m $1"
}

print_success() {
    echo -e "\033[32m[SUCCESS]\033[0m $1"
}

print_error() {
    echo -e "\033[31m[ERROR]\033[0m $1" >&2
    exit 1
}

check_dep() {
    if ! command -v "$1" &> /dev/null; then
        print_error "Dependency '$1' not found. Please install it to continue."
    fi
}

# --- Main Script ---

# 1. Check for dependencies
print_info "Checking for required dependencies..."
check_dep "curl"
check_dep "jq"
check_dep "tmux"
print_success "All dependencies are installed."

# 2. Get the latest release URL from GitHub
print_info "Fetching the latest release from GitHub..."
API_URL="https://api.github.com/repos/$REPO/releases"
RELEASE_DATA=$(curl -s -L "$API_URL" | jq -r '.[0]')

if [ "$RELEASE_DATA" = "null" ]; then
    print_error "Could not find any releases. Please check the repository and workflow status."
fi

BINARY_URL=$(echo "$RELEASE_DATA" | jq -r ".assets[] | select(.name==\"$BINARY_NAME\") | .browser_download_url")

if [ -z "$BINARY_URL" ] || [ "$BINARY_URL" = "null" ]; then
    print_error "Could not find the '$BINARY_NAME' binary in the latest release."
fi
print_success "Found latest release with $BINARY_NAME binary."

# 3. Download the binary
TEMP_DIR=$(mktemp -d)
BINARY_PATH="$TEMP_DIR/$BINARY_NAME"

print_info "Downloading binary from GitHub releases..."
curl -s -L -o "$BINARY_PATH" "$BINARY_URL"
if [ $? -ne 0 ]; then
    print_error "Failed to download the binary from GitHub releases."
fi

if [ ! -f "$BINARY_PATH" ]; then
    print_error "Binary '$BINARY_NAME' was not downloaded successfully."
fi
print_success "Binary downloaded successfully."

# 4. Set up the installation directory
print_info "Setting up installation directory at $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"
mv "$BINARY_PATH" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/$BINARY_NAME"
print_success "Binary moved and made executable."

# 5. Configure the .env file
ENV_FILE="$INSTALL_DIR/.env"
if [ -f "$ENV_FILE" ]; then
    print_info ".env file already exists. Skipping creation."
else
    print_info "Creating .env file template..."
    cat > "$ENV_FILE" << EOL
# Gemini API Configuration
GEMINI_API_KEY="YOUR_API_KEY_HERE"
GEMINI_MODEL_NAME="gemini-2.5-flash"
GEMINI_FALLBACK_MODEL="gemini-2.0-flash-exp"
GEMINI_PROMPT_TEXT="Transcribe this audio recording."

# Advanced Audio Processing Settings
# Maximum size per segment before splitting (in MB)
MAX_SEGMENT_SIZE_MB="2.0"

# Speed multiplier for very large files (2.0 = 2x speed)
SPEED_MULTIPLIER="2.0"

# Silence detection threshold for sox (percentage or dB) - higher = fewer segments
SILENCE_THRESHOLD="5%"

# Minimum silence duration to split on (seconds) - higher = fewer segments
MIN_SILENCE_DURATION="3.0"
EOL
    print_success ".env file template created."
    echo
    print_info "IMPORTANT: Please edit $ENV_FILE and add your Gemini API key!"
    print_info "Replace 'YOUR_API_KEY_HERE' with your actual Gemini API key."
    print_info "You can get your API key from: https://aistudio.google.com/app/apikey"
fi

# 6. Create the runner script
print_info "Creating runner script at $RUNNER_SCRIPT..."
cat > "$RUNNER_SCRIPT" << EOL
#!/bin/bash
SESSION_NAME="voice-ai"

# Check if the tmux session already exists
tmux has-session -t \$SESSION_NAME 2>/dev/null

if [ \$? != 0 ]; then
    echo "Starting new tmux session: \$SESSION_NAME"
    tmux new-session -d -s \$SESSION_NAME "cd $INSTALL_DIR && ./$BINARY_NAME"
    echo "Voice AI started in the background. Use 'tmux a -t \$SESSION_NAME' to view logs."
else
    echo "Session '\$SESSION_NAME' is already running."
    echo "To stop it, run: tmux kill-session -t \$SESSION_NAME"
fi
EOL
chmod +x "$RUNNER_SCRIPT"
print_success "Runner script created and made executable."

# 7. Clean up
print_info "Cleaning up temporary files..."
rm -rf "$TEMP_DIR"

# 8. Final instructions
echo
print_success "Installation complete!"
echo
print_info "Setup Summary:"
print_info "- Binary installed: $INSTALL_DIR/$BINARY_NAME"
print_info "- Configuration: $INSTALL_DIR/.env"
print_info "- Runner script: $RUNNER_SCRIPT"
echo
print_info "Next steps:"
print_info "1. Edit $INSTALL_DIR/.env and add your Gemini API key"
print_info "2. To start the service, run: $RUNNER_SCRIPT"
print_info "3. To view the logs, run: tmux attach -t voice-ai"
print_info "4. To stop the service, run: tmux kill-session -t voice-ai"
echo
print_success "Enjoy your voice-to-text AI!"
