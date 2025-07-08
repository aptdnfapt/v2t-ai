package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// Configuration
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

// Global state
type AppState struct {
	config        *Config
	isRecording   bool
	isProcessing  bool
	arecordCmd    *exec.Cmd
	mu            sync.RWMutex
	client        *genai.Client
	ctx           context.Context
}

func main() {
	// Load configuration
	config := loadConfig()
	
	// Initialize Gemini client
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(config.APIKey))
	if err != nil {
		log.Fatalf("Failed to create Gemini client: %v", err)
	}
	defer client.Close()

	// Initialize app state
	app := &AppState{
		config: config,
		client: client,
		ctx:    ctx,
	}

	// Setup signal handlers
	app.setupSignalHandlers()

	// Check dependencies
	if !app.checkDependencies() {
		log.Fatal("Missing required dependencies")
	}

	// Write PID file
	if err := app.writePIDFile(); err != nil {
		log.Fatalf("Failed to write PID file: %v", err)
	}
	defer app.cleanup()

	logMessage("🚀 ADVANCED FAST Voice AI (Go) started")
	logMessage(fmt.Sprintf("Features: Parallel processing, Smart load balancing, Instant fallback"))
	logMessage(fmt.Sprintf("Models: %s (primary), %s (fallback)", config.PrimaryModel, config.FallbackModel))
	logMessage(fmt.Sprintf("Config: Max segment: %.1fMB, Speed: %.1fx, Workers: %d", 
		config.MaxSegmentSizeMB, config.SpeedMultiplier, config.MaxWorkers))
	logMessage(fmt.Sprintf("PID: %d - Send SIGUSR1 to toggle recording", os.Getpid()))

	// Main loop
	app.mainLoop()
}

func loadConfig() *Config {
	return &Config{
		APIKey:              getEnvOrDefault("GEMINI_API_KEY", ""),
		PrimaryModel:        getEnvOrDefault("GEMINI_MODEL_NAME", "gemini-2.5-flash"),
		FallbackModel:       getEnvOrDefault("GEMINI_FALLBACK_MODEL", "gemini-2.0-flash-exp"),
		PromptText:          getEnvOrDefault("GEMINI_PROMPT_TEXT", "Transcribe this audio accurately and quickly."),
		MaxSegmentSizeMB:    getEnvFloat("MAX_SEGMENT_SIZE_MB", 2.0),
		SpeedMultiplier:     getEnvFloat("SPEED_MULTIPLIER", 2.0),
		SilenceThreshold:    getEnvOrDefault("SILENCE_THRESHOLD", "5%"),
		MinSilenceDuration:  getEnvFloat("MIN_SILENCE_DURATION", 3.0),
		MaxWorkers:          getEnvInt("MAX_WORKERS", 3),
		PIDFile:             "/tmp/voice_input_gemini.pid",
		AudioTempFile:       "/tmp/voice_input_audio_go.wav",
		ARecordDevice:       getEnvOrDefault("ARECORD_DEVICE", "default"),
		ARecordFormat:       getEnvOrDefault("ARECORD_FORMAT", "S16_LE"),
		ARecordRate:         getEnvOrDefault("ARECORD_RATE", "16000"),
		ARecordChannels:     getEnvOrDefault("ARECORD_CHANNELS", "1"),
	}
}

func (app *AppState) setupSignalHandlers() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		for sig := range sigChan {
			switch sig {
			case syscall.SIGUSR1:
				app.toggleRecording()
			case syscall.SIGTERM, syscall.SIGINT:
				logMessage("Received exit signal. Cleaning up...")
				app.cleanup()
				os.Exit(0)
			}
		}
	}()
}

func (app *AppState) toggleRecording() {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.isRecording {
		logMessage("Signal: Stopping recording...")
		if app.arecordCmd != nil && app.arecordCmd.Process != nil {
			app.arecordCmd.Process.Signal(syscall.SIGTERM)
		}
		app.isRecording = false
		app.isProcessing = true
	} else {
		if app.isProcessing {
			logMessage("Signal: Ignoring start, currently processing previous recording.")
			return
		}

		logMessage("Signal: Starting recording...")
		app.startRecording()
	}
}

func (app *AppState) startRecording() {
	// Command to record raw PCM audio
	args := []string{
		"-D", app.config.ARecordDevice,
		"-f", app.config.ARecordFormat,
		"-r", app.config.ARecordRate,
		"-c", app.config.ARecordChannels,
		"-t", "raw",
	}

	app.arecordCmd = exec.Command("arecord", args...)
	
	// Start recording in a goroutine
	go func() {
		stdout, err := app.arecordCmd.StdoutPipe()
		if err != nil {
			logMessage(fmt.Sprintf("Failed to get stdout pipe: %v", err))
			return
		}

		if err := app.arecordCmd.Start(); err != nil {
			logMessage(fmt.Sprintf("Failed to start arecord: %v", err))
			return
		}

		app.isRecording = true
		logMessage("Recording started. Streaming to advanced processing...")

		// Read audio data
		audioData, err := io.ReadAll(stdout)
		if err != nil {
			logMessage(fmt.Sprintf("Error reading audio data: %v", err))
			return
		}

		// Wait for process to finish
		app.arecordCmd.Wait()

		logMessage(fmt.Sprintf("Read %.2f MB of audio data", float64(len(audioData))/(1024*1024)))

		// Process audio with advanced features
		go app.processAudioAdvanced(audioData)
	}()
}

func (app *AppState) processAudioAdvanced(audioData []byte) {
	defer func() {
		app.mu.Lock()
		app.isProcessing = false
		app.mu.Unlock()
	}()

	audioSizeMB := float64(len(audioData)) / (1024 * 1024)
	logMessage(fmt.Sprintf("Audio size: %.2f MB", audioSizeMB))

	// Create WAV data
	wavData := app.createWAVData(audioData)

	var transcribedText string
	var err error

	if audioSizeMB <= app.config.MaxSegmentSizeMB {
		// Small audio - direct processing
		logMessage("Using direct processing for small audio")
		transcribedText, err = app.transcribeSegmentSmart(wavData, 0, false)
	} else {
		// Large audio - advanced processing
		logMessage(fmt.Sprintf("Large audio detected (%.2f MB). Using advanced processing...", audioSizeMB))
		transcribedText, err = app.processLargeAudio(wavData, audioSizeMB)
	}

	if err != nil {
		logMessage(fmt.Sprintf("Error in audio processing: %v", err))
		app.saveAudioForDebugging(wavData)
		return
	}

	if transcribedText != "" {
		logMessage(fmt.Sprintf("Final transcription: '%s'", transcribedText))
		if app.copyToClipboard(transcribedText) {
			app.cleanupTempAudio()
		} else {
			app.saveAudioForDebugging(wavData)
		}
	} else {
		logMessage("No transcription received")
		app.saveAudioForDebugging(wavData)
	}
}

func (app *AppState) processLargeAudio(wavData []byte, audioSizeMB float64) (string, error) {
	// Save to temp file for processing
	tempFile := app.config.AudioTempFile
	if err := os.WriteFile(tempFile, wavData, 0644); err != nil {
		return "", fmt.Errorf("failed to write temp file: %v", err)
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
		}
	}

	// Split audio by silence
	segments, err := app.splitAudioBySilence(processFile)
	if err != nil || len(segments) == 0 {
		logMessage("Audio splitting failed, using direct processing...")
		return app.transcribeSegmentSmart(wavData, 0, false)
	}

	defer func() {
		for _, segment := range segments {
			os.Remove(segment)
		}
	}()

	logMessage(fmt.Sprintf("Split audio into %d segments", len(segments)))

	// Parallel transcription with smart load balancing
	return app.transcribeSegmentsParallel(segments)
}

func (app *AppState) transcribeSegmentsParallel(segments []string) (string, error) {
	logMessage(fmt.Sprintf("Starting parallel transcription of %d segments...", len(segments)))

	type segmentResult struct {
		index int
		text  string
		err   error
	}

	resultChan := make(chan segmentResult, len(segments))
	semaphore := make(chan struct{}, app.config.MaxWorkers)

	// Start all transcription goroutines
	var wg sync.WaitGroup
	for i, segment := range segments {
		wg.Add(1)
		go func(idx int, segmentFile string) {
			defer wg.Done()
			semaphore <- struct{}{} // Acquire
			defer func() { <-semaphore }() // Release

			text, err := app.transcribeSegmentSmart(nil, idx, true, segmentFile)
			resultChan <- segmentResult{index: idx, text: text, err: err}
		}(i, segment)
	}

	// Close result channel when all goroutines finish
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	results := make(map[int]string)
	for result := range resultChan {
		if result.err != nil {
			logMessage(fmt.Sprintf("Segment %d failed: %v", result.index+1, result.err))
		} else if result.text != "" {
			results[result.index] = result.text
		}
	}

	// Combine results in order
	var transcriptParts []string
	for i := 0; i < len(segments); i++ {
		if text, exists := results[i]; exists {
			transcriptParts = append(transcriptParts, text)
		}
	}

	if len(transcriptParts) > 0 {
		combined := strings.Join(transcriptParts, " ")
		logMessage(fmt.Sprintf("Combined transcription from %d segments", len(transcriptParts)))
		return combined, nil
	}

	return "", fmt.Errorf("no successful transcriptions from any segment")
}

func (app *AppState) transcribeSegmentSmart(wavData []byte, segmentIndex int, useLoadBalancing bool, segmentFile ...string) (string, error) {
	var audioData []byte
	var err error

	// Read audio data
	if len(segmentFile) > 0 && segmentFile[0] != "" {
		audioData, err = os.ReadFile(segmentFile[0])
		if err != nil {
			return "", fmt.Errorf("failed to read segment file: %v", err)
		}
	} else {
		audioData = wavData
	}

	// Smart model selection
	var primaryModel, fallbackModel string
	if useLoadBalancing {
		// Load balance: alternate between models
		if segmentIndex%2 == 0 {
			primaryModel = app.config.PrimaryModel
			fallbackModel = app.config.FallbackModel
		} else {
			primaryModel = app.config.FallbackModel
			fallbackModel = app.config.PrimaryModel
		}
	} else {
		primaryModel = app.config.PrimaryModel
		fallbackModel = app.config.FallbackModel
	}

	// Try primary model first
	logMessage(fmt.Sprintf("Transcribing segment %d with %s (primary)...", segmentIndex+1, primaryModel))
	text, err := app.transcribeWithModel(audioData, primaryModel)
	if err == nil && text != "" {
		return text, nil
	}

	// If primary failed, try fallback immediately
	if err != nil {
		logMessage(fmt.Sprintf("Primary model failed for segment %d: %v", segmentIndex+1, err))
	}
	logMessage(fmt.Sprintf("Trying fallback model %s for segment %d...", fallbackModel, segmentIndex+1))
	
	text, err = app.transcribeWithModel(audioData, fallbackModel)
	if err != nil {
		return "", fmt.Errorf("both models failed: %v", err)
	}

	return text, nil
}

func (app *AppState) transcribeWithModel(audioData []byte, modelName string) (string, error) {
	start := time.Now()

	// Get the model
	model := app.client.GenerativeModel(modelName)
	model.SetTemperature(0.1)
	model.SetMaxOutputTokens(1000)

	// Create the request parts
	parts := []genai.Part{
		genai.Text(app.config.PromptText),
		genai.Blob{
			MIMEType: "audio/wav",
			Data:     audioData,
		},
	}

	// Generate content
	result, err := model.GenerateContent(app.ctx, parts...)
	
	duration := time.Since(start)
	
	if err != nil {
		return "", fmt.Errorf("API request failed: %v", err)
	}

	if len(result.Candidates) == 0 {
		return "", fmt.Errorf("no candidates in response")
	}

	if len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no parts in response")
	}

	// Extract text from the first part
	if textPart, ok := result.Candidates[0].Content.Parts[0].(genai.Text); ok {
		text := string(textPart)
		if text == "" {
			return "", fmt.Errorf("empty text in response")
		}
		logMessage(fmt.Sprintf("Transcription completed in %.2fs", duration.Seconds()))
		return strings.TrimSpace(text), nil
	}

	return "", fmt.Errorf("unexpected response format")
}

// Helper functions
func (app *AppState) createWAVData(rawData []byte) []byte {
	// Create WAV header for 16kHz, 16-bit, mono PCM
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
	writeUint32LE(header[16:20], 16) // fmt chunk size
	writeUint16LE(header[20:22], 1)  // audio format (PCM)
	writeUint16LE(header[22:24], uint16(channels))
	writeUint32LE(header[24:28], uint32(sampleRate))
	writeUint32LE(header[28:32], uint32(sampleRate*channels*bitsPerSample/8)) // byte rate
	writeUint16LE(header[32:34], uint16(channels*bitsPerSample/8)) // block align
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

func (app *AppState) speedUpAudio(inputFile, outputFile string) bool {
	cmd := exec.Command("ffmpeg", "-i", inputFile, "-filter:a", 
		fmt.Sprintf("atempo=%.1f", app.config.SpeedMultiplier), "-y", outputFile)
	
	if err := cmd.Run(); err != nil {
		logMessage(fmt.Sprintf("Error speeding up audio: %v", err))
		return false
	}
	return true
}

func (app *AppState) splitAudioBySilence(inputFile string) ([]string, error) {
	tempDir := filepath.Dir(inputFile) + "/segments"
	os.MkdirAll(tempDir, 0755)
	
	outputPattern := filepath.Join(tempDir, "segment_%03d.wav")
	
	cmd := exec.Command("sox", inputFile, outputPattern,
		"silence", "1", "0.1", app.config.SilenceThreshold,
		"1", fmt.Sprintf("%.1f", app.config.MinSilenceDuration), app.config.SilenceThreshold,
		":", "newfile", ":", "restart")
	
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sox failed: %v", err)
	}
	
	// Find created segments
	pattern := filepath.Join(tempDir, "segment_*.wav")
	segments, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	
	return segments, nil
}

func (app *AppState) copyToClipboard(text string) bool {
	var cmd *exec.Cmd
	
	// Detect session type
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
	}
	
	logMessage("✅ Copied to clipboard!")
	return true
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

func (app *AppState) checkDependencies() bool {
	required := []string{"arecord", "sox", "ffmpeg"}
	missing := []string{}
	
	for _, cmd := range required {
		if _, err := exec.LookPath(cmd); err != nil {
			missing = append(missing, cmd)
		}
	}
	
	// Check clipboard tools
	hasClipboard := false
	for _, cmd := range []string{"wl-copy", "xclip"} {
		if _, err := exec.LookPath(cmd); err == nil {
			hasClipboard = true
			break
		}
	}
	
	if !hasClipboard {
		missing = append(missing, "wl-copy or xclip")
	}
	
	if len(missing) > 0 {
		logMessage(fmt.Sprintf("Missing dependencies: %s", strings.Join(missing, ", ")))
		logMessage("Install with: sudo apt install alsa-utils sox ffmpeg wl-clipboard xclip")
		return false
	}
	
	return true
}

func (app *AppState) writePIDFile() error {
	pid := fmt.Sprintf("%d", os.Getpid())
	return os.WriteFile(app.config.PIDFile, []byte(pid), 0644)
}

func (app *AppState) cleanup() {
	app.mu.Lock()
	defer app.mu.Unlock()
	
	if app.arecordCmd != nil && app.arecordCmd.Process != nil {
		app.arecordCmd.Process.Kill()
	}
	
	os.Remove(app.config.PIDFile)
	os.Remove(app.config.AudioTempFile)
}

func (app *AppState) mainLoop() {
	// Keep the main goroutine alive
	select {}
}

// Utility functions
func logMessage(message string) {
	fmt.Printf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), message)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

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