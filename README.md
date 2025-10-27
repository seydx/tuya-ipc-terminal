# Tuya IPC Terminal

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/)
[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)]()

A powerful CLI tool to connect Tuya Smart Cameras to RTSP clients through reverse-engineered Tuya browser client APIs. Stream your Tuya cameras directly to any RTSP-compatible media player or home automation system.

## ✨ Features

- 🔐 **Dual Authentication**: QR code scanning OR email/password login
- 🌍 **Multi-Region Support**: Support for all Tuya regions (EU, US, China, India)
- 👥 **Multi-User Management**: Handle multiple Tuya Smart accounts simultaneously
- 📡 **Session Persistence**: Automatic session management with validation
- 🎥 **Camera Discovery**: Automatic detection of all cameras from authenticated accounts
- 🎬 **RTSP Server**: WebRTC-to-RTSP bridge for universal compatibility
- 🚀 **Real-time Streaming**: Direct camera access with minimal latency
- 🔄 **Multi-Client Support**: Multiple RTSP clients per camera stream
- 🎯 **H265/HEVC Ready**: Advanced codec support for optimal performance
- 🎤 **Two-Way Audio**: Bidirectional audio for compatible cameras

## 🚀 Quick Start

### Installation

```bash
git clone <repository-url>
cd tuya-ipc-terminal
chmod +x build.sh
./build.sh
```

### 1-Minute Setup

```bash
# 1. Add your Tuya account (interactive)
./tuya-ipc-terminal auth add eu-central user@example.com

# 2. Discover your cameras
./tuya-ipc-terminal cameras refresh

# 3. Start streaming
./tuya-ipc-terminal rtsp start --port 8554

# 4. Watch your cameras
ffplay rtsp://localhost:8554/MyCamera
```

## 📖 Complete Documentation

### 🔐 Authentication Management

#### Two Authentication Methods

**🎯 QR Code**
```bash
./tuya-ipc-terminal auth add eu-central user@example.com
# Scan QR code with Tuya Smart/Smart Life app
```

**🔑 Email/Password**
```bash
./tuya-ipc-terminal auth add eu-central user@example.com --password
# Enter password and select country code interactively
```

#### User Management Commands

```bash
# List all authenticated users
./tuya-ipc-terminal auth list

# Remove user authentication
./tuya-ipc-terminal auth remove eu-central user@example.com

# Refresh expired session
./tuya-ipc-terminal auth refresh eu-central user@example.com

# Test session validity
./tuya-ipc-terminal auth test eu-central user@example.com
```

#### 🌍 Regional Information

```bash
# Show all available regions
./tuya-ipc-terminal auth show-regions

# Show country codes for password authentication
./tuya-ipc-terminal auth show-country-codes
./tuya-ipc-terminal auth show-country-codes --search germany
```

**Available Regions:**
| Region | Endpoint | Description |
|--------|----------|-------------|
| `eu-central` | protect-eu.ismartlife.me | Central Europe |
| `eu-east` | protect-we.ismartlife.me | East Europe |
| `us-west` | protect-us.ismartlife.me | West America |
| `us-east` | protect-ue.ismartlife.me | East America |
| `china` | protect.ismartlife.me | China |
| `india` | protect-in.ismartlife.me | India |

### 📹 Camera Management

```bash
# List all discovered cameras
./tuya-ipc-terminal cameras list

# List cameras for specific user
./tuya-ipc-terminal cameras list --user eu-central_user_at_example_com

# Refresh camera discovery
./tuya-ipc-terminal cameras refresh

# Get detailed camera information
./tuya-ipc-terminal cameras info [camera-id-or-name]
```

### 📡 RTSP Server Management

```bash
# Start RTSP server
./tuya-ipc-terminal rtsp start --port 8554

# Start as background daemon
./tuya-ipc-terminal rtsp start --port 8554 --daemon

# Stop RTSP server
./tuya-ipc-terminal rtsp stop

# Check server status
./tuya-ipc-terminal rtsp status

# List all available camera endpoints
./tuya-ipc-terminal rtsp list-endpoints
```

## 🎬 RTSP Streaming Guide

### 📺 Connecting to Camera Streams

Camera streams are available at:
```
rtsp://localhost:[port]/[camera-name]
rtsp://localhost:[port]/[camera-name]/sd  # Sub-stream (lower quality)
```


### 🏠 Home Automation Integration

**Home Assistant**
```yaml
camera:
  - platform: generic
    stream_source: rtsp://localhost:8554/LivingRoomCamera
    name: "Living Room Camera"
    
  - platform: generic
    stream_source: rtsp://localhost:8554/FrontDoor
    name: "Front Door Camera"
```

**Go2RTC Integration**
```yaml
streams:
  living_room:
    - rtsp://localhost:8554/LivingRoomCamera
  front_door:
    - rtsp://localhost:8554/FrontDoor
```


## 🏗️ Advanced Usage

### 🔧 Running as System Service

**Create systemd service:**
```bash
sudo tee /etc/systemd/system/tuya-rtsp.service > /dev/null <<EOF
[Unit]
Description=Tuya IPC Terminal RTSP Server
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=$PWD
ExecStart=$PWD/tuya-ipc-terminal rtsp start --port 8554
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable tuya-rtsp
sudo systemctl start tuya-rtsp
```

### 🔀 Multiple Server Instances

```bash
# Different ports for different purposes
./tuya-ipc-terminal rtsp start --port 8554 --daemon  # Main cameras
./tuya-ipc-terminal rtsp start --port 8555 --daemon  # Backup/secondary
```

### 👥 Multi-User Setup Example

```bash
# Add accounts from different regions
./tuya-ipc-terminal auth add eu-central home@example.com      # European account
./tuya-ipc-terminal auth add --password us-west work@company.com  # US business account
./tuya-ipc-terminal auth add china personal@example.cn        # Chinese account

# Discover all cameras
./tuya-ipc-terminal cameras refresh

# List everything
./tuya-ipc-terminal auth list
./tuya-ipc-terminal cameras list
```



### 🐋 Running as a Docker container

First, lets build the image:
```
docker build -t tuya_ipc:latest
```
For the initial setup, run an interactive setup on a temporary container for creating the authentication files (replace with your region/email):

```
docker run --rm -it -v ./tuya_data:/app/.tuya_data tuya_ipc:latest ./tuya-ipc-terminal auth add eu-central user@example.com
```

Then execute the following command to have a lasting container:

```
docker run --name tuya_ipc --restart unless-stopped -p 8554:8554 -u 1000:1000 -v /etc/localtime:/etc/localtime:ro -v ./tuya_data:/app/.tuya-data:rw tuya_ipc:latest
```

Done. You can access the streams at:

```
rtsp://localhost:8554/[camera-name]
rtsp://localhost:8554/[camera-name]/sd  # Sub-stream (lower quality)
```

### 📦 Running with Docker Compose

For the initial setup, run an interactive setup on a temporary container for creating the authentication files (replace with your region/email) same as the setup for normal docker:
```
docker build -t tuya_ipc:latest && docker run --rm -it -v ./tuya_data:/app/.tuya_data tuya_ipc:latest ./tuya-ipc-terminal auth add eu-central user@example.com
```
Then add the compose.yml file:
```
services:
  tuya_ipc:
    container_name: tuya_ipc
    build: .
    image: tuya_ipc:latest
    restart: unless-stopped
    user: "1000:1000"
    volumes:
      - /etc/localtime:/etc/localtime:ro
      - ./tuya_data:/app/.tuya-data:rw
```

Now just issue

```
docker compose up -d
```
And done. You can access the streams at:

```
rtsp://localhost:8554/[camera-name]
rtsp://localhost:8554/[camera-name]/sd  # Sub-stream (lower quality)
```

#### 🕐 Auto refreshing password auth with 🐋 Docker

The script located at [refresh_auth_token.sh](./scripts/refresh_auth_token.sh) can be used with a cron job to refresh your auth session automatically if you use the password login method.

Make sure to edit the [.env](.scripts/.env) file to use your credentials so it refreshes the correct account.

## 🔄 Data Flow

1. **Authentication** → Tuya Cloud API
2. **Discovery** → MQTT + Tuya API
3. **WebRTC Connection** → Direct camera stream
4. **RTP Processing** → Video/audio packet handling
5. **RTSP Server** → Standard protocol output

## 💾 Data Storage

```
.tuya-data/
├── user_eu-central_user_at_example_com.json    # User sessions
├── user_us-west_business_at_company_com.json   # Multiple accounts
└── cameras.json                                # Camera registry
```

## 🛠️ Technical Details

### 🎥 Supported Features

| Feature | Status | Notes |
|---------|--------|-------|
| H.264 Video | ✅ | Universal support |
| H.265/HEVC | ✅ | Better compression |
| PCMU Audio | ✅ | Standard quality |
| PCMA Audio | ✅ | Alternative codec |
| HD Streams | ✅ | Main camera stream |
| SD Streams | ✅ | Sub-stream, lower bandwidth |
| Multi-client | ✅ | Multiple viewers per camera |
| Two-way Audio | ✅ | Camera dependent |

### 🎯 Supported Camera Types

| Category | Description | Compatibility |
|----------|-------------|---------------|
| `sp` | Smart cameras | ✅ Full support |
| `dghsxj` | Additional camera type | ✅ Full support |
| Others | Generic Tuya cameras | ⚠️ May work |

## 🐛 Troubleshooting

### 🔐 Authentication Issues

**Problem**: Login fails
```bash
# 1. Check session status
./tuya-ipc-terminal auth test eu-central user@example.com

# 2. Try refreshing
./tuya-ipc-terminal auth refresh eu-central user@example.com

# 3. Re-authenticate if needed
./tuya-ipc-terminal auth remove eu-central user@example.com
./tuya-ipc-terminal auth add eu-central user@example.com
```

**Problem**: Country code not found (password login)
```bash
# Search for your country
./tuya-ipc-terminal auth show-country-codes --search germany
./tuya-ipc-terminal auth show-country-codes --search "united states"
```

### 📹 Camera Discovery Issues

**Problem**: No cameras found
```bash
# 1. Verify authentication
./tuya-ipc-terminal auth list

# 2. Force refresh
./tuya-ipc-terminal cameras refresh

# 3. Check if cameras are online in Tuya Smart app
./tuya-ipc-terminal cameras list --verbose
```

### 📡 RTSP Connection Issues

**Problem**: Can't connect to stream
```bash
# 1. Check server status
./tuya-ipc-terminal rtsp status

# 2. List available streams
./tuya-ipc-terminal rtsp list-endpoints

# 3. Test with simple player
ffplay rtsp://localhost:8554/CameraName
```

**Problem**: Poor stream quality
```bash
# Try sub-stream for lower bandwidth
ffplay rtsp://localhost:8554/CameraName/sd

# Check camera capabilities
./tuya-ipc-terminal cameras info "Camera Name"
```

### Development Setup

```bash
git clone <repository-url>
cd tuya-ipc-terminal
go mod download
go run main.go --help
```

## ⚠️ Limitations & Considerations

### Technical Limitations
- 🌐 Requires active internet connection to Tuya servers
- ☁️ Dependent on Tuya Cloud availability
- 📱 QR authentication requires mobile app
- 🔧 Some advanced camera features may not be supported

### Security Considerations
- 🔒 Sessions stored locally in JSON files
- 🛡️ No additional encryption beyond Tuya's implementation
- 🔥 Firewall configuration recommended for external access
- 🔐 Consider network security for RTSP streams

## 📄 License

MIT License - See [LICENSE](LICENSE) for details.

## ⚠️ Disclaimer

This tool is created through reverse engineering of Tuya's web client APIs. Use responsibly and ensure compliance with:

- Tuya's Terms of Service
- Local privacy laws and regulations
- Camera usage regulations in your jurisdiction

The authors are not responsible for any issues arising from the use of this software.

## 🆘 Support

Having issues? Here's how to get help:

1. 📖 Check this README for common solutions
2. 🐛 Search existing [GitHub Issues](link-to-issues)
3. 💬 Create a new issue with:
   - Your operating system
   - Go version (`go version`)
   - Error messages
   - Steps to reproduce

---

**⭐ If this project helps you, please consider giving it a star on GitHub!**
