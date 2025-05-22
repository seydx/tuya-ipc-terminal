package rtsp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"tuya-ipc-terminal/pkg/storage"
	"tuya-ipc-terminal/pkg/tuya"
)

// RTSPServer represents the RTSP server
type RTSPServer struct {
	port           int
	listener       net.Listener
	storageManager *storage.StorageManager
	clients        map[string]*RTSPClient
	streams        map[string]*CameraStream
	mutex          sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	running        bool
}

// RTSPClient represents a connected RTSP client
type RTSPClient struct {
	conn                    net.Conn
	session                 string
	cameraPath              string
	stream                  *CameraStream
	transportMode           TransportMode
	videoPort               int
	audioPort               int
	backchannelAudioPort    int
	videoChannel            byte
	audioChannel            byte
	backchannelAudioChannel byte
	setupCount              int
}

// CameraStream represents an active camera stream
type CameraStream struct {
	camera       *storage.CameraInfo
	resolution   string
	user         *storage.UserSession
	webrtcBridge *WebRTCBridge
	// rtpForwarder *RTPForwarder
	clients      map[string]*RTSPClient
	mutex        sync.RWMutex
	active       bool
	lastActivity time.Time
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port                 int
	MaxClients           int
	StreamTimeout        time.Duration
	ConnectionTimeout    time.Duration
	EnableAuthentication bool
}

// NewRTSPServer creates a new RTSP server instance
func NewRTSPServer(port int, storageManager *storage.StorageManager) *RTSPServer {
	ctx, cancel := context.WithCancel(context.Background())

	return &RTSPServer{
		port:           port,
		storageManager: storageManager,
		clients:        make(map[string]*RTSPClient),
		streams:        make(map[string]*CameraStream),
		ctx:            ctx,
		cancel:         cancel,
		running:        false,
	}
}

// Start starts the RTSP server
func (s *RTSPServer) Start() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		return fmt.Errorf("server is already running")
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %v", s.port, err)
	}

	s.listener = listener
	s.running = true

	fmt.Printf("RTSP Server started on port %d\n", s.port)
	fmt.Printf("Available endpoints:\n")

	// List available camera endpoints
	if err := s.printAvailableEndpoints(); err != nil {
		fmt.Printf("Warning: Could not list camera endpoints: %v\n", err)
	}

	// Start accepting connections
	go s.acceptConnections()

	// Start cleanup routine
	go s.cleanupRoutine()

	return nil
}

// Stop stops the RTSP server
func (s *RTSPServer) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.running {
		return fmt.Errorf("server is not running")
	}

	fmt.Println("Stopping RTSP server...")

	// Cancel context to stop all goroutines
	s.cancel()

	// Close listener
	if s.listener != nil {
		s.listener.Close()
	}

	// Close all client connections
	for _, client := range s.clients {
		client.conn.Close()
	}

	// Stop all streams
	for _, stream := range s.streams {
		stream.Stop()
	}

	s.running = false
	fmt.Println("RTSP server stopped")

	return nil
}

// IsRunning returns whether the server is running
func (s *RTSPServer) IsRunning() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.running
}

// GetPort returns the server port
func (s *RTSPServer) GetPort() int {
	return s.port
}

// GetStats returns server statistics
func (s *RTSPServer) GetStats() ServerStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	activeStreams := 0
	for _, stream := range s.streams {
		if stream.active {
			activeStreams++
		}
	}

	return ServerStats{
		Port:         s.port,
		Running:      s.running,
		ClientCount:  len(s.clients),
		StreamCount:  activeStreams,
		TotalStreams: len(s.streams),
	}
}

// ServerStats holds server statistics
type ServerStats struct {
	Port         int  `json:"port"`
	Running      bool `json:"running"`
	ClientCount  int  `json:"clientCount"`
	StreamCount  int  `json:"activeStreamCount"`
	TotalStreams int  `json:"totalStreams"`
}

// acceptConnections accepts incoming RTSP connections
func (s *RTSPServer) acceptConnections() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				if s.running {
					fmt.Printf("Error accepting connection: %v\n", err)
				}
				continue
			}

			// Handle connection in goroutine
			go s.handleConnection(conn)
		}
	}
}

// handleConnection handles a single RTSP connection
func (s *RTSPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Parse RTSP request
	request, err := parseRTSPRequest(conn)
	if err != nil {
		fmt.Printf("Error parsing RTSP request: %v\n", err)
		return
	}

	// Extract camera path from URL
	cameraPath, streamResolution := extractCameraPath(request.URL)
	if cameraPath == "" {
		sendRTSPResponse(conn, 404, "Not Found", "")
		return
	}

	// Find camera
	camera, user, err := s.findCamera(cameraPath)
	if err != nil {
		fmt.Printf("Camera not found for path %s: %v\n", cameraPath, err)
		sendRTSPResponse(conn, 404, "Not Found", "Camera not found")
		return
	}

	fmt.Printf("New RTSP connection for camera: %s (%s)\n", camera.DeviceName, camera.DeviceID)

	// Create or get existing stream
	stream, err := s.getOrCreateStream(camera, streamResolution, user)
	if err != nil {
		fmt.Printf("Failed to create stream for camera %s: %v\n", camera.DeviceName, err)
		sendRTSPResponse(conn, 500, "Internal Server Error", "Failed to create stream")
		return
	}

	// Create RTSP client
	client := &RTSPClient{
		conn:          conn,
		session:       generateSessionID(),
		cameraPath:    cameraPath,
		stream:        stream,
		transportMode: TransportUDP, // Default to UDP
		videoPort:     0,            // Will be set during SETUP
		audioPort:     0,            // Will be set during SETUP
		videoChannel:  0,            // For TCP interleaved
		audioChannel:  1,            // For TCP interleaved
		setupCount:    0,
	}

	// Add client to server and stream
	s.addClient(client)
	stream.AddClient(client)

	// Handle RTSP protocol
	s.handleRTSPProtocol(client, request)
}

// findCamera finds a camera by RTSP path
func (s *RTSPServer) findCamera(path string) (*storage.CameraInfo, *storage.UserSession, error) {
	cameras, err := s.storageManager.GetAllCameras()
	if err != nil {
		return nil, nil, err
	}

	// Find camera by RTSP path
	for _, camera := range cameras {
		if camera.RTSPPath == path {
			// Get user for this camera
			users, err := s.storageManager.ListUsers()
			if err != nil {
				continue
			}

			for _, user := range users {
				if user.UserKey == camera.UserKey {
					return &camera, &user, nil
				}
			}
		}
	}

	return nil, nil, fmt.Errorf("camera not found for path: %s", path)
}

// getOrCreateStream gets existing stream or creates new one
func (s *RTSPServer) getOrCreateStream(camera *storage.CameraInfo, streamResolution string, user *storage.UserSession) (*CameraStream, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Check if stream already exists
	streamId := fmt.Sprintf("%s-%s", camera.DeviceID, streamResolution)
	if stream, exists := s.streams[streamId]; exists {
		stream.lastActivity = time.Now()
		return stream, nil
	}

	// Create new stream
	stream := &CameraStream{
		camera:       camera,
		resolution:   streamResolution,
		user:         user,
		clients:      make(map[string]*RTSPClient),
		active:       false,
		lastActivity: time.Now(),
	}

	// Create WebRTC bridge
	stream.webrtcBridge = NewWebRTCBridge(camera, streamResolution, user, s.storageManager)

	stream.webrtcBridge.OnError = func(err error) {
		if stream.active {
			fmt.Printf("WebRTC error for camera %s: %v\n", camera.DeviceName, err)
			stream.Stop()
			delete(s.streams, streamId)
		}
	}

	s.streams[streamId] = stream

	fmt.Printf("Created new stream for camera: %s\n", camera.DeviceName)
	return stream, nil
}

// addClient adds a client to the server
func (s *RTSPServer) addClient(client *RTSPClient) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.clients[client.session] = client
}

// removeClient removes a client from the server
func (s *RTSPServer) removeClient(sessionID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if client, exists := s.clients[sessionID]; exists {
		// Remove client from stream
		if client.stream != nil {
			client.stream.RemoveClient(sessionID)
		}

		delete(s.clients, sessionID)
		client.conn.Close()
	}
}

// printAvailableEndpoints prints available camera endpoints
func (s *RTSPServer) printAvailableEndpoints() error {
	cameras, err := s.storageManager.GetAllCameras()
	if err != nil {
		return err
	}

	if len(cameras) == 0 {
		fmt.Println("  No cameras available. Run 'cameras refresh' first.")
		return nil
	}

	for _, camera := range cameras {
		var skill *tuya.Skill
		json.Unmarshal([]byte(camera.Skill), &skill)

		supportClarity := skill != nil && (skill.WebRTC&(1<<5)) != 0
		baseUrl := fmt.Sprintf("rtsp://localhost:%d%s", s.port, camera.RTSPPath)

		if supportClarity {
			fmt.Printf("  %s/hd (%s)\n", baseUrl, camera.DeviceName)
			fmt.Printf("  %s/sd (%s)\n", baseUrl, camera.DeviceName)
		} else {
			fmt.Printf("  %s (%s)\n", baseUrl, camera.DeviceName)
		}
	}

	return nil
}

// cleanupRoutine periodically cleans up inactive streams
func (s *RTSPServer) cleanupRoutine() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.cleanupInactiveStreams()
		}
	}
}

// cleanupInactiveStreams removes streams that have been inactive
func (s *RTSPServer) cleanupInactiveStreams() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	for deviceID, stream := range s.streams {
		// Remove streams inactive for more than 5 minutes
		if now.Sub(stream.lastActivity) > 5*time.Minute && len(stream.clients) == 0 {
			fmt.Printf("Cleaning up inactive stream for camera: %s\n", stream.camera.DeviceName)
			stream.Stop()
			delete(s.streams, deviceID)
		}
	}
}

// AddClient adds a client to the stream
func (cs *CameraStream) AddClient(client *RTSPClient) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	cs.clients[client.session] = client
	cs.lastActivity = time.Now()

	// Start stream if not active
	if !cs.active {
		go cs.startStream()
	}
}

// RemoveClient removes a client from the stream
func (cs *CameraStream) RemoveClient(sessionID string) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	// Remove from RTP forwarder
	if cs.webrtcBridge.rtpForwarder != nil {
		cs.webrtcBridge.rtpForwarder.RemoveClient(sessionID)
	}

	delete(cs.clients, sessionID)
	cs.lastActivity = time.Now()

	// Stop stream if no clients
	if len(cs.clients) == 0 && cs.active {
		go cs.stopStream()
	}
}

// startStream starts the camera stream
func (cs *CameraStream) startStream() {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if cs.active {
		return
	}

	fmt.Printf("Starting stream for camera: %s\n", cs.camera.DeviceName)

	// Start WebRTC bridge
	if err := cs.webrtcBridge.Start(); err != nil {
		fmt.Printf("Failed to start WebRTC bridge: %v\n", err)
		return
	}

	cs.active = true
}

// stopStream stops the camera stream
func (cs *CameraStream) stopStream() {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if !cs.active {
		return
	}

	fmt.Printf("Stopping stream for camera: %s\n", cs.camera.DeviceName)

	// Stop WebRTC bridge
	cs.webrtcBridge.Stop()
	cs.active = false
}

// Stop stops the camera stream
func (cs *CameraStream) Stop() {
	cs.stopStream()
}
