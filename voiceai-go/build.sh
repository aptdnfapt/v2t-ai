#!/bin/bash

echo "🚀 Building VoiceAI Go..."

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.21 or later."
    echo "   Download from: https://golang.org/dl/"
    exit 1
fi

# Check Go version
GO_VERSION=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')
if [[ $(echo "$GO_VERSION < 1.21" | bc -l) -eq 1 ]]; then
    echo "❌ Go version $GO_VERSION is too old. Please install Go 1.21 or later."
    exit 1
fi

echo "✅ Go version: $(go version)"

# Initialize module if needed
if [ ! -f "go.mod" ]; then
    echo "📦 Initializing Go module..."
    go mod init voiceai-go
fi

# Download dependencies
echo "📥 Downloading dependencies..."
go mod tidy

# Build the binary
echo "🔨 Building binary..."
go build -ldflags="-s -w" -o voiceai-go main.go

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    echo "📁 Binary created: ./voiceai-go"
    echo "📏 Binary size: $(du -h voiceai-go | cut -f1)"
    echo ""
    echo "🎯 Usage:"
    echo "   ./voiceai-go"
    echo ""
    echo "🔧 Don't forget to set your API key:"
    echo "   export GEMINI_API_KEY=\"your_api_key_here\""
    echo ""
    echo "📋 Toggle recording:"
    echo "   kill -USR1 \$(cat /tmp/voice_input_gemini.pid)"
else
    echo "❌ Build failed!"
    exit 1
fi