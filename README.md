# Tuya IPC Terminal

A CLI tool to connect Tuya Smart Cameras to RTSP clients through reverse-engineered Tuya browser client APIs.

## Features

- **Multi-user Authentication**: Support for multiple Tuya Smart accounts across different regions
- **Session Management**: Persistent authentication with automatic session validation
- **Camera Discovery**: Automatic discovery of all cameras from authenticated accounts
- **RTSP Server**: Provides RTSP endpoints for Tuya cameras with WebRTC-to-RTP bridge
- **Real-time Streaming**: Direct camera access via WebRTC with RTSP output
- **Multi-client Support**: Multiple RTSP clients can connect to the same camera stream
- **H265/HEVC Support**: Stream cameras using H265 codec for better performance
- **Two-way Audio**: With two-way audio support for compatible cameras

## Installation

### Prerequisites

- Go 1.21 or later
- Tuya Smart or Smart Life mobile app for QR code authentication

### Build from Source

```bash
git clone <repository-url>
cd tuya-ipc-terminal
chmod +x build.sh
./build.sh
```

## Quick Start

1. **Add a user account**:
   ```bash
   ./tuya-ipc-terminal auth add eu-central user@example.com
   ```

2. **Discover cameras**:
   ```bash
   ./tuya-ipc-terminal cameras refresh
   ```

3. **Start RTSP server**:
   ```bash
   ./tuya-ipc-terminal rtsp start --port 8554
   ```

4. **Connect with media player**:
   ```bash
   ffplay rtsp://localhost:8554/MyCamera
   # or
   vlc rtsp://localhost:8554/MyCamera
   ```

## Usage

### Authentication Management

#### List authenticated users
```bash
./tuya-ipc-terminal auth list
```

#### Add new user
```bash
./tuya-ipc-terminal auth add [region] [email]
```

Available regions:
- `eu-central` - Central Europe
- `eu-east` - East Europe  
- `us-west` - West America
- `us-east` - East America
- `china` - China
- `india` - India

Example:
```bash
./tuya-ipc-terminal auth add eu-central user@example.com
```

#### Remove user
```bash
./tuya-ipc-terminal auth remove eu-central user@example.com
```

#### Refresh user session
```bash
./tuya-ipc-terminal auth refresh eu-central user@example.com
```

#### Test session validity
```bash
./tuya-ipc-terminal auth test eu-central user@example.com
```

### Camera Management

#### List all cameras
```bash
./tuya-ipc-terminal cameras list
```

#### List cameras for specific user
```bash
./tuya-ipc-terminal cameras list --user eu-central_user_at_example_com
```

#### Refresh camera discovery
```bash
./tuya-ipc-terminal cameras refresh
```

#### Get detailed camera information
```bash
./tuya-ipc-terminal cameras info [camera-id-or-name]
```

### RTSP Server Management

#### Start RTSP server
```bash
./tuya-ipc-terminal rtsp start --port 8554
```

#### Start as daemon (background)
```bash
./tuya-ipc-terminal rtsp start --port 8554 --daemon
```

#### Stop RTSP server
```bash
./tuya-ipc-terminal rtsp stop
```

#### Show server status
```bash
./tuya-ipc-terminal rtsp status
```

#### List available endpoints
```bash
./tuya-ipc-terminal rtsp list-endpoints
```

## RTSP Streaming

### Connecting to Camera Streams

Once the RTSP server is running, you can connect to camera streams using any RTSP-compatible media player:

#### Using FFplay
```bash
ffplay rtsp://localhost:8554/MyCamera
```

#### Using VLC Media Player
```bash
vlc rtsp://localhost:8554/MyCamera
```

#### Using FFmpeg (recording)
```bash
ffmpeg -i rtsp://localhost:8554/MyCamera -c copy output.mp4
```

### Stream URLs

Camera streams are available at:
```
rtsp://localhost:[port]/[camera-name]
```

For example:
- `rtsp://localhost:8554/LivingRoomCamera`
- `rtsp://localhost:8554/FrontDoor`
- `rtsp://localhost:8554/BackyardCam`
- `rtsp://localhost:8554/BackyardCam/sd` (for sub stream)

Use `rtsp list-endpoints` to see all available camera URLs.

## Authentication Process

1. Run `./tuya-ipc-terminal auth add [region] [email]`
2. A QR code will be displayed in your terminal
3. Open Tuya Smart or Smart Life app
4. Scan the QR code to authenticate
5. The session will be saved for future use

## Data Storage

All authentication sessions and camera information are stored in the `.tuya-data` directory:

```
.tuya-data/
├── user_eu-central_user_at_example_com.json  # User sessions
├── user_us-west_another_at_example_com.json
└── cameras.json                              # Camera registry
```

## Architecture

The project is structured as follows:

```
tuya-ipc-terminal/
├── cmd/
│   ├── auth/          # Authentication commands
│   ├── cameras/       # Camera management commands
│   ├── rtsp/          # RTSP server commands
│   └── root.go        # Root command setup
├── pkg/
│   ├── tuya/          # Tuya API client
│   ├── storage/       # Session & camera storage
│   ├── rtsp/          # RTSP server implementation
│   ├── webrtc/        # WebRTC implementation
│   └── utils/         # Utilities
└── main.go
```

## Technical Details

### WebRTC to RTSP Bridge

The tool creates a bridge between Tuya's WebRTC streams and standard RTSP:

1. **WebRTC Connection**: Establishes WebRTC connection to camera via Tuya's MQTT system
2. **RTP Packet Processing**: Receives RTP packets from WebRTC peer connection
3. **RTSP Server**: Serves processed streams via standard RTSP protocol
4. **Multi-client Support**: Multiple RTSP clients can connect to the same camera

### Supported Features

- **Video Codecs**: H.264, H.265/HEVC
- **Audio Codecs**: PCMU, PCMA, PCML
- **Stream Qualities**: HD (main stream), SD (sub stream)
- **Multi-client**: Multiple RTSP clients per camera
- **Auto-cleanup**: Automatic cleanup of inactive streams
- **Session Persistence**: Persistent authentication across restarts

### Stream Management

- **On-demand**: Streams start when first client connects
- **Auto-stop**: Streams stop when last client disconnects
- **Resource Efficient**: Minimal resource usage when no clients connected
- **Error Recovery**: Automatic reconnection on connection failures

## Supported Camera Types

The tool automatically detects cameras with the following categories:
- `sp` - Smart cameras
- `dghsxj` - Additional camera type

## Performance

- **Low Latency**: Direct WebRTC connection minimizes delay
- **Efficient**: Single WebRTC connection serves multiple RTSP clients
- **Scalable**: Supports multiple cameras and users simultaneously
- **Resource Aware**: Automatic cleanup of unused connections

## Troubleshooting

### Authentication Issues

If you get authentication errors:

1. Check session validity:
   ```bash
   ./tuya-ipc-terminal auth test [region] [email]
   ```

2. Refresh the session:
   ```bash
   ./tuya-ipc-terminal auth refresh [region] [email]
   ```

3. If problems persist, remove and re-add the user:
   ```bash
   ./tuya-ipc-terminal auth remove [region] [email]
   ./tuya-ipc-terminal auth add [region] [email]
   ```

### No Cameras Found

1. Ensure you have authenticated users:
   ```bash
   ./tuya-ipc-terminal auth list
   ```

2. Refresh camera discovery:
   ```bash
   ./tuya-ipc-terminal cameras refresh
   ```

3. Check if cameras are online in the Tuya Smart app

### RTSP Connection Issues

1. Check server status:
   ```bash
   ./tuya-ipc-terminal rtsp status
   ```

2. Verify endpoints:
   ```bash
   ./tuya-ipc-terminal rtsp list-endpoints
   ```

3. Test with simple media player:
   ```bash
   ffplay rtsp://localhost:8554/[camera-path]
   ```

4. Check camera is online:
   ```bash
   ./tuya-ipc-terminal cameras list --online-only
   ```

### WebRTC Connection Issues

- Ensure cameras are online in Tuya Smart app
- Check network connectivity to Tuya servers
- Verify MQTT connection is stable
- Look for WebRTC error messages in server logs

### Stream Quality Issues

1. Check camera supports desired quality:
   ```bash
   ./tuya-ipc-terminal cameras info [camera-id]
   ```

2. Try different stream resolution (HD vs SD)
3. Check network bandwidth and stability

Note: Some cameras may not support all resolutions.

## Advanced Usage

### Running as System Service

Create a systemd service file for background operation:

```ini
[Unit]
Description=Tuya IPC Terminal RTSP Server
After=network.target

[Service]
Type=simple
User=your-user
WorkingDirectory=/path/to/tuya-ipc-terminal
ExecStart=/path/to/tuya-ipc-terminal rtsp start --port 8554
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### Multiple Server Instances

Run multiple instances on different ports:

```bash
# Instance 1 - Port 8554
./tuya-ipc-terminal rtsp start --port 8554 --daemon

# Instance 2 - Port 8555  
./tuya-ipc-terminal rtsp start --port 8555 --daemon
```

### Integration with Home Assistant

Add cameras to Home Assistant using RTSP integration:

```yaml
camera:
  - platform: generic
    stream_source: rtsp://localhost:8554/LivingRoomCamera
    name: "Living Room Camera"
    
  - platform: generic
    stream_source: rtsp://localhost:8554/FrontDoor
    name: "Front Door Camera"
```

### Integration with Go2RTC:

```yaml
streams:
  MyCamera:
    - rtsp://localhost:8554/MyCamera
```

## Examples

### Complete Setup Workflow

```bash
# 1. Build the application
./build.sh

# 2. Add your Tuya account
./tuya-ipc-terminal auth add eu-central user@example.com
# Scan QR code with Tuya Smart app

# 3. Discover cameras
./tuya-ipc-terminal cameras refresh

# 4. List available cameras
./tuya-ipc-terminal cameras list

# 5. Start RTSP server
./tuya-ipc-terminal rtsp start --port 8554

# 6. Connect with media player
ffplay rtsp://localhost:8554/MyCamera
```

### Multi-user Setup

```bash
# Add multiple accounts
./tuya-ipc-terminal auth add eu-central user1@example.com
./tuya-ipc-terminal auth add us-west user2@example.com

# Refresh to get all cameras
./tuya-ipc-terminal cameras refresh

# List all users and cameras
./tuya-ipc-terminal auth list
./tuya-ipc-terminal cameras list
```

## API Integration

The tool can be extended with additional REST API endpoints for integration with other systems. The modular architecture allows easy addition of HTTP API servers alongside the RTSP server.

## Contributing

Contributions are welcome! Areas for improvement:

- REST API for camera control
- PTZ (Pan/Tilt/Zoom) control
- WebUI for management

Please feel free to submit issues and pull requests.

## Limitations

- Requires active internet connection to Tuya servers
- Camera streams depend on Tuya Cloud availability  
- QR code authentication requires mobile app
- Some advanced camera features may not be supported
- Session management is basic and may require manual refresh

## Security Considerations

- Sessions are stored locally in JSON files
- No encryption of stored credentials beyond Tuya's own
- Firewall configuration recommended for external access

## License

MIT

## Disclaimer

This tool is created through reverse engineering of Tuya's web client. Use at your own risk and ensure compliance with Tuya's terms of service. The authors are not responsible for any issues arising from the use of this software.