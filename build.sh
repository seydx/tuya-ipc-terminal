#!/bin/bash

set -e

echo "Building tuya-ipc-terminal..."

# Get dependencies
echo "Getting dependencies..."
go mod tidy

# Verify all packages can be imported
echo "Verifying packages..."
go list ./...

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