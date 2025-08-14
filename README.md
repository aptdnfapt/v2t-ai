# VoiceAI - Gemini Voice-to-Text Transcription with TUI

VoiceAI is a powerful voice-to-text transcription tool that uses Google's Gemini API for accurate, real-time transcription. This repository contains both a Python backend for audio processing and a sleek Terminal User Interface (TUI) built with Go for easy management of recordings.

The transcribed text is automatically copied to your system clipboard, making it easy to use in any application.

Description : So you hit a key bind and it starts recordings and then you hit that same key binds again to stop the recording and gets the voice to text back 

## Quick Installation

For a quick installation, run:

```bash
curl -sSL https://raw.githubusercontent.com/aptdnfapt/v2t-ai/main/install.sh | bash
```

This will download and install both the TUI application and the Python transcription backend to `~/.local/bin/`, and prompt you for your Gemini API key.

## Features

-   **Terminal User Interface (TUI)**: Modern, intuitive terminal interface for managing voice recordings
-   **Fast Transcription**: Optimized Python backend using Gemini API for quick, accurate transcriptions
-   **Clipboard Integration**: Transcribed text is automatically copied to your clipboard
-   **Recording Management**: View, play back, and retry transcriptions of previous recordings
-   **System Tray Integration**: Visual feedback with system tray icon indicating recording status
-   **Robust**: Includes PID file management to prevent multiple instances and proper resource cleanup
-   **Configurable**: Set your API key during installation, with optional customization of Gemini model and transcription prompt
-   **Retry Logic**: Automatically retries transcription requests to the Gemini API on failure

## Requirements

### System Dependencies

VoiceAI requires the following command-line tools to be installed on your system:

-   `arecord`: For audio recording (part of ALSA).
-   `xclip`: For copying text to the clipboard on **X11**.
-   `wl-clipboard`: Provides `wl-copy` for clipboard on **Wayland**.
-   `yad`: For the system tray icon.

On **Debian/Ubuntu**, you can install them with:
```bash
sudo apt-get update
sudo apt-get install alsa-utils xclip wl-clipboard yad
```

On **Fedora**, you can install them with:
```bash
sudo dnf install alsa-utils xclip wl-clipboard yad
```

### Python Dependencies

The Python transcription backend requires the following libraries:
- `requests`: For API communication with Gemini
- `python-dotenv`: For environment variable management

These will be automatically installed during the quick installation process.

## Setup and Configuration

### Quick Installation (Recommended)

For most users, the quickest way to install VoiceAI is to run:

```bash
curl -sSL https://raw.githubusercontent.com/aptdnfapt/v2t-ai/main/install.sh | bash
```

This will:
1. Download and install the TUI application and Python backend to `~/.local/bin/`
2. Prompt you for your Google Gemini API key and create a `.env` file
3. Install required Python dependencies


Then to use it you need to run on terminal or make a key bind that runs it . Check the section bellow . Running this starts the recordings and changes the icon for yad in system tray indicating that its recording and running it again stops the recordings and saves it sends to gemini and gets the text in your clipboard for you to paste .

```bash
sh -c 'kill -s USR1 $(cat /tmp/voice_input_gemini.pid)'
```

### Manual Installation

If you prefer to install manually:

1. Clone this repository:
   ```bash
   git clone https://github.com/aptdnfapt/v2t-ai.git
   cd v2t-ai
   ```

2. Obtain a Gemini API key from [Google AI Studio](https://aistudio.google.com/app/apikey).

3. Run the installation script:
   ```bash
   ./install.sh
   ```

4. The script will prompt you for your API key and automatically create the `.env` file in `~/.voiceai_history/`.

## Usage

VoiceAI consists of two components:
1. A background Python service that handles audio recording and transcription
2. A Terminal User Interface (TUI) for managing recordings and settings

### Starting the Background Service

The Python transcription service needs to be running in the background to handle voice recordings. Start it with:

```bash
voiceai.gemini.live.fast.py
```

This will start the service with a system tray icon indicating its status.

### Using the TUI

Once the background service is running, you can start the TUI to manage your recordings:

```bash
voiceai-tui
```

The TUI provides a clean interface where you can:
- View previous recordings
- Play back audio files
- Retry transcriptions
- Copy text to clipboard

### Setting Up a Global Hotkey

To toggle voice recording, you need to set up a global hotkey that sends a `SIGUSR1` signal to the background service.

#### Using the PID File (Recommended)

This command reads the Process ID (PID) from the file created by the script (`/tmp/voice_input_gemini.pid`) and sends the signal directly:

```bash
sh -c 'kill -s USR1 $(cat /tmp/voice_input_gemini.pid)'
```

#### Binding the Command

Go to your desktop environment's or window manager's keyboard settings and create a new custom shortcut.

-   **For Desktop Environments (GNOME, KDE, XFCE, etc.)**:
    -   Go to Settings -> Keyboard -> Custom Shortcuts.
    -   Create a new shortcut with the command above.
    -   Assign it to a convenient hotkey (e.g., `Super`+`Shift`+`R`).

-   **For Tiling Window Managers (like i3wm)**:
    -   Edit your window manager's configuration file (e.g., `~/.config/i3/config`).
    -   Add a line to bind the key. For example, to bind `Ctrl+Alt+V`:
    ```
    # Binds Ctrl+Alt+V to toggle voice recording
    bindsym Ctrl+Mod1+v exec --no-startup-id sh -c 'kill -s USR1 $(cat /tmp/voice_input_gemini.pid)'
    ```

## Running VoiceAI on Startup

To make VoiceAI truly useful, you'll want both the background service and TUI to start automatically when you log in.

### Method 1: Using Desktop Environment Autostart

This is the most robust method for most desktop environments (GNOME, KDE, XFCE, etc.).

1.  Create a `.desktop` file in `~/.config/autostart/`, for example `voiceai-service.desktop`.
2.  Add the following content:

    ```ini
    [Desktop Entry]
    Name=VoiceAI Service
    Comment=Starts the VoiceAI transcription service
    Exec=/home/your_user/.local/bin/voiceai.gemini.live.fast.py
    Icon=audio-input-microphone
    Terminal=false
    Type=Application
    Categories=Utility;
    ```

3.  Make it executable: `chmod +x ~/.config/autostart/voiceai-service.desktop`.

This will start the background service automatically after you log in. You can then launch the TUI manually when needed with `voiceai-tui`.

### Method 2: Using tmux for Terminal-Based Management

For those who prefer terminal-based management, you can run the service inside a `tmux` session:

```bash
#!/bin/bash

# Start a new detached tmux session named "voiceai"
tmux new -d -s voiceai

# Send the command to start the voice service to the tmux session
tmux send-keys -t voiceai "export DISPLAY=:0 && ~/.local/bin/voiceai.gemini.live.fast.py" C-m
```

Save this script and run it after logging in, or add it to your shell's startup file.

## Using the Terminal User Interface (TUI)

VoiceAI includes a modern TUI built with Go that provides an intuitive interface for managing your voice recordings. The TUI allows you to:

- View a list of all your previous recordings
- Play back audio files directly from the interface
- Retry transcriptions for better accuracy
- Copy transcribed text to the clipboard with one keypress

To launch the TUI, simply run:
```bash
voiceai-tui
```

The TUI requires the background service to be running. It communicates with the service through Unix signals and file I/O to provide a seamless experience.

### TUI Keybindings

- **Space**: Start/stop recording
- **Enter**: Play selected audio file
- **c**: Copy transcription to clipboard
- **r**: Retry transcription for selected recording
- **q** or **Ctrl+C**: Quit the application

## Installation Script Details

The installation script (`install.sh`) performs the following actions:

1. Downloads the latest TUI binary to `~/.local/bin/voiceai-tui`
2. Downloads the Python transcription script to `~/.local/bin/voiceai.gemini.live.fast.py`
3. Installs required Python dependencies using pip
4. Creates a `~/.voiceai_history/` directory for storing recordings
5. Prompts for your Google Gemini API key and creates a `.env` file

The installation script is designed to be run multiple times safely - it will only update existing files and won't overwrite your `.env` file if it already exists.

## Conclusion

VoiceAI provides a complete solution for voice-to-text transcription on Linux systems. With its combination of a fast Python backend and intuitive TUI, it offers both power and ease of use. The system tray integration provides visual feedback, while the TUI offers comprehensive recording management.

By using Google's Gemini API, VoiceAI delivers accurate transcriptions that are automatically copied to your clipboard for immediate use in any application.
