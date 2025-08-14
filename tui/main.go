package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const historyDirName = ".voiceai_history"

var homeDir, _ = os.UserHomeDir()
var historyDir = filepath.Join(homeDir, historyDirName)
var audioDir = filepath.Join(historyDir, "audio")
var textDir = filepath.Join(historyDir, "text")

type recording struct {
	ID        string
	Timestamp string
	Preview   string
	Text      string
}

func (r recording) Title() string       { return r.ID }
func (r recording) Description() string { return r.Preview }
func (r recording) FilterValue() string { return r.ID }

type model struct {
	recentList   list.Model
	allList      list.Model
	recordings   []recording
	quitting     bool
	activeView   view
	textData     textViewData
	message      string
	messageTimer int
	audioCmd     *exec.Cmd
	isPlaying    bool
	activeList   listType
}

type view int

const (
	listView view = iota
	textView
)

type listType int

const (
	recentListType listType = iota
	allListType
)

type textViewData struct {
	text  string
	title string
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle message timeout
	if m.messageTimer > 0 {
		m.messageTimer--
		if m.messageTimer == 0 {
			m.message = ""
		}
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		// Handle mouse clicks to switch between sections
		if msg.Type == tea.MouseLeft {
			// Simple logic: if click is in upper half, switch to recent, else switch to all
			if msg.Y < 15 {
				if m.activeList != recentListType {
					m.activeList = recentListType
					return m, nil
				}
			} else {
				if m.activeList != allListType {
					m.activeList = allListType
					return m, nil
				}
			}
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.activeView == textView {
				m.activeView = listView
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		case "tab":
			// Switch between recent and all lists
			if m.activeList == recentListType {
				m.activeList = allListType
			} else {
				m.activeList = recentListType
			}
			return m, nil
		case "enter":
			var selected recording
			if m.activeList == recentListType && len(m.recordings) > 0 && m.recordings[0].ID != "no-recordings" {
				selected = m.recentList.SelectedItem().(recording)
			} else if m.activeList == allListType && len(m.recordings) > 0 && m.recordings[0].ID != "no-recordings" {
				selected = m.allList.SelectedItem().(recording)
			} else {
				return m, nil
			}

			m.textData = textViewData{
				text:  selected.Text,
				title: selected.ID,
			}
			m.activeView = textView
			return m, nil
		case "esc":
			if m.activeView == textView {
				m.activeView = listView
				return m, nil
			}
		case "p":
			var selected recording
			if m.activeList == recentListType && len(m.recordings) > 0 && m.recordings[0].ID != "no-recordings" {
				selected = m.recentList.SelectedItem().(recording)
			} else if m.activeList == allListType && len(m.recordings) > 0 && m.recordings[0].ID != "no-recordings" {
				selected = m.allList.SelectedItem().(recording)
			} else {
				return m, nil
			}
			return m.playAudio(selected)
		case "s":
			if m.isPlaying {
				return m.stopAudio()
			}
		case "c":
			var selected recording
			if m.activeList == recentListType && len(m.recordings) > 0 && m.recordings[0].ID != "no-recordings" {
				selected = m.recentList.SelectedItem().(recording)
			} else if m.activeList == allListType && len(m.recordings) > 0 && m.recordings[0].ID != "no-recordings" {
				selected = m.allList.SelectedItem().(recording)
			} else {
				return m, nil
			}
			return m.copyToClipboard(selected)
		case "r":
			var selected recording
			if m.activeList == recentListType && len(m.recordings) > 0 && m.recordings[0].ID != "no-recordings" {
				selected = m.recentList.SelectedItem().(recording)
			} else if m.activeList == allListType && len(m.recordings) > 0 && m.recordings[0].ID != "no-recordings" {
				selected = m.allList.SelectedItem().(recording)
			} else {
				return m, nil
			}
			return m.retryTranscription(selected)
		case "R":
			recordings, err := loadRecordings()
			if err != nil {
				m.message = fmt.Sprintf("Error loading recordings: %v", err)
				m.messageTimer = 60
			} else {
				m.recordings = recordings
				if len(recordings) == 0 {
					recordings = append(recordings, recording{
						ID:        "no-recordings",
						Timestamp: "N/A",
						Preview:   "No transcriptions available",
						Text:      "No transcriptions available",
					})
				}

				// Update recent list (max 3 items)
				var recentItems []list.Item
				if len(recordings) > 0 && recordings[0].ID != "no-recordings" {
					limit := 3
					if len(recordings) < 3 {
						limit = len(recordings)
					}
					for i := 0; i < limit; i++ {
						recentItems = append(recentItems, recordings[i])
					}
				} else {
					recentItems = append(recentItems, recordings[0])
				}
				m.recentList.SetItems(recentItems)

				// Update all list
				allItems := make([]list.Item, len(recordings))
				for i, r := range recordings {
					allItems[i] = r
				}
				m.allList.SetItems(allItems)

				m.message = "Refreshed recordings list"
				m.messageTimer = 30
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		if m.activeView == listView {
			// Split window between recent and all lists
			height := msg.Height - v - 12 // Leave space for headers and messages
			recentHeight := height / 3
			allHeight := height - recentHeight - 2

			m.recentList.SetSize(msg.Width-h, recentHeight)
			m.allList.SetSize(msg.Width-h, allHeight)
		}
	}

	var cmd tea.Cmd
	if m.activeView == listView {
		if m.activeList == recentListType {
			m.recentList, cmd = m.recentList.Update(msg)
		} else {
			m.allList, cmd = m.allList.Update(msg)
		}
	}
	return m, cmd
}

// Styling inspired by Charm Crush
var (
	// Color palette
	indigo       = lipgloss.AdaptiveColor{Light: "#5A56E0", Dark: "#7571F9"}
	cream        = lipgloss.AdaptiveColor{Light: "#FFFDF5", Dark: "#FFFDF5"}
	fuschia      = lipgloss.AdaptiveColor{Light: "#EE6FF8", Dark: "#EE6FF8"}
	green        = lipgloss.AdaptiveColor{Light: "#04B575", Dark: "#04B575"}
	red          = lipgloss.AdaptiveColor{Light: "#FF4672", Dark: "#ED567A"}
	subtleIndigo = lipgloss.AdaptiveColor{Light: "#7D79F6", Dark: "#514DC1"}

	// Base styles
	docStyle = lipgloss.NewStyle().Padding(1, 2)

	// Section styles
	sectionStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(indigo).
			Padding(1, 2)

	titleStyle = lipgloss.NewStyle().
			Foreground(indigo).
			Bold(true).
			MarginBottom(1)

	activeTitleStyle = lipgloss.NewStyle().
				Foreground(fuschia).
				Bold(true).
				MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			MarginTop(1)

	messageStyle = lipgloss.NewStyle().
			Foreground(red).
			Bold(true)

	// Text view styles
	textViewStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(indigo).
			Padding(1, 2)

	textTitleStyle = lipgloss.NewStyle().
			Foreground(fuschia).
			Bold(true).
			MarginBottom(1)

	textContentStyle = lipgloss.NewStyle().
				Foreground(cream).
				Width(80)
)

func (m model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	if m.activeView == textView {
		return m.renderTextView()
	}

	return m.renderListView()
}

func (m model) renderListView() string {
	// Update list titles based on active status
	if m.activeList == recentListType {
		m.recentList.Title = "▶ Recent Recordings (Last 3)"
		m.recentList.Styles.Title = activeTitleStyle
	} else {
		m.recentList.Title = "Recent Recordings (Last 3)"
		m.recentList.Styles.Title = titleStyle
	}

	if m.activeList == allListType {
		m.allList.Title = "▶ All Recordings"
		m.allList.Styles.Title = activeTitleStyle
	} else {
		m.allList.Title = "All Recordings"
		m.allList.Styles.Title = titleStyle
	}

	// Render recent recordings section
	recentView := sectionStyle.Render(m.recentList.View())

	// Render all recordings section
	allView := sectionStyle.Render(m.allList.View())

	// Combine sections
	sections := lipgloss.JoinVertical(lipgloss.Center,
		recentView,
		allView,
	)

	// Add help text
	helpText := "↑/↓: Navigate • Tab: Switch sections • Enter: View Text • p: Play Audio"
	if m.isPlaying {
		helpText += " • s: Stop Audio"
	}
	helpText += " • c: Copy Text • r: Retry Transcription • R: Refresh • q: Quit"

	help := helpStyle.Render(helpText)

	// Add message if exists
	var message string
	if m.message != "" {
		message = messageStyle.Render("\n" + m.message)
	}

	return docStyle.Render(
		lipgloss.JoinVertical(lipgloss.Center,
			sections,
			message,
			help,
		),
	)
}

func (m model) renderTextView() string {
	header := textTitleStyle.Render(m.textData.title)

	content := textContentStyle.Render(m.textData.text)

	footer := helpStyle.Render("Press 'q' or 'esc' to return to list")

	textView := textViewStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			header,
			content,
		),
	)

	return docStyle.Render(
		lipgloss.JoinVertical(lipgloss.Center,
			textView,
			footer,
		),
	)
}

func (m model) playAudio(selected recording) (tea.Model, tea.Cmd) {
	// Check if already playing
	if m.isPlaying {
		m.message = "Audio is already playing. Press 's' to stop first."
		m.messageTimer = 60
		return m, nil
	}

	audioFile := filepath.Join(audioDir, selected.ID+".wav")

	// Check if aplay is available
	if _, err := exec.LookPath("aplay"); err != nil {
		m.message = "Error: aplay not found. Please install alsa-utils"
		m.messageTimer = 60
		return m, nil
	}

	// Check if file exists
	if _, err := os.Stat(audioFile); os.IsNotExist(err) {
		m.message = fmt.Sprintf("Audio file not found: %s", audioFile)
		m.messageTimer = 60
		return m, nil
	}

	// Try to play audio
	m.audioCmd = exec.Command("aplay", audioFile)
	if err := m.audioCmd.Start(); err != nil {
		m.message = fmt.Sprintf("Error playing audio: %v", err)
		m.messageTimer = 60
		return m, nil
	}

	m.isPlaying = true
	m.message = "Playing audio... Press 's' to stop"
	m.messageTimer = 120
	return m, nil
}

func (m model) stopAudio() (tea.Model, tea.Cmd) {
	if m.isPlaying && m.audioCmd != nil {
		// Try to terminate the process gracefully first
		if err := m.audioCmd.Process.Signal(os.Interrupt); err != nil {
			// If that fails, kill it forcefully
			m.audioCmd.Process.Kill()
		}

		// Wait for the process to finish (with timeout)
		done := make(chan error, 1)
		go func() {
			done <- m.audioCmd.Wait()
		}()

		select {
		case <-done:
			// Process finished normally
		case <-time.After(2 * time.Second):
			// Timeout - kill it forcefully
			m.audioCmd.Process.Kill()
		}

		m.audioCmd = nil
		m.isPlaying = false
		m.message = "Audio playback stopped"
		m.messageTimer = 30
	}
	return m, nil
}

func (m model) copyToClipboard(selected recording) (tea.Model, tea.Cmd) {
	// Detect clipboard tool
	sessionType := os.Getenv("XDG_SESSION_TYPE")
	var cmd *exec.Cmd

	if strings.Contains(strings.ToLower(sessionType), "wayland") {
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			m.message = "wl-copy not found. Install wl-clipboard for Wayland"
			m.messageTimer = 60
			return m, nil
		}
	} else {
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else {
			m.message = "xclip not found. Install xclip for X11"
			m.messageTimer = 60
			return m, nil
		}
	}

	cmd.Stdin = strings.NewReader(selected.Text)

	if err := cmd.Run(); err != nil {
		m.message = fmt.Sprintf("Failed to copy to clipboard: %v", err)
		m.messageTimer = 60
		return m, nil
	}

	m.message = "Text copied to clipboard!"
	m.messageTimer = 30
	return m, nil
}

func (m model) retryTranscription(selected recording) (tea.Model, tea.Cmd) {
	audioFile := filepath.Join(audioDir, selected.ID+".wav")

	// Check if audio file exists
	if _, err := os.Stat(audioFile); os.IsNotExist(err) {
		m.message = fmt.Sprintf("Audio file not found: %s", audioFile)
		m.messageTimer = 60
		return m, nil
	}

	m.message = "Retrying transcription... This may take a moment"
	m.messageTimer = 120

	// Call the Python script to retry transcription
	// The Python script is in the parent directory
	pythonScript := "../voiceai.gemini.live.fast.py"

	// Check if Python script exists
	if _, err := os.Stat(pythonScript); os.IsNotExist(err) {
		// Try alternative path
		pythonScript = filepath.Join("..", "voiceai.gemini.live.fast.py")
		if _, err := os.Stat(pythonScript); os.IsNotExist(err) {
			m.message = "Python script not found for transcription"
			m.messageTimer = 60
			return m, nil
		}
	}

	// Execute the Python script with the audio file
	cmd := exec.Command("python3", pythonScript, "--retry", audioFile, selected.ID)

	// Run the command and wait for completion
	if err := cmd.Run(); err != nil {
		m.message = fmt.Sprintf("Transcription failed: %v", err)
		m.messageTimer = 60
		return m, nil
	}

	// Reload recordings to show updated transcription
	recordings, err := loadRecordings()
	if err != nil {
		m.message = "Transcription completed but failed to refresh list"
		m.messageTimer = 60
		return m, nil
	}

	m.recordings = recordings

	// Update recent list (max 3 items)
	var recentItems []list.Item
	if len(recordings) > 0 && recordings[0].ID != "no-recordings" {
		limit := 3
		if len(recordings) < 3 {
			limit = len(recordings)
		}
		for i := 0; i < limit; i++ {
			recentItems = append(recentItems, recordings[i])
		}
	} else {
		recentItems = append(recentItems, recordings[0])
	}
	m.recentList.SetItems(recentItems)

	// Update all list
	allItems := make([]list.Item, len(recordings))
	for i, r := range recordings {
		allItems[i] = r
	}
	m.allList.SetItems(allItems)

	m.message = "Transcription retry completed successfully!"
	m.messageTimer = 120

	return m, nil
}

func loadRecordings() ([]recording, error) {
	if _, err := os.Stat(textDir); os.IsNotExist(err) {
		return []recording{}, nil
	}

	files, err := ioutil.ReadDir(textDir)
	if err != nil {
		return nil, err
	}

	var recordings []recording
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".txt" {
			id := strings.TrimSuffix(file.Name(), ".txt")
			textPath := filepath.Join(textDir, file.Name())

			textBytes, err := ioutil.ReadFile(textPath)
			if err != nil {
				continue
			}

			text := string(textBytes)
			preview := text
			if len(preview) > 50 {
				preview = preview[:50] + "..."
			}

			// Try to parse timestamp from ID
			timestamp := id
			if t, err := time.Parse("2006-01-02_15-04-05", id); err == nil {
				timestamp = t.Format("2006-01-02 15:04:05")
			}

			recordings = append(recordings, recording{
				ID:        id,
				Timestamp: timestamp,
				Preview:   preview,
				Text:      text,
			})
		}
	}

	// Sort by timestamp (newest first)
	// Simple bubble sort for now
	for i := 0; i < len(recordings)-1; i++ {
		for j := 0; j < len(recordings)-i-1; j++ {
			if recordings[j].ID < recordings[j+1].ID {
				recordings[j], recordings[j+1] = recordings[j+1], recordings[j]
			}
		}
	}

	return recordings, nil
}

func main() {
	recordings, err := loadRecordings()
	if err != nil {
		fmt.Printf("Error loading recordings: %v\n", err)
		os.Exit(1)
	}

	if len(recordings) == 0 {
		recordings = append(recordings, recording{
			ID:        "no-recordings",
			Timestamp: "N/A",
			Preview:   "No transcriptions available",
			Text:      "No transcriptions available",
		})
	}

	// Create recent recordings list (max 3 items)
	var recentItems []list.Item
	if len(recordings) > 0 && recordings[0].ID != "no-recordings" {
		limit := 3
		if len(recordings) < 3 {
			limit = len(recordings)
		}
		for i := 0; i < limit; i++ {
			recentItems = append(recentItems, recordings[i])
		}
	} else {
		recentItems = append(recentItems, recordings[0])
	}

	// Create all recordings list
	allItems := make([]list.Item, len(recordings))
	for i, r := range recordings {
		allItems[i] = r
	}

	// Create custom delegate for better styling
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("117")).
		Bold(true)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	recentList := list.New(recentItems, delegate, 0, 0)
	allList := list.New(allItems, delegate, 0, 0)

	recentList.Title = "Recent Recordings (Last 3)"
	allList.Title = "All Recordings"

	recentList.Styles.Title = titleStyle
	allList.Styles.Title = titleStyle

	m := model{
		recentList: recentList,
		allList:    allList,
		recordings: recordings,
		activeView: listView,
		activeList: recentListType, // Start with recent list active
	}

	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
		os.Exit(1)
	}
}
