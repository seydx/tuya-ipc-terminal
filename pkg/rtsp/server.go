package rtsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"tuya-ipc-terminal/pkg/core"
	"tuya-ipc-terminal/pkg/storage"
	"tuya-ipc-terminal/pkg/tuya"
)

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

type RTSPClient struct {
	conn             net.Conn
	session          string
	cameraPath       string
	stream           *CameraStream
	reader           *bufio.Reader
	transportMode    TransportMode
	videoPort        int
	audioPort        int
	backAudioPort    int
	videoChannel     byte
	audioChannel     byte
	backAudioChannel byte
	setupCount       int
}

type CameraStream struct {
	camera       *storage.CameraInfo
	resolution   string
	user         *storage.UserSession
	webrtcBridge *WebRTCBridge
	clients      map[string]*RTSPClient
	mutex        sync.RWMutex
	active       bool
	lastActivity time.Time

	// Delayed shutdown
	shutdownTimer *time.Timer
	shutdownDelay time.Duration
}

type ServerConfig struct {
	Port                 int
	MaxClients           int
	StreamTimeout        time.Duration
	ConnectionTimeout    time.Duration
	EnableAuthentication bool
}

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

func (s *RTSPServer) Start() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		return errors.New("server is already running")
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %v", s.port, err)
	}

	s.listener = listener
	s.running = true

	core.Logger.Info().Msgf("RTSP Server started on port %d", s.port)
	core.Logger.Info().Msgf("Available endpoints:")

	// List available camera endpoints
	if err := s.printAvailableEndpoints(); err != nil {
		core.Logger.Warn().Msgf("Could not list camera endpoints: %v", err)
	}

	// Start accepting connections
	go s.acceptConnections()

	// Start cleanup routine
	go s.cleanupRoutine()

	return nil
}

func (s *RTSPServer) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.running {
		return errors.New("server is not running")
	}

	core.Logger.Info().Msg("Stopping RTSP server...")

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
	core.Logger.Info().Msg("RTSP server stopped")

	return nil
}

func (s *RTSPServer) IsRunning() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.running
}

func (s *RTSPServer) GetPort() int {
	return s.port
}

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

type ServerStats struct {
	Port         int  `json:"port"`
	Running      bool `json:"running"`
	ClientCount  int  `json:"clientCount"`
	StreamCount  int  `json:"activeStreamCount"`
	TotalStreams int  `json:"totalStreams"`
}

func (s *RTSPServer) acceptConnections() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				if s.running {
					core.Logger.Error().Err(err).Msg("Error accepting connection")
				}
				continue
			}

			// Handle connection in goroutine
			go s.handleConnection(conn)
		}
	}
}

func (s *RTSPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Parse initial RTSP request
	request, err := s.parseRTSPRequestFromReader(reader)
	if err != nil {
		core.Logger.Error().Err(err).Msg("Error parsing initial RTSP request")
		return
	}

	// Extract camera path from URL
	cameraPath, streamResolution := extractCameraPath(request.URL)
	if cameraPath == "" {
		core.Logger.Error().Msg("Invalid RTSP URL")
		sendRTSPResponse(conn, 400, "Bad Request", nil, "")
		return
	}

	// Find camera
	camera, user, err := s.findCamera(cameraPath)
	if err != nil {
		core.Logger.Error().Msgf("Error finding camera for path %s: %v", cameraPath, err)
		sendRTSPResponse(conn, 500, "Internal Server Error", nil, "")
		return
	}

	if camera == nil {
		core.Logger.Error().Msgf("Camera not found for path %s", cameraPath)
		sendRTSPResponse(conn, 404, "Not Found", nil, "")
		return
	}

	core.Logger.Info().Msgf("New RTSP connection for camera: %s (%s)", camera.DeviceName, camera.DeviceID)

	// Create or get existing stream
	stream, err := s.getOrCreateStream(camera, streamResolution, user)
	if err != nil {
		core.Logger.Error().Err(err).Msgf("Failed to create stream for camera %s", camera.DeviceName)
		sendRTSPResponse(conn, 500, "Internal Server Error", nil, "Failed to create stream")
		return
	}

	// Create RTSP client
	client := &RTSPClient{
		conn:             conn,
		reader:           reader,
		session:          generateSessionID(),
		cameraPath:       cameraPath,
		stream:           stream,
		transportMode:    TransportUDP, // Default to UDP
		videoPort:        0,
		audioPort:        0,
		backAudioPort:    0,
		videoChannel:     0,
		audioChannel:     1,
		backAudioChannel: 2,
		setupCount:       0,
	}

	// Add client to server and stream
	s.addClient(client)
	stream.AddClient(client)

	// Handle initial request
	s.handleRTSPMethod(client, request)

	// Handle further requests
	s.handleRTSPProtocol(client)
}

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

	return nil, nil, nil
}

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
	stream := NewCameraStream(camera, streamResolution, user, s.storageManager)

	stream.webrtcBridge.OnError = func(err error) {
		stream.mutex.Lock()
		defer stream.mutex.Unlock()

		if stream.active {
			core.Logger.Error().Err(err).Msgf("WebRTC error for camera %s", camera.DeviceName)
			stream.stopStreamInternal()

			s.mutex.Lock()
			delete(s.streams, streamId)
			s.mutex.Unlock()
		}
	}

	s.streams[streamId] = stream

	core.Logger.Info().Msgf("Created new stream for camera: %s", camera.DeviceName)
	return stream, nil
}

func (s *RTSPServer) addClient(client *RTSPClient) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.clients[client.session] = client
}

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

func (s *RTSPServer) printAvailableEndpoints() error {
	cameras, err := s.storageManager.GetAllCameras()
	if err != nil {
		return err
	}

	if len(cameras) == 0 {
		core.Logger.Warn().Msg("  No cameras available. Run 'cameras refresh' first.")
		return nil
	}

	for _, camera := range cameras {
		var skill *tuya.Skill
		json.Unmarshal([]byte(camera.Skill), &skill)

		supportClarity := skill != nil && (skill.WebRTC&(1<<5)) != 0
		baseUrl := fmt.Sprintf("rtsp://localhost:%d%s", s.port, camera.RTSPPath)

		if supportClarity {
			core.Logger.Info().Msgf("  %s/hd (%s)", baseUrl, camera.DeviceName)
			core.Logger.Info().Msgf("  %s/sd (%s)", baseUrl, camera.DeviceName)
		} else {
			core.Logger.Info().Msgf("  %s (%s)", baseUrl, camera.DeviceName)
		}
	}

	return nil
}

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

func (s *RTSPServer) cleanupInactiveStreams() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	for deviceID, stream := range s.streams {
		// Remove streams inactive for more than 5 minutes
		if now.Sub(stream.lastActivity) > 5*time.Minute && len(stream.clients) == 0 {
			core.Logger.Debug().Msgf("Cleaning up inactive stream for camera: %s", stream.camera.DeviceName)
			stream.Stop()
			delete(s.streams, deviceID)
		}
	}
}

func NewCameraStream(camera *storage.CameraInfo, resolution string, user *storage.UserSession, storageManager *storage.StorageManager) *CameraStream {
	stream := &CameraStream{
		camera:        camera,
		resolution:    resolution,
		user:          user,
		clients:       make(map[string]*RTSPClient),
		active:        false,
		lastActivity:  time.Now(),
		shutdownDelay: 5 * time.Second, // 5 second delay before shutdown
	}

	stream.webrtcBridge = NewWebRTCBridge(camera, resolution, user, storageManager)

	return stream
}

func (cs *CameraStream) AddClient(client *RTSPClient) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	// Cancel any pending shutdown
	if cs.shutdownTimer != nil {
		cs.shutdownTimer.Stop()
		cs.shutdownTimer = nil
		core.Logger.Debug().Msgf("Cancelled pending shutdown for camera %s - new client connected", cs.camera.DeviceName)
	}

	cs.clients[client.session] = client
	cs.lastActivity = time.Now()

	// Start stream if not active
	if !cs.active {
		go cs.startStream()
	}
}

func (cs *CameraStream) RemoveClient(sessionID string) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	// Remove from RTP forwarder
	if cs.webrtcBridge != nil && cs.webrtcBridge.rtpForwarder != nil {
		cs.webrtcBridge.rtpForwarder.RemoveClient(sessionID)
	}

	delete(cs.clients, sessionID)
	cs.lastActivity = time.Now()

	// Schedule stream shutdown if no clients
	if len(cs.clients) == 0 && cs.active {
		cs.scheduleShutdown()
	}
}

func (cs *CameraStream) SetShutdownDelay(delay time.Duration) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	cs.shutdownDelay = delay
}

func (cs *CameraStream) Stop() {
	cs.stopStream()
}

func (cs *CameraStream) startStream() {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if cs.active {
		return
	}

	core.Logger.Info().Msgf("Starting stream for camera: %s", cs.camera.DeviceName)

	if err := cs.webrtcBridge.Start(); err != nil {
		core.Logger.Error().Err(err).Msg("Failed to start WebRTC bridge")
		return
	}

	cs.active = true
}

func (cs *CameraStream) stopStream() {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	cs.stopStreamInternal()
}

func (cs *CameraStream) stopStreamInternal() {
	if !cs.active {
		return
	}

	// Cancel any pending shutdown
	if cs.shutdownTimer != nil {
		cs.shutdownTimer.Stop()
		cs.shutdownTimer = nil
	}

	core.Logger.Info().Msgf("Stopping stream for camera: %s", cs.camera.DeviceName)

	if cs.webrtcBridge != nil {
		cs.webrtcBridge.Stop()
	}
	cs.active = false
}

// scheduleShutdown schedules a delayed shutdown
func (cs *CameraStream) scheduleShutdown() {
	// Cancel any existing timer
	if cs.shutdownTimer != nil {
		cs.shutdownTimer.Stop()
	}

	core.Logger.Debug().Msgf("Scheduling shutdown for camera %s in %v", cs.camera.DeviceName, cs.shutdownDelay)

	cs.shutdownTimer = time.AfterFunc(cs.shutdownDelay, func() {
		cs.mutex.Lock()
		defer cs.mutex.Unlock()

		// Double-check no clients connected during the delay
		if len(cs.clients) == 0 && cs.active {
			core.Logger.Info().Msgf("Executing delayed shutdown for camera %s", cs.camera.DeviceName)
			cs.stopStreamInternal()
		}

		cs.shutdownTimer = nil
	})
}
