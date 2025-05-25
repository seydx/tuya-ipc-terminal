#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Function to print colored output
print_color() {
    printf "${1}${2}${NC}\n"
}

print_header() {
    echo ""
    print_color $CYAN "$(printf '=%.0s' {1..70})"
    print_color $CYAN "  $1"
    print_color $CYAN "$(printf '=%.0s' {1..70})"
    echo ""
}

print_section() {
    echo ""
    print_color $YELLOW "$1"
    print_color $YELLOW "$(printf -- '-%.0s' {1..${#1}})"
}

# Build information
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION=$(go version | awk '{print $3}')

print_header "TUYA IPC TERMINAL BUILD"

# Get dependencies
print_section "Getting dependencies..."
go mod tidy

# Verify all packages can be imported
print_section "Verifying packages..."
go list ./...

# Build for current platform
print_section "Building binary..."
LDFLAGS="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}"
go build -ldflags "${LDFLAGS}" -o tuya-ipc-terminal .

print_color $GREEN "✓ Build complete: ./tuya-ipc-terminal"

# Show build info
if command -v ls >/dev/null 2>&1; then
    SIZE=$(ls -lh tuya-ipc-terminal | awk '{print $5}')
    print_color $GREEN "✓ Binary size: ${SIZE}"
fi

print_color $GREEN "✓ Version: ${VERSION}"
print_color $GREEN "✓ Build time: ${BUILD_TIME}"
print_color $GREEN "✓ Go version: ${GO_VERSION}"

# Show available commands
print_header "COMMAND REFERENCE"

print_section "Authentication Commands"
echo "  ./tuya-ipc-terminal auth list                                   # List all authenticated users"
echo "  ./tuya-ipc-terminal auth add [region] [email]                   # Add new user (QR code)"
echo "  ./tuya-ipc-terminal auth add --password [region] [email]        # Add user (email/password)"
echo "  ./tuya-ipc-terminal auth remove [region] [email]                # Remove user authentication"
echo "  ./tuya-ipc-terminal auth refresh [region] [email]               # Refresh user session"
echo "  ./tuya-ipc-terminal auth test [region] [email]                  # Test session validity"
echo "  ./tuya-ipc-terminal auth show-regions                           # Show available regions"
echo "  ./tuya-ipc-terminal auth show-country-codes                     # Show country codes"
echo "  ./tuya-ipc-terminal auth show-country-codes --search germany    # Search countries"

print_section "Camera Management Commands"
echo "  ./tuya-ipc-terminal cameras list                       # List all cameras"
echo "  ./tuya-ipc-terminal cameras refresh                    # Refresh camera list"
echo "  ./tuya-ipc-terminal cameras info [camera-id]           # Show camera details"
echo "  ./tuya-ipc-terminal cameras shared                     # List shared cameras"

print_section "RTSP Server Commands"
echo "  ./tuya-ipc-terminal rtsp start --port 8554             # Start RTSP server"
echo "  ./tuya-ipc-terminal rtsp stop                          # Stop RTSP server"
echo "  ./tuya-ipc-terminal rtsp status                        # Show server status"
echo "  ./tuya-ipc-terminal rtsp list-endpoints                # List available streams"
echo "  ./tuya-ipc-terminal rtsp restart                       # Restart RTSP server"

print_section "Available Regions"
echo "  eu-central    Central Europe     (protect-eu.ismartlife.me)"
echo "  eu-east       East Europe        (protect-we.ismartlife.me)"
echo "  us-west       West America       (protect-us.ismartlife.me)"
echo "  us-east       East America       (protect-ue.ismartlife.me)"
echo "  china         China              (protect.ismartlife.me)"
echo "  india         India              (protect-in.ismartlife.me)"

print_section "Authentication Methods"
echo "  QR Code       Scan QR code with Tuya Smart/Smart Life app (default)"
echo "  Password      Use email and password with country code selection"

print_header "QUICK START GUIDE"

print_color $CYAN "Step 1: Authenticate"
echo "  ./tuya-ipc-terminal auth add eu-central user@example.com"
echo "  # Choose authentication method when prompted"
echo ""

print_color $CYAN "Step 2: List Available Regions & Country Codes (if needed)"
echo "  ./tuya-ipc-terminal auth show-regions"
echo "  ./tuya-ipc-terminal auth show-country-codes --search germany"
echo ""

print_color $CYAN "Step 3: Refresh Camera List"
echo "  ./tuya-ipc-terminal cameras refresh"
echo ""

print_color $CYAN "Step 4: Start RTSP Server"
echo "  ./tuya-ipc-terminal rtsp start --port 8554"
echo ""

print_color $CYAN "Step 5: Access Camera Streams"
echo "  # High Definition Stream:"
echo "  ffplay rtsp://localhost:8554/CameraName/hd"
echo ""
echo "  # Standard Definition Stream:"
echo "  ffplay rtsp://localhost:8554/CameraName/sd"
echo ""
echo "  # With VLC:"
echo "  vlc rtsp://localhost:8554/CameraName/hd"

print_header "USEFUL EXAMPLES"

print_color $PURPLE "Authentication Examples:"
echo "  # QR Code authentication (default)"
echo "  ./tuya-ipc-terminal auth add eu-central john@example.com"
echo ""
echo "  # Password authentication"
echo "  ./tuya-ipc-terminal auth add --password us-west sarah@example.com"
echo ""
echo "  # Test if session is still valid"
echo "  ./tuya-ipc-terminal auth test eu-central john@example.com"
echo ""

print_color $PURPLE "Camera Management Examples:"
echo "  # List all cameras with details"
echo "  ./tuya-ipc-terminal cameras list"
echo ""
echo "  # Get specific camera information"
echo "  ./tuya-ipc-terminal cameras info bf123456789abcdef"
echo ""
echo "  # Refresh camera list after adding new cameras"
echo "  ./tuya-ipc-terminal cameras refresh"
echo ""

print_color $PURPLE "RTSP Server Examples:"
echo "  # Start server on custom port"
echo "  ./tuya-ipc-terminal rtsp start --port 9554"
echo ""
echo "  # Check what streams are available"
echo "  ./tuya-ipc-terminal rtsp list-endpoints"
echo ""
echo "  # Monitor server status"
echo "  ./tuya-ipc-terminal rtsp status"

print_header "TROUBLESHOOTING"

print_color $RED "Common Issues & Solutions:"
echo ""
echo "❌ Authentication fails:"
echo "   → Check region is correct for your account"
echo "   → Verify email address is correct"
echo "   → Try refreshing: ./tuya-ipc-terminal auth refresh [region] [email]"
echo "   → Only Tuya accounts work, not Smart Life accounts"
echo ""

echo "❌ No cameras found:"
echo "   → Run: ./tuya-ipc-terminal cameras refresh"
echo "   → Check if cameras are online in Tuya Smart app"
echo "   → Verify authentication: ./tuya-ipc-terminal auth test [region] [email]"
echo ""

echo "❌ RTSP stream not working:"
echo "   → Check server status: ./tuya-ipc-terminal rtsp status"
echo "   → Verify endpoints: ./tuya-ipc-terminal rtsp list-endpoints"
echo "   → Try restarting: ./tuya-ipc-terminal rtsp restart"
echo ""

echo "❌ Country code not found during password login:"
echo "   → Search by name: ./tuya-ipc-terminal auth show-country-codes --search [country]"
echo "   → View all codes: ./tuya-ipc-terminal auth show-country-codes"
echo ""

print_color $GREEN "✅ For more help, run any command with --help"
print_color $GREEN "✅ Example: ./tuya-ipc-terminal auth --help"

print_header "BUILD COMPLETE"
print_color $GREEN "🎉 tuya-ipc-terminal is ready to use!"
print_color $CYAN "Start with: ./tuya-ipc-terminal auth add [region] [email]"
echo ""