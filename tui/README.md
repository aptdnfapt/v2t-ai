# VoiceAI TUI

A terminal-based user interface for viewing VoiceAI transcription history, built with Go and Charm's Bubble Tea library.

## Features

- Beautiful terminal-based interface
- View list of recent transcriptions with previews
- Full text view when selecting a transcription
- Audio playback functionality
- Retry transcription feature
- Keyboard navigation
- Refresh capability

## Installation

Make sure you have Go installed, then run:

```bash
go mod tidy
go build -o voiceai-tui
```

## Usage

Run the TUI application:

```bash
./voiceai-tui
```

## Controls

### List View
- **↑/↓ arrows**: Navigate the list
- **Enter**: View full transcription text
- **p**: Play audio recording
- **r**: Retry transcription
- **R**: Refresh recordings list
- **q/Ctrl+C**: Quit the application

### Text View
- **Esc/q**: Return to list view

## Dependencies

- Go 1.16+
- Charm Bubble Tea library
- `aplay` for audio playback (part of alsa-utils)
- `ffmpeg` for audio processing (for retry transcription feature)

## Notes

The retry transcription feature currently shows a placeholder message. In a full implementation, it would:
1. Call the Gemini API with the selected audio file
2. Save the new transcription
3. Update the text file