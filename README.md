# Gemini Voice-to-Text Transcription Scripts

This repository contains two Python scripts for real-time voice-to-text transcription using Google's Gemini API. The transcribed text is automatically copied to the system clipboard.

-   `voiceai.gemini.py`: A headless, command-line-only version.
-   `voiceai.gemini.yad-systray.py`: A version that provides a system tray icon using `yad` to give visual feedback on the recording status.

## Features

-   **Simple Control**: Start and stop recording with a single global hotkey.
-   **Clipboard Integration**: Transcribed text is automatically copied to your clipboard using `xclip` (for X11) or `wl-copy` (for Wayland).
-   **Visual Feedback (Systray version)**: A tray icon indicates whether the script is idle (microphone) or recording (red dot).
-   **Robust**: Includes PID file management to prevent multiple instances and proper resource cleanup.
-   **Configurable**: Easily change the Gemini model, audio recording parameters, and more directly within the scripts.
-   **Retry Logic**: Automatically retries transcription requests to the Gemini API on failure.

## 1. Requirements

### System Dependencies

You need the following command-line tools installed.

-   `arecord`: For audio recording (part of ALSA).
-   `xclip`: For copying text to the clipboard on **X11**.
-   `wl-clipboard`: Provides `wl-copy` for clipboard on **Wayland**.
-   `yad`: For the system tray icon (only for the `yad-systray` version).
-   `file`: To determine the audio file's MIME type.

On **Debian/Ubuntu**, you can install them with:
```bash
sudo apt-get update
sudo apt-get install alsa-utils xclip wl-clipboard yad file
```

On **Fedora**, you can install them with:
```bash
sudo dnf install alsa-utils xclip wl-clipboard yad file
```

### Python Dependencies

The scripts require the `requests` library to communicate with the Gemini API.

Install it using `pip`:
```bash
pip install requests
```

## 2. Setup and Configuration

### Step 2.1: Get the Scripts

Clone this repository or download the script files to a directory on your computer, for example, `~/scripts/`.

```bash
# Example:
git clone <repository_url> ~/scripts/gemini-voice
cd ~/scripts/gemini-voice
```

### Step 2.2: Set Your Gemini API Key

1.  Obtain a Gemini API key from [Google AI Studio](https://aistudio.google.com/app/apikey).
2.  run 
```bash
cp .env.example .env 
```
   use your editor and then add your api key 
```bash
nano .env
```




### Step 2.3: Make Scripts Executable

Navigate to the directory where you saved the scripts and make them executable:

```bash
chmod +x voiceai.gemini.py voiceai.gemini.yad-systray.py
```

## 3. Usage

The intended way to use these scripts is to have one running in the background and trigger it with a global keyboard shortcut.

### Step 3.1: Set Up a Global Hotkey

The scripts listen for a `SIGUSR1` signal to toggle recording on and off. You need to bind a command that sends this signal to a key of your choice. There are two common ways to do this.

#### Method 1: Using `pkill` (Simpler)

This command finds the script process by its name and sends the signal. It's easy but can fail if you have other processes with `voiceai.gemini` in their name.

```bash
pkill -USR1 -f voiceai.gemini
```

#### Method 2: Using the PID File (More Robust)

This command reads the Process ID (PID) from the file created by the script (`/tmp/voice_input_gemini.pid`) and sends the signal directly to that specific process. This is the recommended method.

```bash
sh -c 'kill -s USR1 $(cat /tmp/voice_input_gemini.pid)'
```

#### Binding the Command

Go to your desktop environment's or window manager's keyboard settings and create a new custom shortcut.

-   **For Desktop Environments (GNOME, KDE, XFCE, etc.)**:
    -   Go to Settings -> Keyboard -> Custom Shortcuts.
    -   Create a new shortcut and paste one of the commands above (the PID file method is recommended).
    -   Assign it to a convenient hotkey (e.g., `Super`+`Shift`+`R`).

-   **For Tiling Window Managers (like i3wm)**:
    -   Edit your window manager's configuration file (e.g., `~/.config/i3/config`).
    -   Add a line to bind the key. For example, to bind `Ctrl+Alt+V`:
    ```
    # Binds Ctrl+Alt+V to toggle voice recording
    bindsym Ctrl+Mod1+v exec --no-startup-id sh -c 'kill -s USR1 $(cat /tmp/voice_input_gemini.pid)'
    ```

### Step 3.2: Choosing and Running the Script

Before setting up the hotkey, decide which script you want to run:

-   `voiceai.gemini.yad-systray.py`: **Recommended for X11 users.** Provides a system tray icon for visual feedback on recording status. Requires `yad` to be installed. **Note:** This script relies on `yad` for the tray icon, which may not work reliably on all **Wayland** setups.
-   `voiceai.gemini.py`: A headless version with no graphical icon. It prints status messages to the terminal or a log file. **Recommended for Wayland users.**

You must start your chosen script *before* you can use the hotkey. The rest of this guide will assume you are using the `yad-systray` version for X11 or the headless version for Wayland.

To test it, run it from your terminal:
```bash
/path/to/your/script/voiceai.gemini.yad-systray.py # For X11
# OR
/path/to/your/script/voiceai.gemini.py # For Wayland
```
If using the systray version, you should see a microphone icon appear in your system tray. Now try your hotkey. The icon should turn into a red dot while recording. Press it again to stop, and the transcribed text will be copied to your clipboard.

## 4. Running the Script on Startup

To make the script truly useful, you want it to start automatically when you log in.

### Method 1: Using Desktop Environment Autostart (Recommended)

This is the most robust method for most desktop environments (GNOME, KDE, XFCE, etc.) as it ensures the script runs correctly within your graphical session.

1.  Create a `.desktop` file in `~/.config/autostart/`, for example `voice-input-gemini.desktop`.
2.  Add the following content, making sure to use the **absolute path** to your script:

    ```ini
    [Desktop Entry]
    Name=Gemini Voice Input
    Comment=Starts the Gemini voice transcription script
    Exec=/home/your_user/scripts/gemini-voice/voiceai.gemini.yad-systray.py
    Icon=audio-input-microphone
    Terminal=false
    Type=Application
    Categories=Utility;
    ```
    **Note for Wayland users**: The `yad-systray` script may not work correctly. You should change the `Exec` line to point to the headless script: `Exec=/home/your_user/scripts/gemini-voice/voiceai.gemini.py`

3.  Make it executable: `chmod +x ~/.config/autostart/voice-input-gemini.desktop`.

This will start the script automatically after you log in.

### Alternative Method: Using tmux

For those who prefer terminal-based management, you can run the script inside a `tmux` session. This keeps the script running in the background while allowing you to easily attach to the session to view logs.

The following is an example bash script to automate this. You can run this script manually after logging in or add it to your shell's startup file (e.g., `~/.profile` or `~/.bashrc`).

**Example `start_voice_ai.sh`:**
```bash
#!/bin/bash

# Start a new detached tmux session named "voice"
tmux new -d -s voice

# (Optional) Set a specific microphone as the default source.
# This line is an EXAMPLE. Use 'pactl list sources' to find your mic's name.
# You may need to uncomment and adapt it for your system.
# pactl set-source-port alsa_input.pci-0000_00_0e.0.analog-stereo analog-input-headset-mic

# Send the command to start the voice script to the tmux session.
# - For X11 with tray icon, use the 'yad-systray' script and 'export DISPLAY=:0'.
# - For Wayland, use the headless 'voiceai.gemini.py' script (no DISPLAY needed).
#
# MAKE SURE to use the absolute path to your script.

# Example for X11:
tmux send-keys -t voice "export DISPLAY=:0 && python3 /home/user/scripts/voiceai.gemini.yad-systray.py" C-m

# Example for Wayland (uncomment to use):
# tmux send-keys -t voice "python3 /home/user/scripts/voiceai.gemini.py" C-m
```

**How to use this script:**

1.  Save the content above to a file, e.g., `~/start_voice_ai.sh`.
2.  **Edit the script:**
    *   Choose the correct `tmux send-keys` command for your session (X11 or Wayland) and comment out the other one.
    *   Replace the example path (`/home/user/scripts/...`) with the **absolute path** to the script on your machine.
    *   If needed, customize the `pactl` command for your microphone.
3.  Make the script executable: `chmod +x ~/start_voice_ai.sh`.
4.  Run it from your terminal: `~/start_voice_ai.sh`.

The script is now running in the background. You can check its output with `tmux attach -t voice`. To detach from the session (leaving the script running), press `Ctrl+b` then `d`.
