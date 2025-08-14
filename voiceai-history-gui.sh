#!/usr/bin/env bash

# VoiceAI History Viewer - GUI for viewing last 3 transcriptions
# Works alongside voiceai.gemini.live.fast.py

# --- Configuration ---
HISTORY_DIR="$HOME/.voiceai_history"
AUDIO_DIR="$HISTORY_DIR/audio"
TEXT_DIR="$HISTORY_DIR/text"
MAX_RECORDINGS=3

# Create directories if they don't exist
mkdir -p "$AUDIO_DIR"
mkdir -p "$TEXT_DIR"

# --- Functions ---

log_message() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >&2
}

check_dependencies() {
    local deps=("yad" "aplay" "python3")
    local missing=()
    
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            missing+=("$dep")
        fi
    done
    
    if [ ${#missing[@]} -ne 0 ]; then
        log_message "Missing dependencies: ${missing[*]}"
        yad --error --text="Missing dependencies: ${missing[*]}" --width=300
        exit 1
    fi
}

# Get list of recordings for display
get_recordings_list() {
    local recordings=()
    local count=0
    
    # Process files in reverse chronological order
    for audio_file in "$AUDIO_DIR"/*.wav; do
        [[ -f "$audio_file" ]] || continue
        
        local basename=$(basename "$audio_file" .wav)
        local text_file="$TEXT_DIR/${basename}.txt"
        local timestamp="$basename"
        
        # Try to extract timestamp from filename
        if [[ "$basename" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}-[0-9]{2}-[0-9]{2} ]]; then
            timestamp=$(echo "$basename" | sed 's/_/ /; s/-/ /g; s/-/ /3; s/-/ /5')
        fi
        
        local text_preview=""
        if [[ -f "$text_file" ]]; then
            text_preview=$(head -n 1 "$text_file" | cut -c1-50)
            [[ $(wc -l < "$text_file") -gt 1 ]] && text_preview+="..."
        else
            text_preview="<No transcription>"
        fi
        
        recordings+=("$basename" "$timestamp" "$text_preview")
        ((count++))
        
        # Limit to MAX_RECORDINGS
        if [ $count -ge $MAX_RECORDINGS ]; then
            break
        fi
    done
    
    # Output for yad --list
    if [ ${#recordings[@]} -gt 0 ]; then
        printf '%s\n' "${recordings[@]}"
    else
        echo "none" "No recordings" "No transcriptions available"
    fi
}

# Play audio file
play_audio() {
    local recording_id="$1"
    local audio_file="$AUDIO_DIR/${recording_id}.wav"
    
    if [[ -f "$audio_file" ]]; then
        aplay "$audio_file" &>/dev/null &
        local pid=$!
        yad --info --text="Playing audio...\n\nClick OK to stop." --width=200 --height=100
        kill $pid 2>/dev/null
    else
        yad --error --text="Audio file not found: $audio_file" --width=300
    fi
}

# Retry transcription
retry_transcription() {
    local recording_id="$1"
    local audio_file="$AUDIO_DIR/${recording_id}.wav"
    local text_file="$TEXT_DIR/${recording_id}.txt"
    
    if [[ ! -f "$audio_file" ]]; then
        yad --error --text="Audio file not found: $audio_file" --width=300
        return 1
    fi
    
    # Show progress dialog
    (
        echo "0"
        echo "# Preparing to retry transcription..."
        sleep 1
        
        echo "30"
        echo "# Converting audio format..."
        # Convert to proper format if needed
        local temp_wav=$(mktemp --suffix=.wav)
        ffmpeg -y -i "$audio_file" -ar 16000 -ac 1 -acodec pcm_s16le "$temp_wav" &>/dev/null
        echo "60"
        echo "# Sending to Gemini API..."
        
        # Here we would call the Python script or API directly
        # For now, simulate with a delay
        sleep 2
        echo "90"
        echo "# Processing response..."
        sleep 1
        echo "100"
    ) | yad --progress --auto-close --text="Retrying transcription..." --width=300
    
    # In a real implementation, we would:
    # 1. Call Gemini API with the audio file
    # 2. Save the new transcription
    # 3. Update the text file
    
    yad --info --text="Transcription retry completed!\n\nNote: This is a placeholder - actual implementation would call Gemini API." --width=300
}

# Show transcription text
show_transcription() {
    local recording_id="$1"
    local text_file="$TEXT_DIR/${recording_id}.txt"
    
    if [[ -f "$text_file" ]]; then
        yad --text-info --filename="$text_file" --title="Transcription - $recording_id" \
            --width=600 --height=400 --wrap --editable
    else
        yad --warning --text="No transcription found for $recording_id" --width=300
    fi
}

# Main GUI function
show_main_window() {
    while true; do
        # Get current recordings data
        local recordings_data=$(get_recordings_list)
        
        # Create main window
        local selected=$(echo "$recordings_data" | yad --list \
            --title="VoiceAI History Viewer" \
            --width=800 --height=400 \
            --column="ID" \
            --column="Timestamp" \
            --column="Preview" \
            --button="Play Audio!gtk-media-play":1 \
            --button="Retry Transcription!gtk-refresh":2 \
            --button="View Text!gtk-find":3 \
            --button="Refresh!gtk-refresh":4 \
            --button="Close!gtk-quit":0 \
            --print-column=0)
        
        local exit_code=$?
        
        # Handle window close
        if [ $exit_code -eq 252 ]; then
            break
        fi
        
        # Handle button clicks
        case $exit_code in
            0)  # Close
                break
                ;;
            1)  # Play Audio
                if [[ "$selected" != "none" && "$selected" != "" ]]; then
                    play_audio "$selected"
                else
                    yad --warning --text="Please select a recording first." --width=200
                fi
                ;;
            2)  # Retry Transcription
                if [[ "$selected" != "none" && "$selected" != "" ]]; then
                    retry_transcription "$selected"
                else
                    yad --warning --text="Please select a recording first." --width=200
                fi
                ;;
            3)  # View Text
                if [[ "$selected" != "none" && "$selected" != "" ]]; then
                    show_transcription "$selected"
                else
                    yad --warning --text="Please select a recording first." --width=200
                fi
                ;;
            4)  # Refresh - do nothing, loop will continue
                ;;
        esac
    done
}

# --- Main Execution ---

# Check dependencies
check_dependencies

# Show main window
show_main_window

log_message "VoiceAI History Viewer closed"