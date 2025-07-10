package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

// GeminiRequest defines the structure for the JSON payload sent to the Gemini REST API.
type GeminiRequest struct {
	Contents         []Content         `json:"contents"`
	GenerationConfig GenerationConfig `json:"generationConfig"`
}

// Content holds the parts of the request.
type Content struct {
	Parts []Part `json:"parts"`
}

// Part can be either text or inline data (like audio).
type Part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"`
}

// InlineData represents the raw media data.
type InlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

// GenerationConfig specifies the content generation parameters.
type GenerationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

// GeminiResponse defines the structure for the JSON response from the Gemini REST API.
type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Config holds all the application configuration.
type Config struct {
	APIKey             string
	PrimaryModel       string
	FallbackModel      string
	PromptText         string
	MaxSegmentSizeMB   float64
	SpeedMultiplier    float64
	PIDFile            string
	AudioTempFile      string
	ARecordDevice      string
	ARecordFormat      string
	ARecordRate        string
	ARecordChannels    string
}

// AppState holds the application's state.
type AppState struct {
	config     *Config
	httpClient *http.Client
	ctx        context.Context
	useYAD     bool
	yadCmd     *exec.Cmd
	yadStdin   io.WriteCloser
}

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		logMessage("No .env file found, using environment variables")
	} else {
		logMessage("Loaded configuration from .env file")
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
		APIKey:             getEnv("GEMINI_API_KEY", ""),
		PrimaryModel:       getEnv("GEMINI_MODEL_NAME", "gemini-2.5-flash"),
		FallbackModel:      getEnv("GEMINI_FALLBACK_MODEL", "gemini-2.0-flash"),
		PromptText:         getEnv("GEMINI_PROMPT_TEXT", "Transcribe this audio recording."),
		MaxSegmentSizeMB:   getEnvFloat("MAX_SEGMENT_SIZE_MB", 2.0),
		SpeedMultiplier:    getEnvFloat("SPEED_MULTIPLIER", 2.0),
		PIDFile:            "/tmp/voice_input_gemini.pid",
		AudioTempFile:      "/tmp/voice_input_audio_go.wav",
		ARecordDevice:      getEnv("ARECORD_DEVICE", "default"),
		ARecordFormat:      getEnv("ARECORD_FORMAT", "S16_LE"),
		ARecordRate:        getEnv("ARECORD_RATE", "16000"),
		ARecordChannels:    getEnv("ARECORD_CHANNELS", "1"),
	}

	if config.APIKey == "" {
		logMessage("ERROR: GEMINI_API_KEY environment variable not set.")
		logMessage("Please create a .env file with GEMINI_API_KEY=\"YOUR_API_KEY\"")
		os.Exit(1)
	}

	logMessage("Gemini API key configured for ADVANCED FAST processing.")

	app := &AppState{
		config:     config,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		ctx:        context.Background(),
		useYAD:     useYAD,
	}

	// Write PID file
	if err := writePIDFile(config.PIDFile); err != nil {
		log.Fatalf("Failed to write PID file: %v", err)
	}
	defer os.Remove(config.PIDFile)

	// Start YAD if enabled
	if useYAD {
		if app.startYAD() {
			logMessage("Yad notification icon started.")
			logMessage("Tray icon active.")
		} else {
			logMessage("WARNING: Tray icon is INACTIVE.")
		}
	} else {
		logMessage("YAD disabled (headless mode).")
	}

	logMessage(fmt.Sprintf("ADVANCED FAST Voice AI script started (PID %d). Send SIGUSR1 to toggle recording.", os.Getpid()))
	logMessage("Features: Parallel processing, Audio segmentation, Fallback model, Speed adjustment")
	logMessage(fmt.Sprintf("Config: Max segment size: %.1fMB, Speed multiplier: %.1fx", app.config.MaxSegmentSizeMB, app.config.SpeedMultiplier))
	logMessage(fmt.Sprintf("Models: %s (primary), %s (fallback)", config.PrimaryModel, config.FallbackModel))
	logMessage("Send SIGUSR1 to toggle recording")

	// Setup signal handlers
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)

	for sig := range sigChan {
		switch sig {
		case syscall.SIGUSR1:
			app.toggleRecording()
		case syscall.SIGTERM, syscall.SIGINT:
			logMessage(fmt.Sprintf("Received signal %v. Exiting gracefully.", sig))
			app.cleanup()
			return
		}
	}
}

func (app *AppState) startYAD() bool {
	logMessage("Starting yad notification icon...")
	if _, err := exec.LookPath("yad"); err != nil {
		logMessage("ERROR: Command 'yad' not found. Please install it.")
		return false
	}

	cmd := exec.Command("yad", "--notification",
		"--image=audio-input-microphone",
		"--text=Voice Input: Idle (Press keybind to record)",
		"--command=:",
		"--listen")

	// Get stdin pipe for sending commands
	stdin, err := cmd.StdinPipe()
	if err != nil {
		logMessage(fmt.Sprintf("ERROR: Could not get stdin pipe for yad: %v", err))
		return false
	}

	if err := cmd.Start(); err != nil {
		logMessage(fmt.Sprintf("ERROR: yad failed to start: %v", err))
		return false
	}

	app.yadCmd = cmd
	app.yadStdin = stdin

	return true
}

func (app *AppState) cleanup() {
	logMessage("Cleaning up resources...")
	if app.yadCmd != nil && app.yadCmd.Process != nil {
		logMessage("Stopping yad notification icon...")
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

// Transcription function using REST API with fallback
func (app *AppState) transcribeAudio(audioData []byte) (string, error) {
	// Try primary model first
	text, err := app.transcribeWithRest(audioData, app.config.PrimaryModel)
	if err == nil && text != "" {
		return text, nil
	}
	
	// If primary failed, try fallback model
	logMessage(fmt.Sprintf("Primary model (%s) failed, trying fallback model (%s)...", app.config.PrimaryModel, app.config.FallbackModel))
	text, err = app.transcribeWithRest(audioData, app.config.FallbackModel)
	if err == nil && text != "" {
		return text, nil
	}
	
	logMessage("Both primary and fallback models failed")
	return "", fmt.Errorf("all transcription attempts failed")
}

// Recording functionality
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

		// Process with ADVANCED features like Python version
		transcript := app.processAudioAdvanced(wavData)

		if transcript != "" {
			logMessage(fmt.Sprintf("Final transcription: '%s'", transcript))
			if app.copyToClipboard(transcript) {
				app.cleanupTempAudio()
			} else {
				app.saveAudioForDebugging(wavData)
			}
		} else {
			logMessage("All transcription attempts failed")
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

	var cmdName string
	if strings.Contains(sessionType, "wayland") {
		cmdName = "wl-copy"
		cmd = exec.Command(cmdName)
	} else {
		cmdName = "xclip"
		cmd = exec.Command(cmdName, "-selection", "clipboard")
	}

	logMessage(fmt.Sprintf("Copying to clipboard using '%s'...", cmdName))
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		logMessage(fmt.Sprintf("Failed to copy to clipboard: %v", err))
		return false
	} else {
		logMessage("Copied to clipboard.")
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
	if app.yadStdin == nil {
		return
	}
	// Send command to YAD via stdin pipe
	_, err := app.yadStdin.Write([]byte(command + "\n"))
	if err != nil {
		logMessage(fmt.Sprintf("Failed to send YAD command: %v", err))
	}
}

func (app *AppState) saveAudioForDebugging(wavData []byte) {
	if err := os.WriteFile(app.config.AudioTempFile, wavData, 0644); err != nil {
		logMessage(fmt.Sprintf("Failed to save audio for debugging: %v", err))
	} else {
		logMessage(fmt.Sprintf("Audio saved for debugging: %s", app.config.AudioTempFile))
	}
}

func (app *AppState) cleanupTempAudio() {
	if _, err := os.Stat(app.config.AudioTempFile); err == nil {
		if err := os.Remove(app.config.AudioTempFile); err == nil {
			logMessage(fmt.Sprintf("Removed: %s", app.config.AudioTempFile))
		}
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

// ADVANCED PROCESSING - Same as Python version
func (app *AppState) processAudioAdvanced(wavData []byte) string {
	audioSizeMB := float64(len(wavData)) / (1024 * 1024)
	logMessage(fmt.Sprintf("Audio size: %.2f MB", audioSizeMB))

	// Strategy selection based on size (SAME AS PYTHON)
	if audioSizeMB <= app.config.MaxSegmentSizeMB {
		// Small audio - direct processing
		logMessage("Using direct processing for small audio")
		transcript, _ := app.transcribeAudio(wavData)
		return transcript
	} else {
		// Large audio - advanced processing
		logMessage(fmt.Sprintf("Large audio detected (%.2f MB). Using advanced processing...", audioSizeMB))
		return app.processLargeAudio(wavData, audioSizeMB)
	}
}

func (app *AppState) processLargeAudio(wavData []byte, audioSizeMB float64) string {
	// Save to temp file for processing
	tempFile := app.config.AudioTempFile
	if err := os.WriteFile(tempFile, wavData, 0644); err != nil {
		logMessage(fmt.Sprintf("Failed to write temp file: %v", err))
		return ""
	}
	defer os.Remove(tempFile)

	// Apply speed if very large
	processFile := tempFile
	if audioSizeMB > app.config.MaxSegmentSizeMB*2 {
		logMessage(fmt.Sprintf("Very large audio. Applying %.1fx speed...", app.config.SpeedMultiplier))
		speedFile := tempFile + "_speed.wav"
		if app.speedUpAudio(tempFile, speedFile) {
			processFile = speedFile
			defer os.Remove(speedFile)
			logMessage("Audio speed increased successfully")
		} else {
			logMessage("Speed increase failed, continuing with original")
		}
	}

	// Read the file that will be processed (original or sped up)
	audioToProcess, err := os.ReadFile(processFile)
	if err != nil {
		logMessage(fmt.Sprintf("Failed to read audio file for transcription: %v", err))
		// Fallback to original in-memory data if read fails
		audioToProcess = wavData
	}

	logMessage("Processing large audio directly...")
	transcript, err := app.transcribeAudio(audioToProcess)
	if err != nil {
		logMessage(fmt.Sprintf("Transcription of large audio failed: %v", err))
		return ""
	}
	return transcript
}

func (app *AppState) speedUpAudio(inputFile, outputFile string) bool {
	cmd := exec.Command("ffmpeg", "-i", inputFile, "-filter:a",
		fmt.Sprintf("atempo=%.1f", app.config.SpeedMultiplier), "-y", outputFile)

	if err := cmd.Run(); err != nil {
		logMessage(fmt.Sprintf("Error speeding up audio: %v", err))
		return false
	}
	return true
}

func (app *AppState) transcribeWithRest(audioData []byte, model string) (string, error) {
	logMessage(fmt.Sprintf("Sending optimized request to Gemini API (%s)...", model))
	start := time.Now()

	// Base64 encode audio data
	base64Audio := base64.StdEncoding.EncodeToString(audioData)

	// Create request payload
	payload := GeminiRequest{
		Contents: []Content{
			{
				Parts: []Part{
					{Text: app.config.PromptText},
					{InlineData: &InlineData{
						MIMEType: "audio/wav",
						Data:     base64Audio,
					}},
				},
			},
		},
		GenerationConfig: GenerationConfig{
			Temperature:     0.1,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %v", err)
	}

	// Create request
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, app.config.APIKey)
	req, err := http.NewRequestWithContext(app.ctx, "POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Send request
	resp, err := app.httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		logMessage(fmt.Sprintf("API request failed after %.2fs: %v", duration.Seconds(), err))
		return "", err
	}
	defer resp.Body.Close()

	// Handle response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			logMessage(fmt.Sprintf("Rate limit hit with %s", model))
		}
		return "", fmt.Errorf("API request failed with status %s: %s", resp.Status, string(body))
	}

	logMessage(fmt.Sprintf("API response received in %.2fs", duration.Seconds()))

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		logMessage(fmt.Sprintf("Failed to parse JSON response from %s: %v", model, err))
		logMessage(fmt.Sprintf("Raw response body: %s", string(body)))
		return "", fmt.Errorf("failed to unmarshal JSON response: %v", err)
	}

	// Debug: Log the response structure
	logMessage(fmt.Sprintf("Response from %s - Candidates: %d", model, len(geminiResp.Candidates)))
	if len(geminiResp.Candidates) > 0 {
		logMessage(fmt.Sprintf("First candidate has %d parts", len(geminiResp.Candidates[0].Content.Parts)))
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		text := geminiResp.Candidates[0].Content.Parts[0].Text
		if text == "" {
			logMessage(fmt.Sprintf("Empty text in response from %s", model))
			return "", fmt.Errorf("no text found in response")
		}
		return strings.TrimSpace(text), nil
	}

	// Log the full response for debugging
	logMessage(fmt.Sprintf("Unexpected response structure from %s: %s", model, string(body)))
	return "", fmt.Errorf("unexpected response structure from API")
}