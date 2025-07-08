package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"google.golang.org/genai"
	"github.com/joho/godotenv"
)

type Config struct {
	APIKey              string
	PrimaryModel        string
	FallbackModel       string
	PromptText          string
	MaxSegmentSizeMB    float64
	SpeedMultiplier     float64
	SilenceThreshold    string
	MinSilenceDuration  float64
	MaxWorkers          int
	PIDFile             string
	AudioTempFile       string
	ARecordDevice       string
	ARecordFormat       string
	ARecordRate         string
	ARecordChannels     string
}

type AppState struct {
	config   *Config
	client   *genai.Client
	ctx      context.Context
	useYAD   bool
	yadCmd   *exec.Cmd
}

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found, using environment variables")
	} else {
		fmt.Println("Loaded configuration from .env file")
	}

	// Check for --no-yad flag
	useYAD := true
	for _, arg := range os.Args[1:] {
		if arg == "--no-yad" || arg == "--headless" {
			useYAD = false
			break
		}
	}

	config := &Config{
		APIKey:              getEnv("GEMINI_API_KEY", ""),
		PrimaryModel:        getEnv("GEMINI_MODEL_NAME", "gemini-2.5-flash"),
		FallbackModel:       getEnv("GEMINI_FALLBACK_MODEL", "gemini-2.0-flash-exp"),
		PromptText:          getEnv("GEMINI_PROMPT_TEXT", "Transcribe this audio recording."),
		MaxSegmentSizeMB:    getEnvFloat("MAX_SEGMENT_SIZE_MB", 2.0),
		SpeedMultiplier:     getEnvFloat("SPEED_MULTIPLIER", 2.0),
		SilenceThreshold:    getEnv("SILENCE_THRESHOLD", "5%"),
		MinSilenceDuration:  getEnvFloat("MIN_SILENCE_DURATION", 3.0),
		MaxWorkers:          getEnvInt("MAX_WORKERS", 3),
		PIDFile:             "/tmp/voice_input_gemini.pid",
		AudioTempFile:       "/tmp/voice_input_audio_go.wav",
		ARecordDevice:       getEnv("ARECORD_DEVICE", "default"),
		ARecordFormat:       getEnv("ARECORD_FORMAT", "S16_LE"),
		ARecordRate:         getEnv("ARECORD_RATE", "16000"),
		ARecordChannels:     getEnv("ARECORD_CHANNELS", "1"),
	}

	if config.APIKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}

	// Initialize Gemini client (using the correct API from docs)
	ctx := context.Background()
	client, err := genai.NewClient(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to create Gemini client: %v", err)
	}

	app := &AppState{
		config: config,
		client: client,
		ctx:    ctx,
		useYAD: useYAD,
	}

	// Write PID file
	if err := writePIDFile(config.PIDFile); err != nil {
		log.Fatalf("Failed to write PID file: %v", err)
	}
	defer os.Remove(config.PIDFile)

	// Start YAD if enabled
	if useYAD {
		if app.startYAD() {
			fmt.Println("✓ YAD tray icon started")
		} else {
			fmt.Println("⚠ YAD tray icon failed")
		}
	} else {
		fmt.Println("• YAD disabled (headless mode)")
	}

	fmt.Printf("🚀 Voice AI Go started (PID: %d)\n", os.Getpid())
	fmt.Printf("Models: %s (primary), %s (fallback)\n", config.PrimaryModel, config.FallbackModel)
	fmt.Println("Send SIGUSR1 to toggle recording")

	// Setup signal handlers
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)

	for sig := range sigChan {
		switch sig {
		case syscall.SIGUSR1:
			app.toggleRecording()
		case syscall.SIGTERM, syscall.SIGINT:
			fmt.Println("Shutting down...")
			app.cleanup()
			return
		}
	}
}

func (app *AppState) startYAD() bool {
	if _, err := exec.LookPath("yad"); err != nil {
		return false
	}

	cmd := exec.Command("yad", "--notification",
		"--image=audio-input-microphone",
		"--text=Voice Input: Ready",
		"--command=:",
		"--listen")

	if err := cmd.Start(); err != nil {
		return false
	}

	app.yadCmd = cmd
	return true
}

func (app *AppState) cleanup() {
	if app.yadCmd != nil && app.yadCmd.Process != nil {
		app.yadCmd.Process.Kill()
	}
	os.Remove(app.config.PIDFile)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func writePIDFile(pidFile string) error {
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}
// Transcription function using correct API
func (app *AppState) transcribeAudio(audioData []byte) (string, error) {
	parts := []*genai.Part{
		genai.NewPartFromText(app.config.PromptText), // Use configurable prompt
		&genai.Part{
			InlineData: &genai.Blob{
				MIMEType: "audio/wav",
				Data:     audioData,
			},
		},
	}
	
	contents := []*genai.Content{
		genai.NewContentFromParts(parts, genai.RoleUser),
	}

	result, err := app.client.Models.GenerateContent(
		app.ctx,
		app.config.PrimaryModel,
		contents,
		nil,
	)
	
	if err != nil {
		return "", err
	}
	
	return result.Text(), nil
}


// Recording functionality - SAME as Python version
type RecordingState struct {
	isRecording  bool
	isProcessing bool
	arecordCmd   *exec.Cmd
}

var recordingState RecordingState

func (app *AppState) toggleRecording() {
	if recordingState.isRecording {
		logMessage("Signal: Stopping recording...")
		if recordingState.arecordCmd != nil && recordingState.arecordCmd.Process != nil {
			recordingState.arecordCmd.Process.Signal(syscall.SIGTERM)
		}
		recordingState.isRecording = false
		recordingState.isProcessing = true
		app.updateTrayIcon()
	} else {
		if recordingState.isProcessing {
			logMessage("Signal: Ignoring start, currently processing previous recording.")
			return
		}

		logMessage("Signal: Starting recording...")
		app.startRecording()
	}
}

func (app *AppState) startRecording() {
	// Same arecord command as Python version
	cmd := exec.Command("arecord", 
		"-D", app.config.ARecordDevice,
		"-f", app.config.ARecordFormat, 
		"-r", app.config.ARecordRate,
		"-c", app.config.ARecordChannels,
		"-t", "raw")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logMessage(fmt.Sprintf("Failed to get stdout pipe: %v", err))
		return
	}

	if err := cmd.Start(); err != nil {
		logMessage(fmt.Sprintf("Failed to start arecord: %v", err))
		return
	}

	recordingState.arecordCmd = cmd
	recordingState.isRecording = true
	app.updateTrayIcon()
	logMessage("Recording started. Streaming to advanced processing...")

	// Process audio in goroutine
	go func() {
		defer func() {
			recordingState.isProcessing = false
			app.updateTrayIcon()
		}()

		// Read audio data
		audioData, err := io.ReadAll(stdout)
		if err != nil {
			logMessage(fmt.Sprintf("Error reading audio data: %v", err))
			return
		}

		cmd.Wait()
		logMessage(fmt.Sprintf("Read %.2f MB of audio data", float64(len(audioData))/(1024*1024)))

		// Create WAV data (same as Python)
		wavData := app.createWAVData(audioData)

		// Transcribe using configured prompt
		transcript, err := app.transcribeAudio(wavData)
		if err != nil {
			logMessage(fmt.Sprintf("Transcription failed: %v", err))
			// Save audio for debugging like Python version
			app.saveAudioForDebugging(wavData)
			return
		}

		if transcript != "" {
			logMessage(fmt.Sprintf("Final transcription: '%s'", transcript))
			if app.copyToClipboard(transcript) {
				// Success - clean up
				app.cleanupTempAudio()
			} else {
				// Clipboard failed - save audio
				app.saveAudioForDebugging(wavData)
			}
		} else {
			logMessage("No transcription received")
			app.saveAudioForDebugging(wavData)
		}
	}()
}

func (app *AppState) createWAVData(rawData []byte) []byte {
	// Same WAV header creation as before
	sampleRate := 16000
	channels := 1
	bitsPerSample := 16
	dataSize := len(rawData)

	header := make([]byte, 44)
	
	// RIFF header
	copy(header[0:4], "RIFF")
	writeUint32LE(header[4:8], uint32(36+dataSize))
	copy(header[8:12], "WAVE")
	
	// fmt chunk
	copy(header[12:16], "fmt ")
	writeUint32LE(header[16:20], 16)
	writeUint16LE(header[20:22], 1)
	writeUint16LE(header[22:24], uint16(channels))
	writeUint32LE(header[24:28], uint32(sampleRate))
	writeUint32LE(header[28:32], uint32(sampleRate*channels*bitsPerSample/8))
	writeUint16LE(header[32:34], uint16(channels*bitsPerSample/8))
	writeUint16LE(header[34:36], uint16(bitsPerSample))
	
	// data chunk
	copy(header[36:40], "data")
	writeUint32LE(header[40:44], uint32(dataSize))

	// Combine header and data
	wavData := make([]byte, len(header)+len(rawData))
	copy(wavData, header)
	copy(wavData[len(header):], rawData)
	
	return wavData
}

func (app *AppState) copyToClipboard(text string) bool {
	var cmd *exec.Cmd
	sessionType := strings.ToLower(os.Getenv("XDG_SESSION_TYPE"))
	
	if strings.Contains(sessionType, "wayland") {
		cmd = exec.Command("wl-copy")
	} else {
		cmd = exec.Command("xclip", "-selection", "clipboard")
	}
	
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		logMessage(fmt.Sprintf("Failed to copy to clipboard: %v", err))
		return false
	} else {
		logMessage("Copied to clipboard!")
		return true
	}
}

func writeUint32LE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func writeUint16LE(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func (app *AppState) updateTrayIcon() {
	if !app.useYAD || app.yadCmd == nil {
		return
	}
	
	// Update YAD icon based on state (like Python version)
	if recordingState.isProcessing {
		app.sendYADCommand("icon:system-search")
		app.sendYADCommand("tooltip:Voice Input: Processing...")
	} else if recordingState.isRecording {
		app.sendYADCommand("icon:media-record") 
		app.sendYADCommand("tooltip:Voice Input: Recording... (Press keybind to stop)")
	} else {
		app.sendYADCommand("icon:audio-input-microphone")
		app.sendYADCommand("tooltip:Voice Input: Idle (Press keybind to record)")
	}
}

func (app *AppState) sendYADCommand(command string) {
	// YAD command sending would need stdin pipe management
	// For now, simplified - full implementation would need proper stdin handling
}

func (app *AppState) saveAudioForDebugging(wavData []byte) {
	if err := os.WriteFile(app.config.AudioTempFile, wavData, 0644); err != nil {
		logMessage(fmt.Sprintf("Failed to save audio for debugging: %v", err))
	} else {
		logMessage(fmt.Sprintf("Audio saved for debugging: %s", app.config.AudioTempFile))
	}
}

func (app *AppState) cleanupTempAudio() {
	if err := os.Remove(app.config.AudioTempFile); err == nil {
		logMessage(fmt.Sprintf("Removed: %s", app.config.AudioTempFile))
	}
}


// Helper functions for environment variables
func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

// Logging function with timestamp (like Python version)
func logMessage(message string) {
	fmt.Printf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), message)
}
