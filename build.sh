#!/bin/bash

# Build script for tuya-ipc-terminal

set -e

echo "Building tuya-ipc-terminal..."

# Check Go version
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
REQUIRED_VERSION="1.21"

if ! printf '%s\n%s\n' "$REQUIRED_VERSION" "$GO_VERSION" | sort -V -C; then
    echo "Error: Go $REQUIRED_VERSION or higher is required (found: $GO_VERSION)"
    exit 1
fi

# Get dependencies
echo "Getting dependencies..."
go mod tidy

# Verify all packages can be imported
echo "Verifying packages..."
go list ./...

# Run tests if any exist
if [ -f "*_test.go" ]; then
    echo "Running tests..."
    go test ./...
fi

# Build for current platform
echo "Building binary..."
go build -ldflags "-s -w" -o tuya-ipc-terminal .

echo "Build complete: ./tuya-ipc-terminal"

# Show file size
if command -v ls >/dev/null 2>&1; then
    echo "Binary size: $(ls -lh tuya-ipc-terminal | awk '{print $5}')"
fi

# Show available commands
echo ""
echo "Available commands:"
echo "=================="
echo ""
echo "Authentication:"
echo "  ./tuya-ipc-terminal auth list"
echo "  ./tuya-ipc-terminal auth add [region] [email]"
echo "  ./tuya-ipc-terminal auth remove [region] [email]"
echo "  ./tuya-ipc-terminal auth refresh [region] [email]"
echo "  ./tuya-ipc-terminal auth test [region] [email]"
echo ""
echo "Camera Management:"
echo "  ./tuya-ipc-terminal cameras list"
echo "  ./tuya-ipc-terminal cameras refresh"
echo "  ./tuya-ipc-terminal cameras info [camera-id]"
echo ""
echo "RTSP Server:"
echo "  ./tuya-ipc-terminal rtsp start --port 8554"
echo "  ./tuya-ipc-terminal rtsp stop"
echo "  ./tuya-ipc-terminal rtsp status"
echo "  ./tuya-ipc-terminal rtsp list-endpoints"
echo ""
echo "Quick Start:"
echo "============"
echo "1. ./tuya-ipc-terminal auth add eu-central user@example.com"
echo "2. ./tuya-ipc-terminal cameras refresh"
echo "3. ./tuya-ipc-terminal rtsp start --port 8554"
echo "4. ffplay rtsp://localhost:8554/CameraName/hd or ffplay rtsp://localhost:8554/CameraName/sd"
echo ""
echo "Available regions: eu-central, eu-east, us-west, us-east, china, india"