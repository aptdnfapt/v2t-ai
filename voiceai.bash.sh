#!/bin/bash

# Voice AI Bash Script with Round-Robin API Keys and FFmpeg Speedup
# Simplified version without audio chunking complexity

set -euo pipefail

# --- Configuration ---
ENV_FILE="${ENV_FILE:-.env}"
HISTORY_DIR="${HOME}/.voiceai_history"
AUDIO_HISTORY_DIR="${HISTORY_DIR}/audio"
TEXT_HISTORY_DIR="${HISTORY_DIR}/text"
REQUEST_COUNTER_FILE="${HISTORY_DIR}/.voiceai_request_counter"
CURRENT_KEY_INDEX_FILE="${HISTORY_DIR}/.voiceai_current_key_index"
LAST_AUDIO_FILE="${HISTORY_DIR}/.last_audio_file"

# Runtime variables
TEMP_AUDIO=""
PROCESSED_AUDIO=""
RETRY_MODE=false
SPECIFIED_AUDIO_FILE=""

# Default values
DEFAULT_ARECORD_DEVICE="default"
DEFAULT_ARECORD_FORMAT="S16_LE"
DEFAULT_ARECORD_RATE="16000"
DEFAULT_ARECORD_CHANNELS="1"
DEFAULT_SPEEDUP_THRESHOLD="5"  # MB
DEFAULT_SPEEDUP_FACTOR="1.5"
DEFAULT_ROTATION_COUNT="3"
DEFAULT_MAX_AUDIO_SIZE="25"  # MB

# --- Helper Functions ---
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >&2
}

check_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        log "ERROR: Command '$1' not found. Please install it."
        exit 1
    fi
}

load_env() {
    if [[ -f "$ENV_FILE" ]]; then
        # Source the .env file
        set -a
        source "$ENV_FILE"
        set +a
        log "Loaded environment from: $ENV_FILE"
    else
        log "Warning: $ENV_FILE not found. Using defaults."
    fi
    
    # Check for required variables
    if [[ -z "${GEMINI_API_KEYS:-}" ]]; then
        log "ERROR: GEMINI_API_KEYS not set in $ENV_FILE"
        log "Please add: GEMINI_API_KEYS=\"key1,key2,key3\""
        exit 1
    fi
    
    # Set defaults if not specified
    ARECORD_DEVICE="${ARECORD_DEVICE:-$DEFAULT_ARECORD_DEVICE}"
    ARECORD_FORMAT="${ARECORD_FORMAT:-$DEFAULT_ARECORD_FORMAT}"
    ARECORD_RATE="${ARECORD_RATE:-$DEFAULT_ARECORD_RATE}"
    ARECORD_CHANNELS="${ARECORD_CHANNELS:-$DEFAULT_ARECORD_CHANNELS}"
    SPEEDUP_THRESHOLD="${SPEEDUP_THRESHOLD:-$DEFAULT_SPEEDUP_THRESHOLD}"
    SPEEDUP_FACTOR="${SPEEDUP_FACTOR:-$DEFAULT_SPEEDUP_FACTOR}"
    KEY_ROTATION_COUNT="${KEY_ROTATION_COUNT:-$DEFAULT_ROTATION_COUNT}"
    MAX_AUDIO_SIZE="${MAX_AUDIO_SIZE:-$DEFAULT_MAX_AUDIO_SIZE}"
    GEMINI_MODEL_NAME="${GEMINI_MODEL_NAME:-gemini-2.5-flash}"
    GEMINI_PROMPT_TEXT="${GEMINI_PROMPT_TEXT:-Transcribe this audio accurately and quickly.}"
    
    log "Configuration loaded. API keys count: $(echo "$GEMINI_API_KEYS" | tr ',' '\n' | wc -l)"
}

setup_directories() {
    mkdir -p "$AUDIO_HISTORY_DIR" "$TEXT_HISTORY_DIR"
    log "Created history directories"
}

# --- API Key Management ---
get_api_key_by_index() {
    local key_index="$1"
    local keys_array
    IFS=',' read -ra keys_array <<< "$GEMINI_API_KEYS"
    echo "${keys_array[$key_index]}"
}

get_current_api_key() {
    local keys_array
    IFS=',' read -ra keys_array <<< "$GEMINI_API_KEYS"
    local total_keys=${#keys_array[@]}
    
    # Initialize counter files if they don't exist
    if [[ ! -f "$REQUEST_COUNTER_FILE" ]]; then
        echo "0" > "$REQUEST_COUNTER_FILE"
    fi
    if [[ ! -f "$CURRENT_KEY_INDEX_FILE" ]]; then
        echo "0" > "$CURRENT_KEY_INDEX_FILE"
    fi
    
    local current_counter
    local current_index
    current_counter=$(cat "$REQUEST_COUNTER_FILE")
    current_index=$(cat "$CURRENT_KEY_INDEX_FILE")
    
    # Check if we need to rotate keys
    if ((current_counter % KEY_ROTATION_COUNT == 0)) && ((current_counter > 0)); then
        current_index=$(( (current_index + 1) % total_keys ))
        echo "$current_index" > "$CURRENT_KEY_INDEX_FILE"
        log "Rotated to API key index: $current_index"
    fi
    
    # Increment counter
    echo $((current_counter + 1)) > "$REQUEST_COUNTER_FILE"
    
    # Return the current key
    local current_key="${keys_array[$current_index]}"
    log "Using API key index: $current_index (request #$((current_counter + 1)))"
    echo "$current_key"
}

get_next_api_key() {
    local current_key_index="$1"
    local keys_array
    IFS=',' read -ra keys_array <<< "$GEMINI_API_KEYS"
    local total_keys=${#keys_array[@]}
    local next_index=$(( (current_key_index + 1) % total_keys ))
    echo "${keys_array[$next_index]}"
}

# --- Audio Processing ---
record_audio() {
    if [[ "$RETRY_MODE" == "true" ]]; then
        log "Retry mode: using existing audio file: $SPECIFIED_AUDIO_FILE"
        return 0
    fi
    
    # Create timestamp for recording
    local timestamp
    timestamp=$(date '+%Y-%m-%d_%H-%M-%S')
    
    # Set audio file paths in history directory
    TEMP_AUDIO="${AUDIO_HISTORY_DIR}/temp_${timestamp}.wav"
    PROCESSED_AUDIO="${AUDIO_HISTORY_DIR}/processed_${timestamp}.wav"
    
    log "Starting audio recording... Press Enter to stop"
    log "Recording to: $TEMP_AUDIO"
    
    # Record audio to temporary file
    arecord -D "$ARECORD_DEVICE" -f "$ARECORD_FORMAT" -r "$ARECORD_RATE" -c "$ARECORD_CHANNELS" "$TEMP_AUDIO" &
    local arecord_pid=$!
    
    # Wait for user to press Enter
    read -r
    kill "$arecord_pid" 2>/dev/null || true
    wait "$arecord_pid" 2>/dev/null || true
    
    log "Recording stopped"
}

process_audio_with_ffmpeg() {
    local input_file="$1"
    local output_file="$2"
    
    # Get audio size in MB
    local audio_size
    audio_size=$(du -m "$input_file" | cut -f1)
    log "Audio size: ${audio_size} MB"
    
    # Check if audio exceeds threshold
    if (( $(echo "$audio_size > $SPEEDUP_THRESHOLD" | bc -l) )); then
        log "Audio is large (${audio_size} MB > ${SPEEDUP_THRESHOLD} MB), speeding up by ${SPEEDUP_FACTOR}x"
        ffmpeg -y -i "$input_file" -filter:a "atempo=$SPEEDUP_FACTOR" "$output_file" 2>/dev/null
        log "Audio processed with ffmpeg speedup"
    else
        log "Audio size is acceptable, copying as-is"
        cp "$input_file" "$output_file"
    fi
    
    # Check final size
    local final_size
    final_size=$(du -m "$output_file" | cut -f1)
    log "Final audio size: ${final_size} MB"
    
    # Check if it exceeds maximum size
    if (( $(echo "$final_size > $MAX_AUDIO_SIZE" | bc -l) )); then
        log "ERROR: Processed audio is too large (${final_size} MB > ${MAX_AUDIO_SIZE} MB)"
        return 1
    fi
    
    return 0
}

# --- Transcription ---
transcribe_audio_with_retry() {
    local audio_file="$1"
    local current_key_index="$2"
    local max_retries="$3"
    
    local keys_array
    IFS=',' read -ra keys_array <<< "$GEMINI_API_KEYS"
    local total_keys=${#keys_array[@]}
    
    for ((attempt=0; attempt<max_retries; attempt++)); do
        local api_key
        if ((attempt == 0)); then
            api_key="${keys_array[$current_key_index]}"
            log "Transcribing with API key index: $current_key_index"
        else
            api_key=$(get_next_api_key $(( (current_key_index + attempt - 1) % total_keys )))
            log "Retry $attempt: Trying different API key"
        fi
        
        log "Transcribing with model: $GEMINI_MODEL_NAME"
        
        # Base64 encode the audio
        local base64_audio
        base64_audio=$(base64 -w 0 "$audio_file")
        
        # Create JSON payload
        local json_payload
        json_payload=$(cat <<EOF
{
  "contents": [{
    "parts": [
      {"text": "$GEMINI_PROMPT_TEXT"},
      {"inlineData": {"mimeType": "audio/wav", "data": "$base64_audio"}
    ]
  }],
  "generationConfig": {
    "temperature": 0.1,
    "maxOutputTokens": 1000
  }
}
EOF
)
        
        # Make API call with curl
        local response
        local api_url="https://generativelanguage.googleapis.com/v1beta/models/${GEMINI_MODEL_NAME}:generateContent?key=${api_key}"
        
        log "Sending request to Gemini API..."
        response=$(curl -s -w "\n%{http_code}" -X POST "$api_url" \
            -H "Content-Type: application/json" \
            -H "Accept: application/json" \
            -d "$json_payload")
        
        # Extract HTTP code and response body
        local http_code
        local response_body
        http_code=$(echo "$response" | tail -n1)
        response_body=$(echo "$response" | head -n -1)
        
        log "API response HTTP code: $http_code"
        
        # Check for success
        if [[ "$http_code" == "200" ]]; then
            # Extract text from response
            local transcribed_text
            transcribed_text=$(echo "$response_body" | jq -r '.candidates[0].content.parts[0].text // empty' 2>/dev/null || echo "")
            
            if [[ -n "$transcribed_text" ]]; then
                echo "$transcribed_text"
                return 0
            else
                log "ERROR: Failed to extract text from response"
                log "Response: $response_body"
            fi
        else
            log "ERROR: API request failed with HTTP $http_code"
            log "Response: $response_body"
            
            # Check if we should retry based on error type
            case "$http_code" in
                429|500|502|503|504)
                    log "Retriable error detected, trying next API key..."
                    continue
                    ;;
                *)
                    log "Non-retriable error, not retrying"
                    return 1
                    ;;
            esac
        fi
    done
    
    log "All $max_retries attempts failed"
    return 1
}

transcribe_audio() {
    local audio_file="$1"
    local current_key_index="$2"
    
    transcribe_audio_with_retry "$audio_file" "$current_key_index" 3
}

# --- History and Clipboard ---
get_last_audio_file() {
    if [[ -f "$LAST_AUDIO_FILE" ]]; then
        cat "$LAST_AUDIO_FILE"
    else
        # Find the most recent audio file
        find "$AUDIO_HISTORY_DIR" -name "*.wav" -type f -printf '%T@ %p\n' | \
            sort -n | tail -n1 | cut -d' ' -f2-
    fi
}

save_to_history() {
    local audio_file="$1"
    local transcribed_text="$2"
    local timestamp
    timestamp=$(date '+%Y-%m-%d_%H-%M-%S')
    
    # Save audio file with proper timestamp
    local audio_path="${AUDIO_HISTORY_DIR}/${timestamp}.wav"
    cp "$audio_file" "$audio_path"
    log "Saved audio to history: $audio_path"
    
    # Save text file
    if [[ -n "$transcribed_text" ]]; then
        local text_path="${TEXT_HISTORY_DIR}/${timestamp}.txt"
        echo "$transcribed_text" > "$text_path"
        log "Saved transcription to history: $text_path"
    fi
    
    # Update last audio file reference
    echo "$audio_path" > "$LAST_AUDIO_FILE"
    
    # Cleanup old audio files (keep last 3)
    cleanup_history
}

cleanup_history() {
    # Remove old audio files, keep only last 3
    find "$AUDIO_HISTORY_DIR" -name "*.wav" -type f -printf '%T@ %p\n' | \
        sort -n | head -n -3 | cut -d' ' -f2- | xargs -r rm -f
    log "Cleaned up old audio files"
}

copy_to_clipboard() {
    local text="$1"
    
    if [[ -z "$text" ]]; then
        return 1
    fi
    
    # Detect clipboard tool based on session type
    local clipboard_tool
    if [[ "${XDG_SESSION_TYPE:-}" == "wayland" ]] && command -v wl-copy >/dev/null 2>&1; then
        clipboard_tool="wl-copy"
    elif command -v xclip >/dev/null 2>&1; then
        clipboard_tool="xclip -selection clipboard"
    else
        log "ERROR: No clipboard tool found (wl-copy or xclip)"
        return 1
    fi
    
    log "Copying to clipboard using: $clipboard_tool"
    if echo "$text" | $clipboard_tool; then
        log "Copied to clipboard successfully"
        return 0
    else
        log "ERROR: Failed to copy to clipboard"
        return 1
    fi
}

# --- Argument Parsing ---
parse_arguments() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --retry)
                RETRY_MODE=true
                if [[ -n "${2:-}" && ! "$2" =~ ^-- ]]; then
                    SPECIFIED_AUDIO_FILE="$2"
                    shift
                fi
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                log "ERROR: Unknown argument: $1"
                show_help
                exit 1
                ;;
        esac
    done
}

show_help() {
    cat << EOF
Voice AI Bash Script - Fast Transcription with FFmpeg Speedup

USAGE:
    $0                    Record new audio and transcribe
    $0 --retry           Retry transcribing the last audio file
    $0 --retry FILE      Retry transcribing specific audio file

OPTIONS:
    --retry              Retry transcription of last or specified audio file
    -h, --help           Show this help message

EXAMPLES:
    $0                   # Record and transcribe new audio
    $0 --retry           # Retry last failed transcription
    $0 --retry /path/to/audio.wav  # Retry specific audio file

ENVIRONMENT:
    Copy .env.bash.example to .env and configure your settings
EOF
}

# --- Main Function ---
main() {
    # Parse arguments first
    parse_arguments "$@"
    
    log "Voice AI Bash Script starting..."
    if [[ "$RETRY_MODE" == "true" ]]; then
        log "Mode: Retry transcription"
    fi
    
    # Check dependencies
    check_command "arecord"
    check_command "ffmpeg"
    check_command "curl"
    check_command "jq"
    check_command "base64"
    check_command "bc"
    
    # Load configuration
    load_env
    setup_directories
    
    # Handle retry mode
    if [[ "$RETRY_MODE" == "true" ]]; then
        if [[ -n "$SPECIFIED_AUDIO_FILE" ]]; then
            if [[ ! -f "$SPECIFIED_AUDIO_FILE" ]]; then
                log "ERROR: Specified audio file not found: $SPECIFIED_AUDIO_FILE"
                exit 1
            fi
            TEMP_AUDIO="$SPECIFIED_AUDIO_FILE"
            PROCESSED_AUDIO="${AUDIO_HISTORY_DIR}/processed_retry_$(date '+%Y-%m-%d_%H-%M-%S').wav"
        else
            # Get last audio file
            local last_audio
            last_audio=$(get_last_audio_file)
            if [[ -z "$last_audio" || ! -f "$last_audio" ]]; then
                log "ERROR: No previous audio file found for retry"
                exit 1
            fi
            TEMP_AUDIO="$last_audio"
            PROCESSED_AUDIO="${AUDIO_HISTORY_DIR}/processed_retry_$(date '+%Y-%m-%d_%H-%M-%S').wav"
            log "Using last audio file: $last_audio"
        fi
    fi
    
    # Record audio (only in non-retry mode)
    record_audio
    
    if [[ "$RETRY_MODE" != "true" && ! -f "$TEMP_AUDIO" ]]; then
        log "ERROR: No audio file created"
        exit 1
    fi
    
    # Process audio with ffmpeg if needed
    if ! process_audio_with_ffmpeg "$TEMP_AUDIO" "$PROCESSED_AUDIO"; then
        log "ERROR: Audio processing failed"
        # In retry mode, don't delete the original audio file
        if [[ "$RETRY_MODE" != "true" ]]; then
            rm -f "$TEMP_AUDIO"
        fi
        rm -f "$PROCESSED_AUDIO"
        exit 1
    fi
    
    # Get current API key index for transcription
    local current_key_index=0
    if [[ -f "$CURRENT_KEY_INDEX_FILE" ]]; then
        current_key_index=$(cat "$CURRENT_KEY_INDEX_FILE")
    fi
    
    # Transcribe audio with retry logic
    local transcribed_text
    if transcribed_text=$(transcribe_audio "$PROCESSED_AUDIO" "$current_key_index"); then
        log "Transcription successful: '$transcribed_text'"
        
        # Save to history
        save_to_history "$PROCESSED_AUDIO" "$transcribed_text"
        
        # Copy to clipboard
        copy_to_clipboard "$transcribed_text"
        
        log "Process completed successfully"
    else
        log "Transcription failed"
        # Still save audio for potential retry
        save_to_history "$PROCESSED_AUDIO" ""
        exit 1
    fi
    
    # Cleanup - only delete temp files in non-retry mode
    if [[ "$RETRY_MODE" != "true" ]]; then
        rm -f "$TEMP_AUDIO"
    fi
    rm -f "$PROCESSED_AUDIO"
    log "Cleanup completed"
}

# --- Run Main ---
main "$@"