#!/usr/bin/env python3

import os
import base64
import json
import requests
import subprocess
import tempfile
import glob
import shutil
from concurrent.futures import ThreadPoolExecutor, as_completed
from dotenv import load_dotenv

# Load environment variables
load_dotenv()

# Import all the advanced settings from the fast script
GEMINI_API_KEY = os.getenv("GEMINI_API_KEY", "")
GEMINI_MODEL_NAME = os.getenv("GEMINI_MODEL_NAME", "gemini-1.5-flash")
GEMINI_FALLBACK_MODEL = os.getenv("GEMINI_FALLBACK_MODEL", "gemini-1.5-flash-8b")
GEMINI_PROMPT_TEXT = os.getenv("GEMINI_PROMPT_TEXT", "Transcribe this audio recording.")

# Advanced audio processing settings (same as fast script)
MAX_SEGMENT_SIZE_MB = float(os.getenv("MAX_SEGMENT_SIZE_MB", "2.0"))
SPEED_MULTIPLIER = float(os.getenv("SPEED_MULTIPLIER", "2.0"))
SILENCE_THRESHOLD = os.getenv("SILENCE_THRESHOLD", "1%")
MIN_SILENCE_DURATION = float(os.getenv("MIN_SILENCE_DURATION", "1.0"))

AUDIO_FILE_PATH = "/tmp/voice_input_audio_fast.wav"

def log_message(message):
    """Print message with timestamp."""
    import time
    print(f"[{time.strftime('%Y-%m-%d %H:%M:%S')}] {message}", flush=True)

# Copy advanced functions from the fast script
def check_command(command_name):
    """Checks if a command exists in the system's PATH."""
    return shutil.which(command_name) is not None

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
        import time
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

def process_audio_with_advanced_features(audio_file_path):
    """
    Advanced audio processing with segmentation, threading, and fallback protection.
    Same logic as the fast script but for saved files.
    """
    transcribed_text = None
    temp_dir = None
    
    try:
        # Read the saved audio file
        log_message(f"Reading saved audio file: {audio_file_path}")
        with open(audio_file_path, 'rb') as f:
            audio_data = f.read()
        
        # Check audio size
        audio_size_mb = get_audio_size_mb(audio_data)
        log_message(f"Audio size: {audio_size_mb:.2f} MB")
        
        # Strategy selection based on size (same as fast script)
        if audio_size_mb <= MAX_SEGMENT_SIZE_MB:
            # Small audio - direct processing
            log_message("Using direct processing for small audio")
            transcribed_text = transcribe_segment(audio_file_path, GEMINI_MODEL_NAME, 0)[1]
            
        else:
            # Large audio - advanced processing
            log_message(f"Large audio detected ({audio_size_mb:.2f} MB). Using advanced processing...")
            
            # Create temporary directory for processing
            temp_dir = tempfile.mkdtemp(prefix="voice_ai_restore_")
            temp_wav_file = audio_file_path  # Use the existing file
            
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
                transcribed_text = transcribe_segment(audio_file_path, GEMINI_FALLBACK_MODEL, 0)[1]
        
        return transcribed_text
            
    except Exception as e:
        log_message(f"Error in advanced audio processing: {e}")
        return None
    
    finally:
        # Clean up temporary directory
        if temp_dir and os.path.exists(temp_dir):
            try:
                shutil.rmtree(temp_dir)
                log_message("Cleaned up temporary processing files")
            except Exception as e:
                log_message(f"Error cleaning up temp directory: {e}")

def transcribe_saved_audio():
    """Transcribe the saved audio file using advanced processing."""
    if not os.path.exists(AUDIO_FILE_PATH):
        log_message(f"ERROR: Audio file not found: {AUDIO_FILE_PATH}")
        return None
    
    if not GEMINI_API_KEY or GEMINI_API_KEY == "YOUR_API_KEY_HERE":
        log_message("ERROR: GEMINI_API_KEY not set in .env file")
        return None
    
    # Check for advanced audio tools
    has_sox = check_command("sox")
    has_ffmpeg = check_command("ffmpeg")
    
    if not has_sox or not has_ffmpeg:
        log_message("WARNING: Missing audio tools (sox/ffmpeg). Using basic processing.")
        log_message("Install with: sudo apt install sox ffmpeg")
        # Fall back to simple processing
        return transcribe_segment(AUDIO_FILE_PATH, GEMINI_MODEL_NAME, 0)[1]
    
    # Use advanced processing (same as fast script)
    return process_audio_with_advanced_features(AUDIO_FILE_PATH)

def copy_to_clipboard(text):
    """Copy text to clipboard."""
    try:
        # Detect session type
        session_type = os.getenv("XDG_SESSION_TYPE", "x11").lower()
        
        if "wayland" in session_type:
            cmd = ["wl-copy"]
        else:
            cmd = ["xclip", "-selection", "clipboard"]
        
        subprocess.run(cmd, input=text.encode('utf-8'), check=True)
        log_message("✅ Copied to clipboard!")
        return True
    except Exception as e:
        log_message(f"❌ Clipboard copy failed: {e}")
        return False

def main():
    """Main function."""
    log_message("🔧 Audio Error Recovery Tool")
    log_message("=" * 50)
    
    # Check if audio file exists
    if not os.path.exists(AUDIO_FILE_PATH):
        log_message(f"❌ No saved audio file found at: {AUDIO_FILE_PATH}")
        log_message("Nothing to recover.")
        return
    
    # Get file info
    file_size = os.path.getsize(AUDIO_FILE_PATH)
    file_size_mb = file_size / (1024 * 1024)
    log_message(f"📁 Found saved audio: {file_size_mb:.2f} MB")
    
    # Transcribe
    transcript = transcribe_saved_audio()
    
    if transcript:
        log_message("🎉 Transcription successful!")
        log_message(f"📝 Result: '{transcript}'")
        
        # Copy to clipboard
        if copy_to_clipboard(transcript):
            # Clean up the saved file
            try:
                os.remove(AUDIO_FILE_PATH)
                log_message(f"🗑️  Cleaned up: {AUDIO_FILE_PATH}")
            except Exception as e:
                log_message(f"⚠️  Could not remove file: {e}")
        else:
            log_message(f"⚠️  File retained for manual processing: {AUDIO_FILE_PATH}")
    else:
        log_message("❌ Transcription failed")
        log_message(f"📁 Audio file retained: {AUDIO_FILE_PATH}")

if __name__ == "__main__":
    main()