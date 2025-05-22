package rtsp

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"tuya-ipc-terminal/pkg/storage"
	"tuya-ipc-terminal/pkg/tuya"

	"github.com/pion/rtp"
)

// RTSPRequest represents an RTSP request
type RTSPRequest struct {
	Method  string
	URL     string
	Version string
	Headers map[string]string
	CSeq    int
}

// RTSPResponse represents an RTSP response
type RTSPResponse struct {
	Version    string
	StatusCode int
	Status     string
	Headers    map[string]string
	Body       string
}

// parseRTSPRequest parses an RTSP request from connection
func parseRTSPRequest(conn net.Conn) (*RTSPRequest, error) {
	reader := bufio.NewReader(conn)

	// Read request line
	line, _, err := reader.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("failed to read request line: %v", err)
	}

	parts := strings.Split(string(line), " ")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid request line: %s", string(line))
	}

	request := &RTSPRequest{
		Method:  parts[0],
		URL:     parts[1],
		Version: parts[2],
		Headers: make(map[string]string),
	}

	// Read headers
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			return nil, fmt.Errorf("failed to read header: %v", err)
		}

		lineStr := string(line)
		if lineStr == "" {
			break // End of headers
		}

		// Parse header
		colonIndex := strings.Index(lineStr, ":")
		if colonIndex == -1 {
			continue
		}

		key := strings.TrimSpace(lineStr[:colonIndex])
		value := strings.TrimSpace(lineStr[colonIndex+1:])
		request.Headers[key] = value

		// Extract CSeq
		if strings.ToLower(key) == "cseq" {
			if cseq, err := strconv.Atoi(value); err == nil {
				request.CSeq = cseq
			}
		}
	}

	return request, nil
}

// sendRTSPResponse sends an RTSP response
func sendRTSPResponse(conn net.Conn, statusCode int, status string, body string) error {
	response := fmt.Sprintf("RTSP/1.0 %d %s\r\n", statusCode, status)
	response += fmt.Sprintf("Server: TuyaIPCTerminal/1.0\r\n")
	response += fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"))

	if body != "" {
		response += fmt.Sprintf("Content-Length: %d\r\n", len(body))
		response += fmt.Sprintf("Content-Type: text/plain\r\n")
	}

	response += "\r\n"

	if body != "" {
		response += body
	}

	_, err := conn.Write([]byte(response))
	return err
}

// sendRTSPResponseWithHeaders sends an RTSP response with custom headers
func sendRTSPResponseWithHeaders(conn net.Conn, statusCode int, status string, headers map[string]string, body string) error {
	response := fmt.Sprintf("RTSP/1.0 %d %s\r\n", statusCode, status)
	response += fmt.Sprintf("Server: TuyaIPCTerminal/1.0\r\n")
	response += fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"))

	// Add custom headers
	for key, value := range headers {
		response += fmt.Sprintf("%s: %s\r\n", key, value)
	}

	if body != "" {
		response += fmt.Sprintf("Content-Length: %d\r\n", len(body))
		response += fmt.Sprintf("Content-Type: application/sdp\r\n")
	}

	response += "\r\n"

	if body != "" {
		response += body
	}

	_, err := conn.Write([]byte(response))
	return err
}

// extractCameraPath extracts camera path from RTSP URL
func extractCameraPath(rtspURL string) (string, string) {
	// Parse URL
	parsed, err := url.Parse(rtspURL)
	if err != nil {
		return "", ""
	}

	// Return path (e.g., "/MyCamera")
	path := parsed.Path
	if path == "" || path == "/" {
		return "", ""
	}

	streamResolution := "hd" // Default to HD

	// check if ends with "/hd" or "/sd"
	if strings.HasSuffix(path, "/hd") {
		streamResolution = "hd"
		path = strings.TrimSuffix(path, "/hd")
	} else if strings.HasSuffix(path, "/sd") {
		streamResolution = "sd"
		path = strings.TrimSuffix(path, "/sd")
	}

	return path, streamResolution
}

// generateSessionID generates a random session ID
func generateSessionID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// handleRTSPProtocol handles the RTSP protocol for a client
func (s *RTSPServer) handleRTSPProtocol(client *RTSPClient, initialRequest *RTSPRequest) {
	defer s.removeClient(client.session)

	s.handleRTSPMethod(client, initialRequest)

	for {
		client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Check für interleaved RTP (backchannel)
		reader := bufio.NewReader(client.conn)
		firstByte, err := reader.Peek(1)
		if err != nil {
			if !strings.Contains(err.Error(), "timeout") {
				fmt.Printf("Error peeking connection: %v\n", err)
			}
			break
		}

		// Handle interleaved RTP packet
		if len(firstByte) > 0 && firstByte[0] == '$' {
			if err := s.handleInterleavedRTP(client, reader); err != nil {
				fmt.Printf("Error handling interleaved RTP: %v\n", err)
				break
			}
			continue
		}

		// Handle regular RTSP request
		request, err := s.parseRTSPRequestFromReader(reader)
		if err != nil {
			if !strings.Contains(err.Error(), "timeout") {
				fmt.Printf("Error parsing RTSP request: %v\n", err)
			}
			break
		}

		s.handleRTSPMethod(client, request)
	}
}

// handleInterleavedRTP handles interleaved RTP packets
func (s *RTSPServer) handleInterleavedRTP(client *RTSPClient, reader *bufio.Reader) error {
	// Read interleaved header: $ + channel + length(2 bytes)
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return fmt.Errorf("failed to read interleaved header: %v", err)
	}

	if header[0] != '$' {
		return fmt.Errorf("invalid interleaved magic byte: %x", header[0])
	}

	channel := header[1]
	length := (int(header[2]) << 8) | int(header[3])

	// Read RTP data
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return fmt.Errorf("failed to read RTP data: %v", err)
	}

	// Check if this is backchannel
	if channel == client.backchannelAudioChannel {
		// fmt.Printf("Received backchannel RTP packet on channel %d\n", channel)

		// Parse und forward backchannel packet
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(data); err != nil {
			return fmt.Errorf("failed to parse backchannel RTP packet: %v", err)
		}

		// Forward to WebRTC bridge
		if client.stream != nil && client.stream.webrtcBridge != nil &&
			client.stream.webrtcBridge.rtpForwarder != nil &&
			client.stream.webrtcBridge.rtpForwarder.OnBackchannelAudio != nil {
			client.stream.webrtcBridge.rtpForwarder.OnBackchannelAudio(packet)
		}
	}

	return nil
}

// parseRTSPRequestFromReader parses an RTSP request from a buffered reader
func (s *RTSPServer) parseRTSPRequestFromReader(reader *bufio.Reader) (*RTSPRequest, error) {
	// Read request line
	line, _, err := reader.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("failed to read request line: %v", err)
	}

	parts := strings.Split(string(line), " ")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid request line: %s", string(line))
	}

	request := &RTSPRequest{
		Method:  parts[0],
		URL:     parts[1],
		Version: parts[2],
		Headers: make(map[string]string),
	}

	// Read headers
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			return nil, fmt.Errorf("failed to read header: %v", err)
		}

		lineStr := string(line)
		if lineStr == "" {
			break // End of headers
		}

		// Parse header
		colonIndex := strings.Index(lineStr, ":")
		if colonIndex == -1 {
			continue
		}

		key := strings.TrimSpace(lineStr[:colonIndex])
		value := strings.TrimSpace(lineStr[colonIndex+1:])
		request.Headers[key] = value

		// Extract CSeq
		if strings.ToLower(key) == "cseq" {
			if cseq, err := strconv.Atoi(value); err == nil {
				request.CSeq = cseq
			}
		}
	}

	return request, nil
}

// handleRTSPMethod handles different RTSP methods
func (s *RTSPServer) handleRTSPMethod(client *RTSPClient, request *RTSPRequest) {
	switch request.Method {
	case "OPTIONS":
		s.handleOptions(client, request)
	case "DESCRIBE":
		s.handleDescribe(client, request)
	case "SETUP":
		s.handleSetup(client, request)
	case "PLAY":
		s.handlePlay(client, request)
	case "TEARDOWN":
		s.handleTeardown(client, request)
	default:
		s.handleUnsupportedMethod(client, request)
	}
}

// handleOptions handles OPTIONS request
func (s *RTSPServer) handleOptions(client *RTSPClient, request *RTSPRequest) {
	headers := map[string]string{
		"CSeq":   strconv.Itoa(request.CSeq),
		"Public": "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN",
	}

	sendRTSPResponseWithHeaders(client.conn, 200, "OK", headers, "")
}

// handleDescribe handles DESCRIBE request
func (s *RTSPServer) handleDescribe(client *RTSPClient, request *RTSPRequest) {
	// Generate SDP for the camera stream
	sdp := s.generateSDP(client.stream.camera, request.URL)

	headers := map[string]string{
		"CSeq":          strconv.Itoa(request.CSeq),
		"Content-Base":  request.URL,
		"Cache-Control": "no-cache",
	}

	sendRTSPResponseWithHeaders(client.conn, 200, "OK", headers, sdp)
}

// handleSetup handles SETUP request
func (s *RTSPServer) handleSetup(client *RTSPClient, request *RTSPRequest) {
	// Parse transport header
	transport := request.Headers["Transport"]
	if transport == "" {
		sendRTSPResponse(client.conn, 400, "Bad Request", "Transport header missing")
		return
	}

	isBackchannel := strings.Contains(request.URL, "/backchannel")
	isVideoTrack := strings.Contains(request.URL, "/video")
	isAudioTrack := strings.Contains(request.URL, "/audio")

	fmt.Printf("Setup track - Video: %v, Audio: %v, Backchannel: %v\n", isVideoTrack, isAudioTrack, isBackchannel)

	var responseTransport string

	// Check transport mode
	if strings.Contains(transport, "RTP/AVP/TCP") {
		// TCP Interleaved mode
		client.transportMode = TransportTCP

		var rtpChannel, rtcpChannel byte

		// Parse interleaved channels if specified by client
		if strings.Contains(transport, "interleaved=") {
			parts := strings.Split(transport, "interleaved=")
			if len(parts) > 1 {
				channelPart := strings.Split(strings.Split(parts[1], ";")[0], "-")
				if len(channelPart) >= 1 {
					var ch int
					fmt.Sscanf(channelPart[0], "%d", &ch)
					rtpChannel = byte(ch)
				}
				if len(channelPart) >= 2 {
					var ch int
					fmt.Sscanf(channelPart[1], "%d", &ch)
					rtcpChannel = byte(ch)
				} else {
					rtcpChannel = rtpChannel + 1
				}
			}
		} else {
			// Assign default channels if not specified
			if isVideoTrack {
				rtpChannel = 0  // Video RTP
				rtcpChannel = 1 // Video RTCP
			} else if isAudioTrack {
				rtpChannel = 2  // Audio RTP
				rtcpChannel = 3 // Audio RTCP
			} else if isBackchannel {
				rtpChannel = 4  // Backchannel RTP
				rtcpChannel = 5 // Backchannel RTCP
			}
		}

		// Set the appropriate channel for this track
		if isVideoTrack {
			client.videoChannel = rtpChannel
			fmt.Printf("Setup VIDEO track - RTP channel: %d, RTCP channel: %d\n", rtpChannel, rtcpChannel)
		} else if isAudioTrack {
			client.audioChannel = rtpChannel
			fmt.Printf("Setup AUDIO track - RTP channel: %d, RTCP channel: %d\n", rtpChannel, rtcpChannel)
		} else if isBackchannel {
			client.backchannelAudioChannel = rtpChannel
			fmt.Printf("Setup BACKCHANNEL track - RTP channel: %d, RTCP channel: %d\n", rtpChannel, rtcpChannel)
		}

		responseTransport = fmt.Sprintf("RTP/AVP/TCP;unicast;interleaved=%d-%d",
			rtpChannel, rtcpChannel)

	} else if strings.Contains(transport, "RTP/AVP") {
		// UDP mode
		client.transportMode = TransportUDP

		// Parse client ports
		var clientRTPPort, clientRTCPPort int
		if strings.Contains(transport, "client_port=") {
			parts := strings.Split(transport, "client_port=")
			if len(parts) > 1 {
				portParts := strings.Split(strings.Split(parts[1], ";")[0], "-")
				if len(portParts) >= 1 {
					fmt.Sscanf(portParts[0], "%d", &clientRTPPort)
				}
				if len(portParts) >= 2 {
					fmt.Sscanf(portParts[1], "%d", &clientRTCPPort)
				}
			}
		}

		if clientRTPPort == 0 {
			sendRTSPResponse(client.conn, 400, "Bad Request", "Invalid client ports")
			return
		}

		// Determine if this is video or audio setup
		isVideoTrack := strings.Contains(request.URL, "/video") || !strings.Contains(request.URL, "/audio")
		if isVideoTrack {
			client.videoPort = clientRTPPort
		} else if isAudioTrack {
			client.audioPort = clientRTPPort
		} else if isBackchannel {
			client.backchannelAudioPort = clientRTPPort
		}

		responseTransport = fmt.Sprintf("RTP/AVP;unicast;client_port=%d-%d;server_port=8000-8001",
			clientRTPPort, clientRTCPPort)

		fmt.Printf("UDP setup - Video port: %d, Audio port: %d\n",
			client.videoPort, client.audioPort)

	} else {
		sendRTSPResponse(client.conn, 461, "Unsupported Transport", "Only RTP/AVP and RTP/AVP/TCP supported")
		return
	}

	// Increment setup count
	client.setupCount++
	fmt.Printf("Client %s setup count: %d (video ch:%d, audio ch:%d)\n",
		client.session, client.setupCount, client.videoChannel, client.audioChannel)

	// Add/update client in RTP forwarder
	var err error
	if client.transportMode == TransportTCP {
		// For TCP, add/update client after each setup
		err = client.stream.webrtcBridge.rtpForwarder.AddTCPClient(client.session, client.conn,
			client.videoChannel, client.audioChannel)
	} else if client.transportMode == TransportUDP {
		// For UDP, wait until we have both ports
		if client.videoPort > 0 && client.audioPort > 0 {
			err = client.stream.webrtcBridge.rtpForwarder.AddUDPClient(client.session,
				client.videoPort, client.audioPort)
		}
	}

	if err != nil {
		fmt.Printf("Error adding RTP client: %v\n", err)
		sendRTSPResponse(client.conn, 500, "Internal Server Error", "Failed to setup RTP forwarding")
		return
	}

	headers := map[string]string{
		"CSeq":      strconv.Itoa(request.CSeq),
		"Transport": responseTransport,
		"Session":   client.session + ";timeout=60",
	}

	sendRTSPResponseWithHeaders(client.conn, 200, "OK", headers, "")
}

// handlePlay handles PLAY request
func (s *RTSPServer) handlePlay(client *RTSPClient, request *RTSPRequest) {
	// Validate session
	sessionHeader := request.Headers["Session"]
	if sessionHeader == "" || !strings.Contains(sessionHeader, client.session) {
		sendRTSPResponse(client.conn, 454, "Session Not Found", "")
		return
	}

	headers := map[string]string{
		"CSeq":     strconv.Itoa(request.CSeq),
		"Session":  client.session,
		"Range":    "npt=0.000-",
		"RTP-Info": fmt.Sprintf("url=%s;seq=1;rtptime=0", request.URL),
	}

	sendRTSPResponseWithHeaders(client.conn, 200, "OK", headers, "")

	// Start streaming (this would typically start RTP packet transmission)
	fmt.Printf("Starting RTSP stream for client %s\n", client.session)
}

// handleTeardown handles TEARDOWN request
func (s *RTSPServer) handleTeardown(client *RTSPClient, request *RTSPRequest) {
	headers := map[string]string{
		"CSeq":    strconv.Itoa(request.CSeq),
		"Session": client.session,
	}

	sendRTSPResponseWithHeaders(client.conn, 200, "OK", headers, "")

	fmt.Printf("Tearing down RTSP stream for client %s\n", client.session)
}

// handleUnsupportedMethod handles unsupported RTSP methods
func (s *RTSPServer) handleUnsupportedMethod(client *RTSPClient, request *RTSPRequest) {
	headers := map[string]string{
		"CSeq": strconv.Itoa(request.CSeq),
	}

	sendRTSPResponseWithHeaders(client.conn, 501, "Not Implemented", headers, "")
}

func (s *RTSPServer) generateSDP(camera *storage.CameraInfo, baseURL string) string {
	sdp := "v=0\r\n"
	sdp += fmt.Sprintf("o=- %d %d IN IP4 0.0.0.0\r\n", time.Now().Unix(), time.Now().Unix())
	sdp += "s=Tuya Camera Stream\r\n"
	sdp += "c=IN IP4 0.0.0.0\r\n"
	sdp += "t=0 0\r\n"
	sdp += "a=control:*\r\n"
	sdp += "a=range:npt=0-\r\n"

	var skill *tuya.Skill
	err := json.Unmarshal([]byte(camera.Skill), &skill)
	if err != nil {
		fmt.Printf("Error unmarshalling skill: %v\n", err)
		return ""
	}

	audioSdp := ""
	videoSdp := ""

	// Video media description based on skill
	if skill != nil && len(skill.Videos) > 0 {
		// Verwende HD stream (streamType 2) als default
		streamType := tuya.GetStreamType(skill, "hd")
		isHEVC := tuya.IsHEVC(skill, streamType)

		var videoInfo *tuya.VideoSkill
		for _, video := range skill.Videos {
			if video.StreamType == streamType {
				videoInfo = &video
				break
			}
		}

		if videoInfo != nil {
			if isHEVC {
				// H.265/HEVC
				videoSdp += "m=video 0 RTP/AVP 96\r\n"
				videoSdp += "a=rtpmap:96 H265/90000\r\n"
				videoSdp += "a=fmtp:96 profile-id=1\r\n"
			} else {
				// H.264
				videoSdp += "m=video 0 RTP/AVP 96\r\n"
				videoSdp += "a=rtpmap:96 H264/90000\r\n"
				videoSdp += "a=fmtp:96 packetization-mode=1;profile-level-id=42001e\r\n"
			}
		}
	} else {
		// Fallback in case no video stream is found
		videoSdp += "m=video 0 RTP/AVP 96\r\n"
		videoSdp += "a=rtpmap:96 H264/90000\r\n"
		videoSdp += "a=fmtp:96 packetization-mode=1;profile-level-id=42001e\r\n"
	}

	videoSdp += fmt.Sprintf("a=control:%s/video\r\n", baseURL)
	videoSdp += "a=recvonly\r\n"

	// Audio media description based on skill
	if skill != nil && len(skill.Audios) > 0 {
		audioInfo := skill.Audios[0] // Nehme ersten audio stream

		switch audioInfo.CodecType {
		// case 101: // PCML
		// 	audioSdp += "m=audio 0 RTP/AVP 97\r\n"
		// 	audioSdp += "a=rtpmap:97 L16/8000\r\n"
		case 101, 105: // PCML and PCMU
			audioSdp += "m=audio 0 RTP/AVP 0\r\n"
			audioSdp += "a=rtpmap:0 PCMU/8000\r\n"
		case 106: // PCMA
			audioSdp += "m=audio 0 RTP/AVP 8\r\n"
			audioSdp += "a=rtpmap:8 PCMA/8000\r\n"
		default:
			// Fallback
			audioSdp += "m=audio 0 RTP/AVP 0\r\n"
			audioSdp += "a=rtpmap:0 PCMU/8000\r\n"
		}
	} else {
		// Fallback in case no audio stream is found
		audioSdp += "m=audio 0 RTP/AVP 0\r\n"
		audioSdp += "a=rtpmap:0 PCMU/8000\r\n"
	}

	backchannelAudio := audioSdp
	backchannelAudio += fmt.Sprintf("a=control:%s/backchannel\r\n", baseURL)
	backchannelAudio += "a=sendonly\r\n"

	audioSdp += fmt.Sprintf("a=control:%s/audio\r\n", baseURL)
	audioSdp += "a=recvonly\r\n"

	finalSdp := sdp + videoSdp + audioSdp + backchannelAudio

	fmt.Printf("Generated SDP:\n%s\n", finalSdp)

	return finalSdp
}
