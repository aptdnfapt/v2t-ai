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
import glob
from concurrent.futures import ThreadPoolExecutor, as_completed

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
GEMINI_MODEL_NAME = os.getenv("GEMINI_MODEL_NAME", "gemini-1.5-flash")
GEMINI_FALLBACK_MODEL = os.getenv("GEMINI_FALLBACK_MODEL", "gemini-1.5-flash-8b")
GEMINI_PROMPT_TEXT = os.getenv("GEMINI_PROMPT_TEXT", "Transcribe this audio accurately and quickly.")
GEMINI_FALLBACK_MODEL = os.getenv("GEMINI_FALLBACK_MODEL", "gemini-1.5-flash-8b")

# Audio processing settings
MAX_SEGMENT_SIZE_MB = float(os.getenv("MAX_SEGMENT_SIZE_MB", "2.0"))  # Split if larger
SPEED_MULTIPLIER = float(os.getenv("SPEED_MULTIPLIER", "2.0"))  # For very large files
SILENCE_THRESHOLD = os.getenv("SILENCE_THRESHOLD", "1%")  # sox silence threshold
MIN_SILENCE_DURATION = float(os.getenv("MIN_SILENCE_DURATION", "1.0"))  # seconds

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

def check_audio_tools():
    """Check if required audio processing tools are available."""
    tools_needed = []
    if not check_command("sox"):
        tools_needed.append("sox")
    if not check_command("ffmpeg"):
        tools_needed.append("ffmpeg")
    
    if tools_needed:
        log_message(f"WARNING: Missing audio tools: {', '.join(tools_needed)}")
        log_message("Install with: sudo apt install sox ffmpeg")
        return False
    return True

def get_audio_size_mb(audio_data):
    """Get audio size in MB."""
    return len(audio_data) / (1024 * 1024)

def speed_up_audio(input_file, output_file, speed_factor=2.0):
    """Speed up audio using ffmpeg without changing pitch."""
    try:
        cmd = [
            "ffmpeg", "-i", input_file, "-filter:a", f"atempo={speed_factor}",
            "-y", output_file
        ]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        return True
    except subprocess.CalledProcessError as e:
        log_message(f"Error speeding up audio: {e}")
        return False

def split_audio_by_silence(input_file, output_dir):
    """Split audio by silence using sox."""
    try:
        # Create output directory
        os.makedirs(output_dir, exist_ok=True)
        
        # Use sox to split by silence
        output_pattern = os.path.join(output_dir, "segment_%03d.wav")
        cmd = [
            "sox", input_file, output_pattern,
            "silence", "1", "0.1", SILENCE_THRESHOLD,
            "1", str(MIN_SILENCE_DURATION), SILENCE_THRESHOLD,
            ":", "newfile", ":", "restart"
        ]
        
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        
        # Get list of created segments
        segments = sorted(glob.glob(os.path.join(output_dir, "segment_*.wav")))
        log_message(f"Split audio into {len(segments)} segments")
        return segments
        
    except subprocess.CalledProcessError as e:
        log_message(f"Error splitting audio: {e}")
        return []

def transcribe_segment(segment_file, model_name, segment_index):
    """Transcribe a single audio segment."""
    try:
        # Read the segment file
        with open(segment_file, 'rb') as f:
            audio_data = f.read()
        
        # Base64 encode
        base64_audio_data = base64.b64encode(audio_data).decode('utf-8')
        
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
        
        log_message(f"Transcribing segment {segment_index + 1} with {model_name}...")
        start_time = time.time()
        
        with requests.Session() as session:
            response = session.post(api_url, headers=headers, json=json_payload, timeout=15)
            response.raise_for_status()
        
        api_time = time.time() - start_time
        log_message(f"Segment {segment_index + 1} completed in {api_time:.2f}s")
        
        response_json = response.json()
        
        try:
            text = response_json["candidates"][0]["content"]["parts"][0]["text"].strip()
            return (segment_index, text)
        except (KeyError, IndexError, TypeError):
            log_message(f"No text found in segment {segment_index + 1} response")
            return (segment_index, "")
            
    except Exception as e:
        log_message(f"Error transcribing segment {segment_index + 1}: {e}")
        return (segment_index, "")

def transcribe_with_gemini_fast(audio_data):
    """
    Ultra-fast transcription using optimized Gemini REST API calls.
    """
    if not GEMINI_API_KEY or GEMINI_API_KEY == "YOUR_API_KEY_HERE":
        log_message("ERROR: GEMINI_API_KEY is not set.")
        return None

    try:
        # Create proper WAV file in memory
        sample_rate = int(ARECORD_RATE)
        channels = int(ARECORD_CHANNELS)
        bits_per_sample = 16
        
        # Create WAV header
        wav_header = create_wav_header(sample_rate, channels, bits_per_sample, len(audio_data))
        
        # Combine header and data
        wav_data = wav_header + audio_data
        
        # Base64 encode the complete WAV file
        base64_audio_data = base64.b64encode(wav_data).decode('utf-8')
        
        # Optimized JSON payload
        json_payload = {
            "contents": [{
                "parts": [
                    {"text": GEMINI_PROMPT_TEXT},
                    {"inlineData": {"mimeType": "audio/wav", "data": base64_audio_data}}
                ]
            }],
            "generationConfig": {
                "temperature": 0.1,  # Lower temperature for more consistent transcription
                "maxOutputTokens": 1000
            }
        }
        
        api_url = f"https://generativelanguage.googleapis.com/v1beta/models/{GEMINI_MODEL_NAME}:generateContent?key={GEMINI_API_KEY}"
        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json"
        }

        log_message(f"Sending optimized request to Gemini API ({GEMINI_MODEL_NAME})...")
        start_time = time.time()
        
        # Use session for connection reuse
        with requests.Session() as session:
            response = session.post(api_url, headers=headers, json=json_payload, timeout=20)
            response.raise_for_status()
            
        api_time = time.time() - start_time
        log_message(f"API response received in {api_time:.2f}s")
        
        response_json = response.json()
        
        # Fast response parsing
        try:
            text = response_json["candidates"][0]["content"]["parts"][0]["text"].strip()
            return text
        except (KeyError, IndexError, TypeError):
            log_message("Error: Could not find transcribed text in Gemini response structure.")
            log_message(f"Response keys: {list(response_json.keys())}")
            return None
            
    except requests.exceptions.Timeout:
        log_message(f"Error: Timeout making API request to Gemini (exceeded 20 seconds).")
        return None
    except requests.exceptions.RequestException as e:
        log_message(f"Error making API request to Gemini: {e}")
        return None
    except Exception as e:
        log_message(f"An unexpected error occurred during Gemini API call: {e}")
        return None

def process_audio_with_advanced_features(audio_data):
    """
    Advanced audio processing with segmentation, threading, and fallback protection.
    """
    transcribed_text = None
    wav_data = None
    temp_dir = None
    
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
        
        # Strategy selection based on size
        if audio_size_mb <= MAX_SEGMENT_SIZE_MB:
            # Small audio - direct processing
            log_message("Using direct processing for small audio")
            transcribed_text = transcribe_with_gemini_fast(audio_data)
            
        else:
            # Large audio - advanced processing
            log_message(f"Large audio detected ({audio_size_mb:.2f} MB). Using advanced processing...")
            
            # Create temporary directory for processing
            temp_dir = tempfile.mkdtemp(prefix="voice_ai_")
            temp_wav_file = os.path.join(temp_dir, "input.wav")
            
            # Save WAV data to temp file
            with open(temp_wav_file, 'wb') as f:
                f.write(wav_data)
            
            # Check if we need to speed up audio
            if audio_size_mb > MAX_SEGMENT_SIZE_MB * 2:
                log_message(f"Very large audio. Applying {SPEED_MULTIPLIER}x speed...")
                speed_file = os.path.join(temp_dir, "speed.wav")
                if speed_up_audio(temp_wav_file, speed_file, SPEED_MULTIPLIER):
                    temp_wav_file = speed_file
                    log_message("Audio speed increased successfully")
                else:
                    log_message("Speed increase failed, continuing with original")
            
            # Split audio by silence
            segments_dir = os.path.join(temp_dir, "segments")
            segments = split_audio_by_silence(temp_wav_file, segments_dir)
            
            if segments:
                # Parallel transcription of segments
                log_message(f"Starting parallel transcription of {len(segments)} segments...")
                
                # Use ThreadPoolExecutor for concurrent API calls
                segment_results = []
                with ThreadPoolExecutor(max_workers=min(4, len(segments))) as executor:
                    # Submit all transcription tasks
                    future_to_segment = {
                        executor.submit(transcribe_segment, segment, GEMINI_MODEL_NAME, i): i
                        for i, segment in enumerate(segments)
                    }
                    
                    # Collect results as they complete
                    for future in as_completed(future_to_segment):
                        try:
                            result = future.result()
                            segment_results.append(result)
                        except Exception as e:
                            segment_index = future_to_segment[future]
                            log_message(f"Segment {segment_index + 1} failed: {e}")
                            # Try fallback model
                            try:
                                log_message(f"Retrying segment {segment_index + 1} with fallback model...")
                                fallback_result = transcribe_segment(
                                    segments[segment_index], 
                                    GEMINI_FALLBACK_MODEL, 
                                    segment_index
                                )
                                segment_results.append(fallback_result)
                            except Exception as fallback_error:
                                log_message(f"Fallback also failed for segment {segment_index + 1}: {fallback_error}")
                                segment_results.append((segment_index, ""))
                
                # Sort results by segment index and combine
                segment_results.sort(key=lambda x: x[0])
                transcribed_parts = [result[1] for result in segment_results if result[1]]
                
                if transcribed_parts:
                    transcribed_text = " ".join(transcribed_parts).strip()
                    log_message(f"Combined transcription from {len(transcribed_parts)} segments")
                else:
                    log_message("No successful transcriptions from any segment")
            
            else:
                # Fallback to direct processing if splitting failed
                log_message("Audio splitting failed, trying direct processing with fallback model...")
                transcribed_text = transcribe_segment(temp_wav_file, GEMINI_FALLBACK_MODEL, 0)[1]
        
        # Handle results
        if transcribed_text:
            log_message(f"Final transcription: '{transcribed_text}'")
            
            # Try to copy to clipboard
            copy_successful = copy_to_clipboard(transcribed_text)
            
            if copy_successful:
                # Success! Clean up temp files
                cleanup_temp_audio()
                if temp_dir and os.path.exists(temp_dir):
                    shutil.rmtree(temp_dir)
                    log_message("Cleaned up temporary files")
            else:
                # Clipboard failed, save audio for debugging
                log_message("Clipboard copy failed. Audio RETAINED for debugging.")
                save_audio_for_debugging(audio_data, wav_data)
        else:
            # Transcription failed completely
            log_message("All transcription attempts failed.")
            log_message(f"Audio RETAINED: {AUDIO_FILE_TMP}")
            save_audio_for_debugging(audio_data, wav_data)
            
    except Exception as e:
        log_message(f"Error in advanced audio processing: {e}")
        # Save audio for debugging on any error
        if wav_data:
            save_audio_for_debugging(audio_data, wav_data)
        else:
            log_message("Could not save audio - WAV data not created")
    
    finally:
        # Clean up temporary directory
        if temp_dir and os.path.exists(temp_dir):
            try:
                shutil.rmtree(temp_dir)
            except Exception as e:
                log_message(f"Error cleaning up temp directory: {e}")

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

    # Check audio processing tools
    if not check_audio_tools():
        log_message("WARNING: Advanced features disabled. Install sox and ffmpeg for best performance.")
        log_message("Falling back to basic processing mode.")

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
    log_message(f"Features: Parallel processing, Audio segmentation, Fallback model, Speed adjustment")
    log_message(f"Config: Max segment size: {MAX_SEGMENT_SIZE_MB}MB, Speed multiplier: {SPEED_MULTIPLIER}x")

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