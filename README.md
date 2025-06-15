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

-   `voiceai.gemini.yad-systray.py`: **Recommended for most users.** Provides a system tray icon for visual feedback on recording status. Requires `yad` to be installed.
-   `voiceai.gemini.py`: A headless version with no graphical icon. It prints status messages to the terminal or a log file.

You must start your chosen script *before* you can use the hotkey. The rest of this guide will assume you are using the `yad-systray` version.

To test it, run it from your terminal:
```bash
/path/to/your/script/voiceai.gemini.yad-systray.py
```
You should see a microphone icon appear in your system tray. Now try your hotkey. The icon should turn into a red dot while recording. Press it again to stop, and the transcribed text will be copied to your clipboard.

## 4. Automatic Startup on Login

### Method 1: Using Desktop Environment Autostart (Recommended for Wayland)

Using `@reboot` with `crontab` can be unreliable on Wayland for applications that need to interact with your graphical session (like `yad` or `wl-copy`). The environment variables (`WAYLAND_DISPLAY`, `XDG_RUNTIME_DIR`) required for them to work are often not available in the `cron` environment.

A more robust method is to use your desktop environment's autostart settings.

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
3.  Make it executable: `chmod +x ~/.config/autostart/voice-input-gemini.desktop`.

This will start the script correctly within your user session after you log in.

### Method 2: Using `crontab` (For X11 or non-desktop setups)

To make this tool truly useful, you want it to start automatically when you log in. You can achieve this using `crontab`.

1.  Open your user's crontab for editing:
    ```bash
    crontab -e
    ```

2.  Add the following line to the bottom of the file. **You must replace `/path/to/your/script/` with the actual, absolute path to the script.**

    ```crontab
    @reboot export DISPLAY=:0 && /path/to/your/script/voiceai.gemini.yad-systray.py >> /tmp/voice_input_gemini.log 2>&1
    ```

*(Note: If you chose to use the headless `voiceai.gemini.py` script, change the filename in the command accordingly. The `export DISPLAY=:0` part is only necessary for the `yad-systray` version but does no harm for the headless one.)*

1.  Open your user's crontab for editing:
    ```bash
    crontab -e
    ```

2.  Add the following line to the bottom of the file. **You must replace `/path/to/your/script/` with the actual, absolute path to the script.**

    ```crontab
    @reboot export DISPLAY=:0 && /path/to/your/script/voiceai.gemini.yad-systray.py >> /tmp/voice_input_gemini.log 2>&1
    ```

