#!/usr/bin/env python3

import os
import signal
import subprocess
import sys
import time
import shutil
import base64
import json
import requests
from dotenv import load_dotenv

# --- Configuration ---
load_dotenv()
GEMINI_API_KEY = os.getenv("GEMINI_API_KEY", "")
GEMINI_MODEL_NAME = os.getenv("GEMINI_MODEL_NAME", "gemini-1.5-flash-latest")
GEMINI_PROMPT_TEXT = os.getenv("GEMINI_PROMPT_TEXT", "Transcribe this audio recording.")


PID_FILE = "/tmp/voice_input_gemini.pid"
AUDIO_FILE_TMP = "/tmp/voice_input_audio.wav"

ARECORD_DEVICE = "default"
ARECORD_FORMAT = "S16_LE"
ARECORD_RATE = "16000"
ARECORD_CHANNELS = "1"

# --- Global State ---
is_recording = False
arecord_process = None
clipboard_command = []

# --- Helper Functions ---
def log_message(message):
    print(f"[{time.strftime('%Y-%m-%d %H:%M:%S')}] {message}", flush=True)

def check_command(command_name):
    if shutil.which(command_name) is None:
        log_message(f"ERROR: Command '{command_name}' not found. Please install it.")
        return False
    return True

def cleanup_resources():
    global arecord_process
    log_message("Cleaning up resources...")
    if arecord_process and arecord_process.poll() is None:
        arecord_process.terminate()
        try: arecord_process.wait(timeout=1)
        except subprocess.TimeoutExpired: arecord_process.kill(); arecord_process.wait()
    if os.path.exists(PID_FILE):
        try: os.remove(PID_FILE)
        except OSError as e: log_message(f"Error removing PID file: {e}")

def handle_exit_signal(signum, frame):
    log_message(f"Received signal {signum}. Exiting gracefully.")
    sys.exit(0)

def get_audio_mime_type(file_path):
    try:
        result = subprocess.run(["file", "--mime-type", "-b", file_path],
                                capture_output=True, text=True, check=True)
        return result.stdout.strip()
    except FileNotFoundError:
        log_message("INFO: 'file' command not found. Defaulting MIME type to 'audio/wav'.")
        return "audio/wav"
    except subprocess.CalledProcessError as e:
        log_message(f"Error determining MIME type: {e}. Defaulting to 'audio/wav'.")
        return "audio/wav"
    except Exception as e:
        log_message(f"Unexpected error in get_audio_mime_type: {e}. Defaulting to 'audio/wav'.")
        return "audio/wav"

def transcribe_with_gemini(audio_file_path):
    if not GEMINI_API_KEY or GEMINI_API_KEY == "YOUR_API_KEY_HERE":
        log_message("ERROR: GEMINI_API_KEY is not set.")
        return None
    if not os.path.exists(audio_file_path):
        log_message(f"Error: Audio file {audio_file_path} not found for Gemini.")
        return None

    mime_type = get_audio_mime_type(audio_file_path)

    try:
        with open(audio_file_path, "rb") as af:
            base64_audio_data = base64.b64encode(af.read()).decode('utf-8')
    except Exception as e:
        log_message(f"Error reading or base64 encoding audio file: {e}")
        return None

    json_payload = {
        "contents": [{"parts": [{"text": GEMINI_PROMPT_TEXT}, {"inlineData": {"mimeType": mime_type, "data": base64_audio_data}}]}]
    }
    api_url = f"https://generativelanguage.googleapis.com/v1beta/models/{GEMINI_MODEL_NAME}:generateContent?key={GEMINI_API_KEY}"
    headers = {"Content-Type": "application/json"}

    log_message(f"Sending request to Gemini API ({GEMINI_MODEL_NAME})... (Timeout: 30s)")
    try:
        response = requests.post(api_url, headers=headers, json=json_payload, timeout=30)
        response.raise_for_status()
        response_json = response.json()
        if "candidates" in response_json and \
           response_json["candidates"] and \
           "content" in response_json["candidates"][0] and \
           "parts" in response_json["candidates"][0]["content"] and \
           response_json["candidates"][0]["content"]["parts"] and \
           "text" in response_json["candidates"][0]["content"]["parts"][0]:
            return response_json["candidates"][0]["content"]["parts"][0]["text"].strip()
        else:
            log_message("Error: Could not find transcribed text in Gemini response structure.")
            log_message(f"Full Gemini Response: {json.dumps(response_json, indent=2)}")
            return None
    except requests.exceptions.Timeout:
        log_message(f"Error: Timeout making API request to Gemini (exceeded 30 seconds).")
        return None
    except requests.exceptions.RequestException as e:
        log_message(f"Error making API request to Gemini: {e}")
        if hasattr(e, 'response') and e.response is not None:
            log_message(f"Gemini API Response Content: {e.response.text}")
        return None
    except json.JSONDecodeError:
        log_message("Error: Could not decode JSON response from Gemini.")
        if 'response' in locals() and response is not None: log_message(f"Raw Gemini Response: {response.text}")
        return None
    except Exception as e:
        log_message(f"An unexpected error occurred during Gemini API call: {e}")
        return None

def process_audio():
    transcribed_text = None
    max_retries = 2
    for attempt in range(max_retries + 1):
        transcribed_text = transcribe_with_gemini(AUDIO_FILE_TMP)
        if transcribed_text:
            break
        if attempt < max_retries:
            log_message(f"Error during transcription, retrying ({attempt + 1}/{max_retries})...")
            time.sleep(1)

    if transcribed_text:
        log_message(f"Gemini: '{transcribed_text}'")
        log_message(f"Copying transcription to clipboard using '{clipboard_command[0]}'...")
        copy_env = os.environ.copy()
        if clipboard_command[0] == "xclip":
            display_var = os.getenv('DISPLAY_FOR_XCLIP', ':0')
            if 'DISPLAY' not in copy_env: copy_env['DISPLAY'] = display_var
            x_authority_file_path = os.getenv('XAUTHORITY_FOR_XCLIP', os.path.expanduser("~/.Xauthority"))
            if 'XAUTHORITY' not in copy_env and os.path.exists(x_authority_file_path): copy_env['XAUTHORITY'] = x_authority_file_path

        copy_successful = False
        try:
            subprocess.run(clipboard_command, input=transcribed_text.encode('utf-8'), check=True, env=copy_env)
            log_message("Copied to clipboard."); copy_successful = True
        except Exception as e: log_message(f"Error with {clipboard_command[0]}: {e}")

        if copy_successful:
            if os.path.exists(AUDIO_FILE_TMP):
                try: os.remove(AUDIO_FILE_TMP); log_message(f"Removed: {AUDIO_FILE_TMP}")
                except OSError as e: log_message(f"Error removing temp audio: {e}")
        else: log_message(f"Clipboard copy failed. Audio RETAINED: {AUDIO_FILE_TMP}")
    else:
        log_message("No transcription from Gemini or API error after all retries.")
        log_message(f"Audio RETAINED: {AUDIO_FILE_TMP}")

def toggle_recording_handler(signum, frame):
    global is_recording, arecord_process
    if is_recording:
        log_message("Signal: Stopping record...")
        if arecord_process and arecord_process.poll() is None:
            arecord_process.terminate()
            try: arecord_process.wait(timeout=1)
            except: arecord_process.kill(); arecord_process.wait()
        is_recording = False
        if os.path.exists(AUDIO_FILE_TMP): process_audio()
        else: log_message("No audio file to process.")
    else:
        log_message("Signal: Starting record...")
        if os.path.exists(AUDIO_FILE_TMP):
            try: os.remove(AUDIO_FILE_TMP)
            except OSError as e: log_message(f"No old temp file: {e}")
        arecord_command = ["arecord", "-D", ARECORD_DEVICE, "-f", ARECORD_FORMAT, "-r", ARECORD_RATE, "-c", ARECORD_CHANNELS, "-t", "wav", AUDIO_FILE_TMP]
        try:
            arecord_process = subprocess.Popen(arecord_command, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            time.sleep(0.1)
            if arecord_process.poll() is not None:
                err_msg = arecord_process.stderr.read().decode(errors='ignore').strip()
                log_message(f"ERROR: arecord failed: {err_msg}")
                is_recording = False
            else: is_recording = True; log_message(f"Recording to {AUDIO_FILE_TMP}")
        except Exception as e: log_message(f"Failed arecord: {e}"); is_recording = False

def main():
    global clipboard_command
    session_type = os.getenv("XDG_SESSION_TYPE", "x11").lower()
    clipboard_tool = ""

    if "wayland" in session_type:
        log_message("Wayland session detected. Using wl-copy for clipboard.")
        clipboard_tool = "wl-copy"
        clipboard_command = ["wl-copy"]
    else:
        log_message("X11 or unknown session type detected. Using xclip for clipboard.")
        clipboard_tool = "xclip"
        clipboard_command = ["xclip", "-selection", "clipboard"]

    if GEMINI_API_KEY == "YOUR_API_KEY_HERE" or not GEMINI_API_KEY:
        log_message("CRITICAL: GEMINI_API_KEY not set."); sys.exit(1)
    if not all(check_command(cmd) for cmd in ["arecord", clipboard_tool]):
        sys.exit(1)
    check_command("file")

    if os.path.exists(PID_FILE):
        try:
            with open(PID_FILE, 'r') as f: pid = int(f.read().strip())
            os.kill(pid, 0); log_message(f"Script already running (PID {pid}). Exiting."); sys.exit(1)
        except (OSError, ValueError):
            log_message(f"Stale PID file ({PID_FILE}). Removing.");
            try: os.remove(PID_FILE)
            except OSError as e: log_message(f"Could not remove stale PID file: {e}."); sys.exit(1)
    try:
        with open(PID_FILE, 'w') as f: f.write(str(os.getpid()))
    except IOError as e: log_message(f"PID write error: {e}"); sys.exit(1)

    log_message(f"Script started (PID {os.getpid()}). Listening for SIGUSR1 to toggle recording.")

    signal.signal(signal.SIGTERM, handle_exit_signal)
    signal.signal(signal.SIGINT, handle_exit_signal)
    signal.signal(signal.SIGUSR1, toggle_recording_handler)

    try:
        while True: signal.pause()
    except: pass
    finally:
        cleanup_resources()
        log_message("Script terminated.")

if __name__ == "__main__":
    main()
