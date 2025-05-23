package rtsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	pion "github.com/pion/webrtc/v4"
	"golang.org/x/net/publicsuffix"

	"tuya-ipc-terminal/pkg/core"
	"tuya-ipc-terminal/pkg/storage"
	"tuya-ipc-terminal/pkg/tuya"
	"tuya-ipc-terminal/pkg/utils"
	"tuya-ipc-terminal/pkg/webrtc"

	"github.com/pion/rtp"
)

type WebRTCBridge struct {
	camera         *storage.CameraInfo
	resolution     string
	streamType     int
	isHEVC         bool
	user           *storage.UserSession
	storageManager *storage.StorageManager

	// WebRTC components
	peerConnection *pion.PeerConnection
	dataChannel    *pion.DataChannel
	mqttClient     *tuya.MQTTClient
	cameraClient   *tuya.MQTTCameraClient
	rtpForwarder   *RTPForwarder

	// State
	connected bool
	waiter    utils.Waiter
	mutex     sync.RWMutex
	
	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// RTP forwarding
	videoTrack  *pion.TrackRemote
	audioTrack  *pion.TrackRemote
	backchannel *pion.TrackLocalStaticRTP

	// Callbacks
	OnVideoPacket func(packet *rtp.Packet)
	OnAudioPacket func(packet *rtp.Packet)
	OnError       func(error)
}

func NewWebRTCBridge(camera *storage.CameraInfo, streamResolution string, user *storage.UserSession, storageManager *storage.StorageManager) *WebRTCBridge {
	ctx, cancel := context.WithCancel(context.Background())
	
	wb := &WebRTCBridge{
		camera:         camera,
		resolution:     streamResolution,
		user:           user,
		rtpForwarder:   NewRTPForwarder(),
		storageManager: storageManager,
		connected:      false,
		waiter:         utils.Waiter{},
		ctx:            ctx,
		cancel:         cancel,
	}

	wb.rtpForwarder.OnBackchannelAudio = wb.ForwardBackchannelAudioPacket

	return wb
}

func (wb *WebRTCBridge) Start() error {
	wb.mutex.Lock()
	defer wb.mutex.Unlock()

	if wb.connected {
		return errors.New("bridge already connected")
	}

	core.Logger.Info().Msgf("Starting WebRTC bridge for camera: %s", wb.camera.DeviceName)

	// Create HTTP client with session
	httpClient := wb.createHTTPClient()
	if httpClient == nil {
		return errors.New("failed to create HTTP client")
	}

	// Get app info
	appInfo, err := tuya.GetAppInfo(httpClient, wb.user.SessionData.ServerHost)
	if err != nil {
		return fmt.Errorf("failed to get app info: %v", err)
	}

	// Get MQTT config
	mqttConfig, err := tuya.GetMQTTConfig(httpClient, wb.user.SessionData.ServerHost)
	if err != nil {
		return fmt.Errorf("failed to get MQTT config: %v", err)
	}

	// Connect to MQTT broker
	wb.mqttClient, err = tuya.NewMqttClient(
		appInfo.Result.ClientId,
		wb.user.SessionData.LoginResult.Domain.MobileMqttsUrl,
		&mqttConfig.Result,
	)

	if err != nil {
		return fmt.Errorf("failed to connect to MQTT: %v", err)
	}

	if err = wb.mqttClient.Connected.Wait(); err != nil {
		return fmt.Errorf("MQTT connection failed: %v", err)
	}

	// Get WebRTC configuration
	webRTCConfig, err := tuya.GetWebRTCConfig(httpClient, wb.user.SessionData.ServerHost, wb.camera.DeviceID)
	if err != nil {
		return fmt.Errorf("failed to get WebRTC config: %v", err)
	}

	// Parse skill information
	var skill tuya.Skill
	if err := json.Unmarshal([]byte(webRTCConfig.Result.Skill), &skill); err != nil {
		return fmt.Errorf("failed to parse skill info: %v", err)
	}

	// Determine stream settings
	wb.streamType = tuya.GetStreamType(&skill, wb.resolution)
	wb.isHEVC = tuya.IsHEVC(&skill, wb.streamType)

	core.Logger.Info().Msgf("Stream settings - Resolution: %s, Type: %d, HEVC: %v", wb.resolution, wb.streamType, wb.isHEVC)

	// Setup WebRTC peer connection
	if err := wb.setupPeerConnection(&webRTCConfig.Result); err != nil {
		return fmt.Errorf("failed to setup peer connection: %v", err)
	}

	// Setup MQTT camera client
	wb.setupMQTTCameraClient(&webRTCConfig.Result)

	// Create and send offer
	if err := wb.createAndSendOffer(); err != nil {
		return fmt.Errorf("failed to create offer: %v", err)
	}

	if err = wb.waiter.Wait(); err != nil {
		return fmt.Errorf("failed to establish connection: %v", err)
	}

	wb.connected = true
	core.Logger.Info().Msgf("WebRTC bridge started successfully for camera: %s", wb.camera.DeviceName)

	return nil
}

func (wb *WebRTCBridge) Stop() {
	wb.mutex.Lock()
	defer wb.mutex.Unlock()

	if !wb.connected {
		return
	}

	core.Logger.Info().Msgf("Stopping WebRTC bridge for camera: %s", wb.camera.DeviceName)

	// Cancel context to stop all goroutines
	wb.cancel()

	// Send disconnect
	if wb.cameraClient != nil {
		wb.cameraClient.SendDisconnect()
	}

	// Close peer connection
	if wb.peerConnection != nil {
		wb.peerConnection.Close()
	}

	// Stop MQTT client
	if wb.mqttClient != nil {
		wb.mqttClient.Stop()
	}

	if wb.rtpForwarder != nil {
		wb.rtpForwarder.Stop()
	}

	wb.connected = false
	core.Logger.Info().Msgf("WebRTC bridge stopped for camera: %s", wb.camera.DeviceName)
}

func (wb *WebRTCBridge) IsConnected() bool {
	wb.mutex.RLock()
	defer wb.mutex.RUnlock()
	return wb.connected
}

func (wb *WebRTCBridge) ForwardBackchannelAudioPacket(packet *rtp.Packet) {
	if wb.backchannel != nil {
		_ = wb.backchannel.WriteRTP(packet)
	}
}

func (wb *WebRTCBridge) setupPeerConnection(webRTCConfig *tuya.WebRTCConfig) error {
	// Convert ICE servers
	iceServerBytes, err := json.Marshal(webRTCConfig.P2PConfig.Ices)
	if err != nil {
		return fmt.Errorf("failed to marshal ICE servers: %v", err)
	}

	iceServers, err := webrtc.UnmarshalICEServers(iceServerBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal ICE servers: %v", err)
	}

	// Create peer connection configuration
	conf := pion.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: pion.ICETransportPolicyAll,
		BundlePolicy:       pion.BundlePolicyMaxBundle,
	}

	// Create WebRTC API
	api, err := webrtc.NewAPI()
	if err != nil {
		return fmt.Errorf("failed to create WebRTC API: %v", err)
	}

	// Create peer connection
	wb.peerConnection, err = api.NewPeerConnection(conf)
	if err != nil {
		return fmt.Errorf("failed to create peer connection: %v", err)
	}

	// On HEVC, use DataChannel to receive video/audio
	if wb.isHEVC {
		maxRetransmits := uint16(5)
		ordered := true

		wb.dataChannel, err = wb.peerConnection.CreateDataChannel("fmp4Stream", &pion.DataChannelInit{
			MaxRetransmits: &maxRetransmits,
			Ordered:        &ordered,
		})

		wb.dataChannel.OnMessage(func(msg pion.DataChannelMessage) {
			if msg.IsString {
				if connected, err := wb.probe(msg); err != nil {
					wb.handleError(err)
				} else if connected {
					wb.waiter.Done(nil)
				}
			} else {
				packet := &rtp.Packet{}
				if err := packet.Unmarshal(msg.Data); err != nil {
					// skip
					return
				}

				switch packet.SSRC {
				case wb.rtpForwarder.videoSSRC:
					wb.rtpForwarder.ForwardVideoPacket(packet)
				case wb.rtpForwarder.audioSSRC:
					wb.rtpForwarder.ForwardAudioPacket(packet)
				}
			}
		})

		wb.dataChannel.OnError(func(err error) {
			wb.handleError(err)
		})

		wb.dataChannel.OnClose(func() {
			wb.handleError(errors.New("datachannel: closed"))
		})

		wb.dataChannel.OnOpen(func() {
			codecRequest, _ := json.Marshal(tuya.DataChannelMessage{
				Type: "codec",
				Msg:  "",
			})

			if err := wb.sendMessageToDataChannel(codecRequest); err != nil {
				wb.handleError(fmt.Errorf("failed to send codec request: %w", err))
			}
		})
	}

	// Setup connection state handler
	wb.peerConnection.OnConnectionStateChange(func(state pion.PeerConnectionState) {
		if state == pion.PeerConnectionStateFailed || state == pion.PeerConnectionStateClosed {
			wb.handleError(errors.New("WebRTC connection failed/closed"))
		}

		if state == pion.PeerConnectionStateConnected {
			core.Logger.Info().Msgf("WebRTC connection established")

			if !wb.isHEVC && wb.resolution == "hd" {
				_ = wb.cameraClient.SendResolution(0)
				wb.waiter.Done(nil)
			}
		}
	})

	// Setup track handler for incoming media if not HEVC
	wb.peerConnection.OnTrack(func(track *pion.TrackRemote, receiver *pion.RTPReceiver) {
		codec := track.Codec()
		core.Logger.Trace().Msgf("Received track: %s, PayloadType: %d", codec.MimeType, codec.PayloadType)

		if track.Kind() == pion.RTPCodecTypeVideo {
			wb.videoTrack = track

			if !wb.isHEVC {
				go wb.handleVideoTrack(track)
			}
		} else if track.Kind() == pion.RTPCodecTypeAudio {
			wb.audioTrack = track

			for _, tr := range wb.peerConnection.GetTransceivers() {
				if tr.Receiver() == receiver && tr.Kind() == pion.RTPCodecTypeAudio {
					if tr.Direction() == pion.RTPTransceiverDirectionSendrecv || tr.Direction() == pion.RTPTransceiverDirectionSendonly {
						localTrack, _ := pion.NewTrackLocalStaticRTP(
							pion.RTPCodecCapability{MimeType: track.Codec().MimeType},
							"audio-backchannel", "pion",
						)
						tr.Sender().ReplaceTrack(localTrack)
						wb.backchannel = localTrack
						core.Logger.Trace().Msgf("Setup backchannel track")
						break
					}
				}
			}

			if !wb.isHEVC {
				go wb.handleAudioTrack(track)
			}
		}
	})

	return nil
}

func (wb *WebRTCBridge) setupMQTTCameraClient(webRTCConfig *tuya.WebRTCConfig) {
	device := &tuya.Device{
		DeviceId:   wb.camera.DeviceID,
		DeviceName: wb.camera.DeviceName,
		Category:   wb.camera.Category,
		ProductId:  wb.camera.ProductID,
		Uuid:       wb.camera.UUID,
	}

	// Create MQTT camera client
	wb.cameraClient = tuya.NewMqttCameraClient(wb.mqttClient, device, webRTCConfig)
	wb.mqttClient.AddCameraClient(wb.cameraClient.SessionId, wb.cameraClient)

	// Setup handlers
	wb.cameraClient.HandleAnswer = func(answer tuya.AnswerFrame) {
		core.Logger.Trace().Msgf("Received WebRTC answer")
		core.Logger.Trace().Msgf("Answer SDP: %s", answer.Sdp)

		desc := pion.SessionDescription{
			Type: pion.SDPTypePranswer,
			SDP:  answer.Sdp,
		}

		if err := wb.peerConnection.SetRemoteDescription(desc); err != nil {
			wb.handleError(err)
			return
		}

		if err := webrtc.SetAnswer(wb.peerConnection, answer.Sdp); err != nil {
			wb.handleError(err)
			return
		}
	}

	wb.cameraClient.HandleCandidate = func(candidate tuya.CandidateFrame) {
		core.Logger.Trace().Msgf("Received ICE candidate: %s", strings.Split(candidate.Candidate, "\r\n")[0])

		if candidate.Candidate != "" {
			err := wb.peerConnection.AddICECandidate(pion.ICECandidateInit{Candidate: candidate.Candidate})
			if err != nil {
				wb.handleError(err)
			}
		}
	}

	wb.cameraClient.HandleError = func(err error) {
		wb.handleError(err)
	}

	wb.cameraClient.HandleDisconnect = func() {
		wb.handleError(errors.New("camera client disconnected"))
		wb.connected = false
	}
}

func (wb *WebRTCBridge) createAndSendOffer() error {
	wb.peerConnection.OnICECandidate(func(candidate *pion.ICECandidate) {
		if candidate != nil {
			core.Logger.Trace().Msgf("Generated ICE candidate: %s", candidate.ToJSON().Candidate)

			if err := wb.cameraClient.SendCandidate("a=" + candidate.ToJSON().Candidate); err != nil {
				core.Logger.Error().Err(err).Msg("Error sending ICE candidate")
			}
		}
	})

	medias := []*utils.Media{
		{Kind: utils.KindAudio, Direction: utils.DirectionSendRecv},
		{Kind: utils.KindVideo, Direction: utils.DirectionRecvonly},
	}

	// Create offer
	offer, err := webrtc.CreateOffer(wb.peerConnection, medias)
	if err != nil {
		return fmt.Errorf("failed to create offer: %v", err)
	}

	// Remove extmap lines to reduce payload size (device limitation)
	re := regexp.MustCompile(`\r\na=extmap[^\r\n]*`)
	offer = re.ReplaceAllString(offer, "")

	core.Logger.Trace().Msgf("Sending WebRTC offer")

	// Send offer
	if err := wb.cameraClient.SendOffer(offer, wb.resolution, wb.streamType, wb.isHEVC); err != nil {
		return fmt.Errorf("failed to send offer: %v", err)
	}

	return nil
}

func (wb *WebRTCBridge) handleVideoTrack(track *pion.TrackRemote) {
	core.Logger.Trace().Msgf("Starting video track handler")
	
	for {
		select {
		case <-wb.ctx.Done():
			return
		default:
			packet, _, err := track.ReadRTP()
			if err != nil {
				if err == io.EOF {
					return
				}
				// Check if it's a known close error
				if strings.Contains(err.Error(), "closed") || 
				   strings.Contains(err.Error(), "EOF") ||
				   strings.Contains(err.Error(), "use of closed network connection") {
					return
				}
				core.Logger.Warn().Err(err).Msg("Unexpected error reading video RTP packet")
				time.Sleep(10 * time.Millisecond)
				continue
			}

			wb.rtpForwarder.ForwardVideoPacket(packet)
		}
	}
}

func (wb *WebRTCBridge) handleAudioTrack(track *pion.TrackRemote) {
	core.Logger.Trace().Msgf("Starting audio track handler")
	
	for {
		select {
		case <-wb.ctx.Done():
			return
		default:
			packet, _, err := track.ReadRTP()
			if err != nil {
				if err == io.EOF {
					return
				}
				if strings.Contains(err.Error(), "closed") || 
				   strings.Contains(err.Error(), "EOF") ||
				   strings.Contains(err.Error(), "use of closed network connection") {
					return
				}
				core.Logger.Warn().Err(err).Msg("Unexpected error reading audio RTP packet")
				time.Sleep(10 * time.Millisecond)
				continue
			}

			wb.rtpForwarder.ForwardAudioPacket(packet)
		}
	}
}

func (wb *WebRTCBridge) createHTTPClient() *http.Client {
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return nil
	}

	if wb.user.SessionData != nil && len(wb.user.SessionData.Cookies) > 0 {
		serverURL, _ := url.Parse(fmt.Sprintf("https://%s", wb.user.SessionData.ServerHost))

		var httpCookies []*http.Cookie
		for _, cookie := range wb.user.SessionData.Cookies {
			httpCookies = append(httpCookies, &http.Cookie{
				Name:     cookie.Name,
				Value:    cookie.Value,
				Domain:   cookie.Domain,
				Path:     cookie.Path,
				Expires:  cookie.Expires,
				Secure:   cookie.Secure,
				HttpOnly: cookie.HttpOnly,
			})
		}

		jar.SetCookies(serverURL, httpCookies)
	}

	return &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
	}
}

func (wb *WebRTCBridge) probe(msg pion.DataChannelMessage) (bool, error) {
	var message tuya.DataChannelMessage
	if err := json.Unmarshal([]byte(msg.Data), &message); err != nil {
		return false, err
	}

	switch message.Type {
	case "codec":
		frameRequest, _ := json.Marshal(tuya.DataChannelMessage{
			Type: "start",
			Msg:  "frame",
		})

		err := wb.sendMessageToDataChannel(frameRequest)
		if err != nil {
			return false, err
		}

	case "recv":
		var recvMessage tuya.RecvMessage
		if err := json.Unmarshal([]byte(message.Msg), &recvMessage); err != nil {
			return false, err
		}

		wb.rtpForwarder.videoSSRC = recvMessage.Video.SSRC
		wb.rtpForwarder.audioSSRC = recvMessage.Audio.SSRC

		completeMsg, _ := json.Marshal(tuya.DataChannelMessage{
			Type: "complete",
			Msg:  "",
		})

		err := wb.sendMessageToDataChannel(completeMsg)
		if err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

func (wb *WebRTCBridge) sendMessageToDataChannel(message []byte) error {
	if wb.dataChannel != nil {
		return wb.dataChannel.Send(message)
	}

	return nil
}

func (wb *WebRTCBridge) handleError(err error) {
	if wb.OnError != nil {
		wb.waiter.Done(err)
		wb.OnError(err)
	}
}
