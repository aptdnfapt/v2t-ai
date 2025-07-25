#!/usr/bin/env python3

import os
import signal
import subprocess
import sys
import time
import shutil
import threading
import queue
import atexit
import base64
import json
import requests
from dotenv import load_dotenv
import io
import wave
import tempfile

# --- Introduction ---
# This script implements an ultra-fast voice-to-text transcription service using optimized Gemini REST API calls.
#
# Speed Optimizations:
# 1. Direct memory processing - no temporary files
# 2. Streaming audio capture with immediate processing
# 3. Optimized WAV header generation in memory
# 4. Concurrent processing with threading
# 5. Minimal API payload with compressed audio
# 6. Fast clipboard operations

# --- Configuration ---
load_dotenv()

# Audio recording settings
ARECORD_DEVICE = "default"
ARECORD_FORMAT = "S16_LE"  # Signed 16-bit Little-Endian PCM
ARECORD_RATE = "16000"     # 16kHz sample rate (optimal for speech)
ARECORD_CHANNELS = "1"     # Mono audio

# Gemini API settings
GEMINI_API_KEY = os.getenv("GEMINI_API_KEY", "")
GEMINI_MODEL_NAME = os.getenv("GEMINI_MODEL_NAME", "gemini-2.5-flash")
GEMINI_FALLBACK_MODEL = os.getenv("GEMINI_FALLBACK_MODEL", "gemini-2.0-flash-exp")
GEMINI_PROMPT_TEXT = os.getenv("GEMINI_PROMPT_TEXT", "Transcribe this audio accurately and quickly.")
GEMINI_FALLBACK_MODEL = os.getenv("GEMINI_FALLBACK_MODEL", "gemini-1.5-flash-8b")

# YAD Notification Configuration
ICON_NAME_IDLE = "audio-input-microphone"
ICON_NAME_RECORDING = "media-record"
ICON_NAME_PROCESSING = "system-search"
TOOLTIP_IDLE = "Voice Input: Idle (Press keybind to record)"
TOOLTIP_RECORDING = "Voice Input: Recording... (Press keybind to stop)"
TOOLTIP_PROCESSING = "Voice Input: Processing..."
YAD_NOTIFICATION_COMMAND_CLICK = ":"

# System Configuration
PID_FILE = "/tmp/voice_input_gemini.pid"
AUDIO_FILE_TMP = "/tmp/voice_input_audio_fast.wav"

# --- Global State ---
is_recording = False
is_processing = False
arecord_process = None
yad_process = None
clipboard_command = []
transcription_thread = None
final_transcript_queue = queue.Queue()

def log_message(message):
    """Prints a message with a timestamp."""
    print(f"[{time.strftime('%Y-%m-%d %H:%M:%S')}] {message}", flush=True)

def check_command(command_name):
    """Checks if a command exists in the system's PATH."""
    if shutil.which(command_name) is None:
        log_message(f"ERROR: Command '{command_name}' not found. Please install it.")
        return False
    return True

def send_yad_command(command_str):
    """Sends a command to the running yad notification process."""
    global yad_process
    if yad_process and yad_process.poll() is None:
        try:
            yad_process.stdin.write(f"{command_str.strip()}\n".encode('utf-8'))
            yad_process.stdin.flush()
        except (BrokenPipeError, AttributeError):
            log_message("ERROR: Broken pipe trying to write to yad. It may have crashed.")
            yad_process = None
        except Exception as e:
            log_message(f"ERROR: Could not send command to yad: {e}")

def update_tray_icon_state():
    """Updates the yad tray icon and tooltip based on the recording state."""
    if not yad_process: return
    if is_processing:
        send_yad_command(f"icon:{ICON_NAME_PROCESSING}")
        send_yad_command(f"tooltip:{TOOLTIP_PROCESSING}")
    elif is_recording:
        send_yad_command(f"icon:{ICON_NAME_RECORDING}")
        send_yad_command(f"tooltip:{TOOLTIP_RECORDING}")
    else:
        send_yad_command(f"icon:{ICON_NAME_IDLE}")
        send_yad_command(f"tooltip:{TOOLTIP_IDLE}")

def cleanup_resources():
    """Cleans up all running processes and files before exiting."""
    global arecord_process, yad_process, transcription_thread
    log_message("Cleaning up resources...")

    if transcription_thread and transcription_thread.is_alive():
        transcription_thread.join(timeout=2)

    if arecord_process and arecord_process.poll() is None:
        arecord_process.terminate()
        try:
            arecord_process.wait(timeout=1)
        except subprocess.TimeoutExpired:
            arecord_process.kill()
            arecord_process.wait()

    if yad_process and yad_process.poll() is None:
        log_message("Stopping yad notification icon...")
        send_yad_command("quit")
        try:
            if yad_process.stdin: yad_process.stdin.close()
            yad_process.wait(timeout=2)
        except Exception:
            pass
        yad_process = None

    if os.path.exists(PID_FILE):
        try:
            os.remove(PID_FILE)
        except OSError as e:
            log_message(f"Error removing PID file: {e}")

def handle_exit_signal(signum, frame):
    """Gracefully exits on SIGTERM or SIGINT."""
    log_message(f"Received signal {signum}. Exiting gracefully.")
    sys.exit(0)

def copy_to_clipboard(text):
    """Copies the given text to the system clipboard."""
    if not text:
        return False
    log_message(f"Copying to clipboard using '{clipboard_command[0]}'...")
    try:
        subprocess.run(clipboard_command, input=text.encode('utf-8'), check=True)
        log_message("Copied to clipboard.")
        return True
    except Exception as e:
        log_message(f"Error with {clipboard_command[0]}: {e}")
        return False

def save_audio_for_debugging(audio_data, wav_data):
    """Saves audio data to temp file for debugging when errors occur."""
    try:
        with open(AUDIO_FILE_TMP, 'wb') as f:
            f.write(wav_data)
        log_message(f"Audio saved for debugging: {AUDIO_FILE_TMP}")
        return True
    except Exception as e:
        log_message(f"Failed to save audio for debugging: {e}")
        return False

def cleanup_temp_audio():
    """Removes temporary audio file if it exists."""
    if os.path.exists(AUDIO_FILE_TMP):
        try:
            os.remove(AUDIO_FILE_TMP)
            log_message(f"Removed: {AUDIO_FILE_TMP}")
        except OSError as e:
            log_message(f"Error removing temp audio: {e}")

def create_wav_header(sample_rate, channels, bits_per_sample, data_size):
    """Creates a WAV header for the given parameters."""
    # WAV header structure
    header = bytearray()
    
    # RIFF header
    header.extend(b'RIFF')
    header.extend((36 + data_size).to_bytes(4, 'little'))  # File size - 8
    header.extend(b'WAVE')
    
    # fmt chunk
    header.extend(b'fmt ')
    header.extend((16).to_bytes(4, 'little'))  # fmt chunk size
    header.extend((1).to_bytes(2, 'little'))   # Audio format (PCM)
    header.extend(channels.to_bytes(2, 'little'))
    header.extend(sample_rate.to_bytes(4, 'little'))
    header.extend((sample_rate * channels * bits_per_sample // 8).to_bytes(4, 'little'))  # Byte rate
    header.extend((channels * bits_per_sample // 8).to_bytes(2, 'little'))  # Block align
    header.extend(bits_per_sample.to_bytes(2, 'little'))
    
    # data chunk
    header.extend(b'data')
    header.extend(data_size.to_bytes(4, 'little'))
    
    return bytes(header)

def get_audio_size_mb(audio_data):
    """Get audio size in MB."""
    return len(audio_data) / (1024 * 1024)

def _transcribe_single_wav(wav_data, model_name):
    """Transcribe a single WAV data byte array with one specific model."""
    log_message(f"Transcribing with {model_name}...")
    start_time = time.time()

    try:
        # Base64 encode
        base64_audio_data = base64.b64encode(wav_data).decode('utf-8')

        # Create API payload
        json_payload = {
            "contents": [{
                "parts": [
                    {"text": GEMINI_PROMPT_TEXT},
                    {"inlineData": {"mimeType": "audio/wav", "data": base64_audio_data}}
                ]
            }],
            "generationConfig": {
                "temperature": 0.1,
                "maxOutputTokens": 1000
            }
        }

        api_url = f"https://generativelanguage.googleapis.com/v1beta/models/{model_name}:generateContent?key={GEMINI_API_KEY}"
        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json"
        }

        # Make the API call
        response = requests.post(api_url, headers=headers, json=json_payload)

        api_time = time.time() - start_time
        log_message(f"API call to {model_name} completed in {api_time:.2f}s with status code {response.status_code}")

        response.raise_for_status()  # Raise an exception for bad status codes (4xx or 5xx)

        response_json = response.json()

        try:
            text = response_json["candidates"][0]["content"]["parts"][0]["text"].strip()
            return text
        except (KeyError, IndexError, TypeError) as e:
            log_message(f"ERROR: Could not extract text from {model_name} response. Response: {response_json}. Error: {e}")
            return None

    except requests.exceptions.HTTPError as e:
        log_message(f"ERROR: HTTP error with {model_name}: {e}")
        if e.response is not None:
            log_message(f"Response status code: {e.response.status_code}")
            log_message(f"Response reason: {e.response.reason}")
            log_message(f"Response content: {e.response.text}")
            if e.response.status_code == 429:
                log_message("This indicates a rate limiting error.")
        return None
    except requests.exceptions.RequestException as e:
        log_message(f"ERROR: A request-related error occurred with {model_name}: {e}")
        log_message("This could be a network issue, DNS error, or timeout.")
        return None
    except Exception as e:
        log_message(f"ERROR: An unexpected error occurred while transcribing with {model_name}: {e}")
        return None

def transcribe_wav_data(wav_data, use_fallback=True):
    """Transcribe a WAV data byte array with primary and optional fallback model."""
    
    # Try primary model first
    text = _transcribe_single_wav(wav_data, GEMINI_MODEL_NAME)
    
    # If primary failed and fallback is enabled
    if text is None and use_fallback:
        log_message(f"Primary model failed, trying fallback model...")
        text = _transcribe_single_wav(wav_data, GEMINI_FALLBACK_MODEL)
        
    return text

def process_audio_with_advanced_features(audio_data):
    """
    Advanced audio processing with fallback protection.
    """
    transcribed_text = None
    wav_data = None
    
    try:
        # Create proper WAV file in memory
        sample_rate = int(ARECORD_RATE)
        channels = int(ARECORD_CHANNELS)
        bits_per_sample = 16
        
        # Create WAV header
        wav_header = create_wav_header(sample_rate, channels, bits_per_sample, len(audio_data))
        wav_data = wav_header + audio_data
        
        # Check audio size
        audio_size_mb = get_audio_size_mb(wav_data)
        log_message(f"Audio size: {audio_size_mb:.2f} MB")
        
        audio_to_process = wav_data
        
        # Now, transcribe the audio_to_process
        log_message("Processing audio directly...")
        transcribed_text = transcribe_wav_data(audio_to_process)

        # Handle results
        if transcribed_text:
            log_message(f"Final transcription: '{transcribed_text}'")
            
            copy_successful = copy_to_clipboard(transcribed_text)
            
            if copy_successful:
                cleanup_temp_audio()
            else:
                log_message("Clipboard copy failed. Audio RETAINED for debugging.")
                save_audio_for_debugging(audio_data, wav_data)
        else:
            log_message("All transcription attempts failed.")
            log_message(f"Audio RETAINED: {AUDIO_FILE_TMP}")
            save_audio_for_debugging(audio_data, wav_data)
            
    except Exception as e:
        log_message(f"Error in advanced audio processing: {e}")
        if wav_data:
            save_audio_for_debugging(audio_data, wav_data)
        else:
            log_message("Could not save audio - WAV data not created")

def transcription_loop(audio_stream, result_queue):
    """
    Fast transcription loop that processes audio data immediately with protection.
    """
    log_message("Fast transcription thread started.")
    try:
        # Read all audio data efficiently
        audio_chunks = []
        chunk_size = 8192  # 8KB chunks for efficient reading
        
        while True:
            chunk = audio_stream.read(chunk_size)
            if not chunk:
                break
            audio_chunks.append(chunk)
        
        # Combine all chunks
        audio_data = b''.join(audio_chunks)
        log_message(f"Read {len(audio_data)} bytes of audio data in {len(audio_chunks)} chunks.")
        
        if audio_data:
            # Process audio with advanced features
            process_audio_with_advanced_features(audio_data)
        else:
            log_message("No audio data received.")

    except Exception as e:
        log_message(f"ERROR in transcription thread: {e}")
    finally:
        log_message("Fast transcription thread finished.")

def toggle_recording_handler(signum, frame):
    """Handles SIGUSR1 to start or stop recording."""
    global is_recording, arecord_process, transcription_thread, is_processing

    if is_recording:
        log_message("Signal: Stopping recording...")
        if arecord_process:
            arecord_process.terminate()
        is_recording = False
        is_processing = True
    else:
        if is_processing:
            log_message("Signal: Ignoring start, currently processing previous recording.")
            return

        log_message("Signal: Starting recording...")

        # Optimized arecord command for raw PCM data
        arecord_command = [
            "arecord", "-D", ARECORD_DEVICE, "-f", ARECORD_FORMAT,
            "-r", ARECORD_RATE, "-c", ARECORD_CHANNELS, "-t", "raw"
        ]
        try:
            arecord_process = subprocess.Popen(arecord_command, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            time.sleep(0.1)
            if arecord_process.poll() is not None:
                err_msg = arecord_process.stderr.read().decode(errors='ignore').strip()
                log_message(f"ERROR: arecord failed to start: {err_msg}")
                is_recording = False
            else:
                log_message("Recording started. Streaming to fast processing...")
                is_recording = True
                # Start the fast transcription thread
                transcription_thread = threading.Thread(
                    target=transcription_loop,
                    args=(arecord_process.stdout, final_transcript_queue)
                )
                transcription_thread.start()
        except Exception as e:
            log_message(f"Failed to start arecord: {e}")
            is_recording = False

    update_tray_icon_state()

def start_yad_notification():
    """Starts the yad notification icon process."""
    global yad_process
    if not check_command("yad"): return None
    yad_command = [
        "yad", "--notification", f"--image={ICON_NAME_IDLE}",
        f"--text={TOOLTIP_IDLE}", f"--command={YAD_NOTIFICATION_COMMAND_CLICK}",
        "--listen"
    ]
    try:
        log_message("Starting yad notification icon...")
        yad_process = subprocess.Popen(yad_command, stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        time.sleep(0.2)
        if yad_process.poll() is not None:
            err = yad_process.stderr.read().decode(errors='ignore').strip()
            log_message(f"ERROR: yad failed to start: {err}")
            return None
        log_message("Yad notification icon started.")
        return yad_process
    except Exception as e:
        log_message(f"ERROR: Failed to start yad: {e}")
        return None

def main():
    """Main function to set up and run the application."""
    global yad_process, clipboard_command, is_processing

    # --- Pre-flight Checks ---
    if not GEMINI_API_KEY or GEMINI_API_KEY == "YOUR_API_KEY_HERE":
        log_message("ERROR: GEMINI_API_KEY environment variable not set.")
        log_message("Please create a .env file with GEMINI_API_KEY=\"YOUR_API_KEY\"")
        sys.exit(1)

    log_message("Gemini API key configured for ADVANCED FAST processing.")

    session_type = os.getenv("XDG_SESSION_TYPE", "x11").lower()
    clipboard_tool = "wl-copy" if "wayland" in session_type else "xclip"
    if not check_command(clipboard_tool): sys.exit(1)
    if not check_command("arecord"): sys.exit(1)

    if clipboard_tool == "wl-copy":
        clipboard_command = ["wl-copy"]
    else:
        clipboard_command = ["xclip", "-selection", "clipboard"]

    # --- PID File Management ---
    if os.path.exists(PID_FILE):
        try:
            with open(PID_FILE, 'r') as f: pid = int(f.read().strip())
            os.kill(pid, 0)
            log_message(f"Script already running (PID {pid}). Exiting.")
            sys.exit(1)
        except (OSError, ValueError):
            log_message(f"Stale PID file found. Removing.")
            try: os.remove(PID_FILE)
            except OSError as e: log_message(f"Could not remove stale PID file: {e}"); sys.exit(1)
    try:
        with open(PID_FILE, 'w') as f: f.write(str(os.getpid()))
    except IOError as e:
        log_message(f"Could not write PID file: {e}"); sys.exit(1)

    # --- Register Signal Handlers ---
    signal.signal(signal.SIGTERM, handle_exit_signal)
    signal.signal(signal.SIGINT, handle_exit_signal)
    signal.signal(signal.SIGUSR1, toggle_recording_handler)
    atexit.register(cleanup_resources)

    # --- Start Services ---
    yad_process = start_yad_notification()
    if yad_process:
        log_message("Tray icon active.")
    else:
        log_message("WARNING: Tray icon is INACTIVE.")

    log_message(f"ADVANCED FAST Voice AI script started (PID {os.getpid()}). Send SIGUSR1 to toggle recording.")
    log_message(f"Features: Fallback model")

    # --- Main Loop ---
    while True:
        try:
            # Check if transcription thread is still running
            if transcription_thread and not transcription_thread.is_alive() and is_processing:
                # Transcription finished, reset processing state
                is_processing = False
                update_tray_icon_state()
            
            # Small sleep to prevent busy waiting
            time.sleep(0.1)
        except KeyboardInterrupt:
            break

if __name__ == "__main__":
    main()
