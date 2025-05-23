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
	"regexp"
	"strconv"
	"strings"
	"time"
	"tuya-ipc-terminal/pkg/core"
	"tuya-ipc-terminal/pkg/storage"
	"tuya-ipc-terminal/pkg/tuya"

	"github.com/pion/rtp"
)

type RTSPRequest struct {
	Method  string
	URL     string
	Version string
	Headers map[string]string
	CSeq    int
}

type RTSPResponse struct {
	Version    string
	StatusCode int
	Status     string
	Headers    map[string]string
	Body       string
}

func sendRTSPResponse(conn net.Conn, statusCode int, status string, headers map[string]string, body string) error {
	var response strings.Builder

	// Status line
	fmt.Fprintf(&response, "RTSP/1.0 %d %s\r\n", statusCode, status)
	fmt.Fprintf(&response, "Server: TuyaIPCTerminal/1.0\r\n")
	fmt.Fprintf(&response, "Date: %s\r\n", time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"))

	// Add custom headers
	for key, value := range headers {
		fmt.Fprintf(&response, "%s: %s\r\n", key, value)
	}

	// Content headers if body exists
	if body != "" {
		fmt.Fprintf(&response, "Content-Length: %d\r\n", len(body))
		if _, hasContentType := headers["Content-Type"]; !hasContentType {
			fmt.Fprintf(&response, "Content-Type: application/sdp\r\n")
		}
	}

	// Empty line to separate headers from body
	response.WriteString("\r\n")

	// Add body if present
	if body != "" {
		response.WriteString(body)
	}

	responseStr := response.String()

	fmt.Println()
	core.Logger.Trace().Msgf("Sending RTSP response:")
	re := regexp.MustCompile(`\r\n|\r|\n`)
	lines := re.Split(responseStr, -1)
	for _, line := range lines {
		if line != "" {
			fmt.Println(line)
		}
	}
	fmt.Println()

	_, err := conn.Write([]byte(responseStr))
	return err
}

func extractCameraPath(rtspURL string) (string, string) {
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

func generateSessionID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (s *RTSPServer) handleRTSPProtocol(client *RTSPClient) {
	defer s.removeClient(client.session)

	for {
		client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Check for interleaved RTP (backchannel)
		firstByte, err := client.reader.Peek(1)
		if err != nil {
			if !strings.Contains(err.Error(), "timeout") {
				core.Logger.Error().Err(err).Msg("Error peeking connection")
			}
			break
		}

		// Handle interleaved RTP packet
		if len(firstByte) > 0 && firstByte[0] == '$' {
			if err := s.handleInterleavedRTP(client); err != nil {
				core.Logger.Error().Err(err).Msg("Error handling interleaved RTP")
				break
			}
			continue
		}

		// Handle regular RTSP request
		request, err := s.parseRTSPRequestFromReader(client.reader)
		if err != nil {
			if !strings.Contains(err.Error(), "timeout") {
				core.Logger.Error().Err(err).Msg("Error parsing RTSP request")
			}
			break
		}

		s.handleRTSPMethod(client, request)
	}
}

func (s *RTSPServer) handleInterleavedRTP(client *RTSPClient) error {
	// Read interleaved header: $ + channel + length(2 bytes)
	header := make([]byte, 4)
	if _, err := io.ReadFull(client.reader, header); err != nil {
		return fmt.Errorf("failed to read interleaved header: %v", err)
	}

	if header[0] != '$' {
		return fmt.Errorf("invalid interleaved magic byte: %x", header[0])
	}

	channel := header[1]
	length := (int(header[2]) << 8) | int(header[3])

	// Read RTP data
	data := make([]byte, length)
	if _, err := io.ReadFull(client.reader, data); err != nil {
		return fmt.Errorf("failed to read RTP data: %v", err)
	}

	// Check if this is backchannel
	if channel == client.backAudioChannel {
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

	fmt.Println()
	core.Logger.Trace().Msg("Received RTSP request:")
	fmt.Printf("%s %s %s\n", request.Method, request.URL, request.Version)
	for key, value := range request.Headers {
		fmt.Printf("%s: %s\n", key, value)
	}
	fmt.Println()

	return request, nil
}

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

func (s *RTSPServer) handleOptions(client *RTSPClient, request *RTSPRequest) {
	headers := map[string]string{
		"CSeq":   strconv.Itoa(request.CSeq),
		"Public": "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN",
	}

	sendRTSPResponse(client.conn, 200, "OK", headers, "")
}

func (s *RTSPServer) handleDescribe(client *RTSPClient, request *RTSPRequest) {
	// Generate SDP for the camera stream
	sdp := s.generateSDP(client.stream.camera, request.URL)

	headers := map[string]string{
		"CSeq":          strconv.Itoa(request.CSeq),
		"Content-Base":  request.URL,
		"Cache-Control": "no-cache",
	}

	sendRTSPResponse(client.conn, 200, "OK", headers, sdp)
}

func (s *RTSPServer) handleSetup(client *RTSPClient, request *RTSPRequest) {
	transport := request.Headers["Transport"]
	if transport == "" {
		sendRTSPResponse(client.conn, 400, "Bad Request", nil, "Transport header missing")
		return
	}

	isBackchannel := strings.Contains(request.URL, "/backchannel")
	isVideoTrack := strings.Contains(request.URL, "/video")
	isAudioTrack := strings.Contains(request.URL, "/audio")

	core.Logger.Trace().Msgf("Setup track - Video: %v, Audio: %v, Backchannel: %v", isVideoTrack, isAudioTrack, isBackchannel)

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

		if isVideoTrack {
			client.videoChannel = rtpChannel
			core.Logger.Trace().Msgf("Setup video track - RTP channel: %d, RTCP channel: %d", rtpChannel, rtcpChannel)
		} else if isAudioTrack {
			client.audioChannel = rtpChannel
			core.Logger.Trace().Msgf("Setup audio track - RTP channel: %d, RTCP channel: %d", rtpChannel, rtcpChannel)
		} else if isBackchannel {
			client.backAudioChannel = rtpChannel
			core.Logger.Trace().Msgf("Setup backchannel track - RTP channel: %d, RTCP channel: %d", rtpChannel, rtcpChannel)
		}

		responseTransport = fmt.Sprintf("RTP/AVP/TCP;unicast;interleaved=%d-%d",
			rtpChannel, rtcpChannel)

		// For TCP, add/update client after each setup
		err := client.stream.webrtcBridge.rtpForwarder.AddTCPClient(client.session, client.conn,
			client.videoChannel, client.audioChannel, client.backAudioChannel)
		if err != nil {
			core.Logger.Error().Err(err).Msg("Error adding TCP RTP client")
			sendRTSPResponse(client.conn, 500, "Internal Server Error", nil,
				"Failed to setup RTP forwarding")
			return
		}

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
			sendRTSPResponse(client.conn, 400, "Bad Request", nil, "Invalid client ports")
			return
		}

		// Default server ports (used for non-backchannel tracks)
		var serverRTPPort, serverRTCPPort int = 0, 0

		// Store client ports based on track type
		if isVideoTrack {
			client.videoPort = clientRTPPort
		} else if isAudioTrack {
			client.audioPort = clientRTPPort
		} else if isBackchannel {
			// For backchannel, setup the server listener and get actual server port
			if client.stream != nil && client.stream.webrtcBridge != nil {
				port, err := client.stream.webrtcBridge.rtpForwarder.SetupUDPBackchannel(
					client.session, clientRTPPort)
				if err != nil {
					core.Logger.Error().Err(err).Msg("Failed to setup UDP backchannel")
					sendRTSPResponse(client.conn, 500, "Internal Server Error", nil,
						"Failed to setup backchannel")
					return
				}
				serverRTPPort = port
				serverRTCPPort = port + 1
			}
		}

		// Build response transport
		if serverRTPPort > 0 {
			// Include server ports for backchannel
			responseTransport = fmt.Sprintf("RTP/AVP;unicast;client_port=%d-%d;server_port=%d-%d",
				clientRTPPort, clientRTCPPort, serverRTPPort, serverRTCPPort)
		} else {
			// No server ports for video/audio (we're only receiving)
			responseTransport = fmt.Sprintf("RTP/AVP;unicast;client_port=%d-%d",
				clientRTPPort, clientRTCPPort)
		}

		core.Logger.Trace().Msgf("UDP setup - Track type: video=%v audio=%v backchannel=%v, Client port: %d, Server port: %d",
			isVideoTrack, isAudioTrack, isBackchannel, clientRTPPort, serverRTPPort)

		// Add/update UDP client with current ports
		err := client.stream.webrtcBridge.rtpForwarder.AddUDPClient(client.session,
			client.videoPort, client.audioPort)
		if err != nil {
			core.Logger.Error().Err(err).Msg("Error adding UDP RTP client")
			sendRTSPResponse(client.conn, 500, "Internal Server Error", nil,
				"Failed to setup RTP forwarding")
			return
		}

	} else {
		sendRTSPResponse(client.conn, 461, "Unsupported Transport", nil,
			"Only RTP/AVP and RTP/AVP/TCP supported")
		return
	}

	// Increment setup count
	client.setupCount++

	core.Logger.Trace().Msgf("Client %s setup count: %d", client.session, client.setupCount)

	headers := map[string]string{
		"CSeq":      strconv.Itoa(request.CSeq),
		"Transport": responseTransport,
		"Session":   client.session + ";timeout=60",
	}

	sendRTSPResponse(client.conn, 200, "OK", headers, "")
}

func (s *RTSPServer) handlePlay(client *RTSPClient, request *RTSPRequest) {
	// Validate session
	sessionHeader := request.Headers["Session"]
	if sessionHeader == "" || !strings.Contains(sessionHeader, client.session) {
		sendRTSPResponse(client.conn, 454, "Session Not Found", nil, "")
		return
	}

	headers := map[string]string{
		"CSeq":     strconv.Itoa(request.CSeq),
		"Session":  client.session,
		"Range":    "npt=0.000-",
		"RTP-Info": fmt.Sprintf("url=%s;seq=1;rtptime=0", request.URL),
	}

	sendRTSPResponse(client.conn, 200, "OK", headers, "")

	core.Logger.Info().Msgf("Starting RTSP stream for client %s", client.session)
}

func (s *RTSPServer) handleTeardown(client *RTSPClient, request *RTSPRequest) {
	headers := map[string]string{
		"CSeq":    strconv.Itoa(request.CSeq),
		"Session": client.session,
	}

	sendRTSPResponse(client.conn, 200, "OK", headers, "")

	core.Logger.Info().Msgf("Tearing down RTSP stream for client %s", client.session)
}

func (s *RTSPServer) handleUnsupportedMethod(client *RTSPClient, request *RTSPRequest) {
	headers := map[string]string{
		"CSeq": strconv.Itoa(request.CSeq),
	}

	sendRTSPResponse(client.conn, 501, "Not Implemented", headers, "")
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
		core.Logger.Error().Err(err).Msg("Error unmarshalling skill")
		return ""
	}

	audioSdp := ""
	videoSdp := ""

	// Video media description based on skill
	if skill != nil && len(skill.Videos) > 0 {
		// HD (main) as default
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

	return finalSdp
}
