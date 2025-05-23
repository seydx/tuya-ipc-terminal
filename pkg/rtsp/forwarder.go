package rtsp

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"tuya-ipc-terminal/pkg/core"
	"tuya-ipc-terminal/pkg/utils"

	"github.com/pion/rtp"
)

// RTP transport mode (UDP or TCP)
type TransportMode int

const (
	TransportUDP TransportMode = iota
	TransportTCP               // Interleaved
)

type RTPForwarder struct {
	clients map[string]*RTPClient
	mutex   sync.RWMutex

	// RTP session info
	videoSSRC uint32
	audioSSRC uint32
	videoSeq  uint16
	audioSeq  uint16

	// Packet count
	firstVideoPacket bool
	firstAudioPacket bool

	OnBackchannelAudio func(*rtp.Packet)
}

type RTPClient struct {
	sessionID     string
	transportMode TransportMode

	// UDP transport - Outgoing connections (server -> client)
	videoConn *net.UDPConn // For sending video to client
	audioConn *net.UDPConn // For sending audio to client

	// UDP transport - Client addresses
	videoAddr *net.UDPAddr
	audioAddr *net.UDPAddr

	// UDP transport - Client ports
	videoPort             int // Client's video receiving port
	audioPort             int // Client's audio receiving port
	backchannelClientPort int // Client's backchannel sending port

	// UDP backchannel listeners (server side)
	backchannelListener     *net.UDPConn // Server's RTP listener for backchannel
	backchannelRTCPListener *net.UDPConn // Server's RTCP listener for backchannel
	backchannelServerPort   int          // Server's RTP listening port
	backchannelRTCPPort     int          // Server's RTCP listening port

	// TCP interleaved transport
	tcpConn          net.Conn
	videoChannel     byte
	audioChannel     byte
	backAudioChannel byte

	lastActivity time.Time
}

func NewRTPForwarder() *RTPForwarder {
	return &RTPForwarder{
		clients:          make(map[string]*RTPClient),
		videoSSRC:        0, // Fixed SSRC for video
		audioSSRC:        1, // Fixed SSRC for audio
		videoSeq:         1,
		audioSeq:         1,
		firstVideoPacket: true,
		firstAudioPacket: true,
	}
}

func (rf *RTPForwarder) AddUDPClient(sessionID string, videoPort, audioPort int) error {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	// Check if client already exists
	if client, exists := rf.clients[sessionID]; exists {
		// Update existing client with new ports
		client.videoPort = videoPort
		client.audioPort = audioPort
		client.lastActivity = time.Now()

		// Create new connections if needed
		if videoPort > 0 && client.videoConn == nil {
			videoAddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("localhost:%d", videoPort))
			videoConn, _ := net.DialUDP("udp", nil, videoAddr)
			client.videoAddr = videoAddr
			client.videoConn = videoConn
		}

		if audioPort > 0 && client.audioConn == nil {
			audioAddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("localhost:%d", audioPort))
			audioConn, _ := net.DialUDP("udp", nil, audioAddr)
			client.audioAddr = audioAddr
			client.audioConn = audioConn
		}

		return nil
	}

	client := &RTPClient{
		sessionID:     sessionID,
		transportMode: TransportUDP,
		videoPort:     videoPort,
		audioPort:     audioPort,
		lastActivity:  time.Now(),
	}

	// Create video connection if port provided
	if videoPort > 0 {
		videoAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("localhost:%d", videoPort))
		if err != nil {
			return fmt.Errorf("failed to resolve video UDP address: %v", err)
		}

		videoConn, err := net.DialUDP("udp", nil, videoAddr)
		if err != nil {
			return fmt.Errorf("failed to create video UDP connection: %v", err)
		}

		client.videoAddr = videoAddr
		client.videoConn = videoConn
	}

	// Create audio connection if port provided
	if audioPort > 0 {
		audioAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("localhost:%d", audioPort))
		if err != nil {
			if client.videoConn != nil {
				client.videoConn.Close()
			}
			return fmt.Errorf("failed to resolve audio UDP address: %v", err)
		}

		audioConn, err := net.DialUDP("udp", nil, audioAddr)
		if err != nil {
			if client.videoConn != nil {
				client.videoConn.Close()
			}
			return fmt.Errorf("failed to create audio UDP connection: %v", err)
		}

		client.audioAddr = audioAddr
		client.audioConn = audioConn
	}

	rf.clients[sessionID] = client

	core.Logger.Trace().Msgf("Added UDP RTP client %s (video port:%d, audio port:%d)",
		sessionID, videoPort, audioPort)
	return nil
}

func (rf *RTPForwarder) SetupUDPBackchannel(sessionID string, clientPort int) (int, error) {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	client, exists := rf.clients[sessionID]
	if !exists {
		return 0, fmt.Errorf("client %s not found", sessionID)
	}

	if client.transportMode != TransportUDP {
		return 0, fmt.Errorf("client %s is not using UDP transport", sessionID)
	}

	// Store client's backchannel port
	client.backchannelClientPort = clientPort

	// If listeners already exist, return existing port
	if client.backchannelListener != nil {
		return client.backchannelServerPort, nil
	}

	// Allocate consecutive ports for RTP/RTCP
	portPair, err := utils.DefaultPortAllocator.GetConsecutiveUDPPorts(nil, 10)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate UDP ports for backchannel: %v", err)
	}

	// Store listeners and ports
	client.backchannelListener = portPair.RTPListener
	client.backchannelRTCPListener = portPair.RTCPListener
	client.backchannelServerPort = portPair.RTPPort
	client.backchannelRTCPPort = portPair.RTCPPort

	// Start goroutines to handle incoming packets
	go rf.handleUDPBackchannelRTP(sessionID, client.backchannelListener)
	go rf.handleUDPBackchannelRTCP(client.backchannelRTCPListener)

	core.Logger.Trace().Msgf("Setup UDP backchannel for client %s (client ports:%d-%d, server ports:%d-%d)",
		sessionID, clientPort, clientPort+1, portPair.RTPPort, portPair.RTCPPort)

	return portPair.RTPPort, nil
}

func (rf *RTPForwarder) handleUDPBackchannelRTP(sessionID string, listener *net.UDPConn) {
	defer listener.Close()

	buffer := make([]byte, 1500)

	for {
		n, _, err := listener.ReadFromUDP(buffer)
		if err != nil {
			if !strings.Contains(err.Error(), "closed") {
				core.Logger.Error().Err(err).Msgf("Error reading UDP RTP backchannel for client %s", sessionID)
			}
			break
		}

		// Parse RTP packet
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buffer[:n]); err != nil {
			continue
		}

		// Forward to WebRTC bridge
		if rf.OnBackchannelAudio != nil {
			rf.OnBackchannelAudio(packet)
		}
	}
}

func (rf *RTPForwarder) handleUDPBackchannelRTCP(listener *net.UDPConn) {
	defer listener.Close()

	buffer := make([]byte, 1500)

	for {
		_, _, err := listener.ReadFromUDP(buffer)
		if err != nil {
			// Ignore
			break
		}

		// Simply discard RTCP packets
	}
}

func (rf *RTPForwarder) AddTCPClient(sessionID string, conn net.Conn, videoChannel, audioChannel, backAudioChannel byte) error {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	// Check if client already exists, update it
	if existingClient, exists := rf.clients[sessionID]; exists {
		core.Logger.Trace().Msgf("TCP client %s already exists, updating channels (video:%d->%d, audio:%d->%d, back:%d->%d)",
			sessionID, existingClient.videoChannel, videoChannel, existingClient.audioChannel, audioChannel, existingClient.backAudioChannel, backAudioChannel)
		existingClient.videoChannel = videoChannel
		existingClient.audioChannel = audioChannel
		existingClient.backAudioChannel = backAudioChannel
		existingClient.lastActivity = time.Now()
		return nil
	}

	client := &RTPClient{
		sessionID:        sessionID,
		transportMode:    TransportTCP,
		tcpConn:          conn,
		videoChannel:     videoChannel,
		audioChannel:     audioChannel,
		backAudioChannel: backAudioChannel,
		lastActivity:     time.Now(),
	}

	rf.clients[sessionID] = client

	core.Logger.Trace().Msgf("Added TCP RTP client %s (video channel:%d, audio channel:%d, back audio channel:%d)",
		sessionID, videoChannel, audioChannel, backAudioChannel)
	return nil
}

func (rf *RTPForwarder) RemoveClient(sessionID string) {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	if client, exists := rf.clients[sessionID]; exists {
		if client.transportMode == TransportUDP {
			if client.videoConn != nil {
				client.videoConn.Close()
			}
			if client.audioConn != nil {
				client.audioConn.Close()
			}
			if client.backchannelListener != nil {
				client.backchannelListener.Close()
			}
			if client.backchannelRTCPListener != nil {
				client.backchannelRTCPListener.Close()
			}
		}

		delete(rf.clients, sessionID)
		core.Logger.Trace().Msgf("Removed RTP client %s", sessionID)
	}
}

func (rf *RTPForwarder) ForwardVideoPacket(packet *rtp.Packet) {
	rf.mutex.RLock()
	defer rf.mutex.RUnlock()

	if len(rf.clients) == 0 {
		return
	}

	// Create a copy of the packet to avoid modifying the original
	packetCopy := *packet
	packetCopy.Header.SSRC = rf.videoSSRC
	packetCopy.Header.SequenceNumber = rf.videoSeq
	rf.videoSeq++

	// Serialize packet
	data, err := packetCopy.Marshal()
	if err != nil {
		core.Logger.Error().Err(err).Msg("Error marshaling video RTP packet")
		return
	}

	// Forward to all clients
	for sessionID, client := range rf.clients {
		client.lastActivity = time.Now()

		if client.transportMode == TransportUDP {
			if client.videoConn != nil {
				if _, err := client.videoConn.Write(data); err != nil {
					core.Logger.Error().Err(err).Msgf("Error forwarding video packet to UDP client %s", sessionID)
				} else if rf.firstVideoPacket {
					rf.firstVideoPacket = false
					core.Logger.Trace().Msgf("Successfully sent first video packet to UDP client %s on port %d",
						sessionID, client.videoPort)
				}
			}
		} else if client.transportMode == TransportTCP {
			if client.tcpConn != nil {
				if err := rf.sendInterleavedRTP(client.tcpConn, client.videoChannel, data); err != nil {
					core.Logger.Error().Err(err).Msgf("Error forwarding video packet to TCP client %s", sessionID)
				} else if rf.firstVideoPacket {
					rf.firstVideoPacket = false
					core.Logger.Trace().Msgf("Successfully sent first video packet to TCP client %s on channel %d",
						sessionID, client.videoChannel)
				}
			}
		}
	}
}

func (rf *RTPForwarder) ForwardAudioPacket(packet *rtp.Packet) {
	rf.mutex.RLock()
	defer rf.mutex.RUnlock()

	if len(rf.clients) == 0 {
		return
	}

	// Create a copy of the packet to avoid modifying the original
	packetCopy := *packet
	packetCopy.Header.SSRC = rf.audioSSRC
	packetCopy.Header.SequenceNumber = rf.audioSeq
	rf.audioSeq++

	// Serialize packet
	data, err := packetCopy.Marshal()
	if err != nil {
		core.Logger.Error().Err(err).Msg("Error marshaling audio RTP packet")
		return
	}

	// Forward to all clients
	for sessionID, client := range rf.clients {
		client.lastActivity = time.Now()

		if client.transportMode == TransportUDP {
			if client.audioConn != nil {
				if _, err := client.audioConn.Write(data); err != nil {
					core.Logger.Error().Err(err).Msgf("Error forwarding audio packet to UDP client %s", sessionID)
				} else if rf.firstAudioPacket {
					rf.firstAudioPacket = false
					core.Logger.Trace().Msgf("Successfully sent first audio packet to UDP client %s on port %d",
						sessionID, client.audioPort)
				}
			}
		} else if client.transportMode == TransportTCP {
			if client.tcpConn != nil {
				if err := rf.sendInterleavedRTP(client.tcpConn, client.audioChannel, data); err != nil {
					core.Logger.Error().Err(err).Msgf("Error forwarding audio packet to TCP client %s", sessionID)
				} else if rf.firstAudioPacket {
					rf.firstAudioPacket = false
					core.Logger.Trace().Msgf("Successfully sent first audio packet to TCP client %s on channel %d",
						sessionID, client.audioChannel)
				}
			}
		}
	}
}

func (rf *RTPForwarder) Stop() {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	for _, client := range rf.clients {
		if client.transportMode == TransportUDP {
			if client.videoConn != nil {
				client.videoConn.Close()
			}
			if client.audioConn != nil {
				client.audioConn.Close()
			}
			if client.backchannelListener != nil {
				client.backchannelListener.Close()
			}
			if client.backchannelRTCPListener != nil {
				client.backchannelRTCPListener.Close()
			}
		} else if client.tcpConn != nil {
			// Don't close TCP connection here, it's managed by RTSP handler
		}
	}

	rf.clients = make(map[string]*RTPClient)
	core.Logger.Trace().Msg("Stopped RTP forwarder and closed all connections")
}

func (rf *RTPForwarder) GetClientCount() int {
	rf.mutex.RLock()
	defer rf.mutex.RUnlock()
	return len(rf.clients)
}

func (rf *RTPForwarder) CleanupInactiveClients(timeout time.Duration) {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	now := time.Now()
	var toRemove []string

	for sessionID, client := range rf.clients {
		if now.Sub(client.lastActivity) > timeout {
			toRemove = append(toRemove, sessionID)
		}
	}

	for _, sessionID := range toRemove {
		if client, exists := rf.clients[sessionID]; exists {
			if client.transportMode == TransportUDP {
				if client.videoConn != nil {
					client.videoConn.Close()
				}
				if client.audioConn != nil {
					client.audioConn.Close()
				}
				if client.backchannelListener != nil {
					client.backchannelListener.Close()
				}
				if client.backchannelRTCPListener != nil {
					client.backchannelRTCPListener.Close()
				}
			}
			delete(rf.clients, sessionID)
			core.Logger.Trace().Msgf("Cleaned up inactive RTP client %s", sessionID)
		}
	}
}

func (rf *RTPForwarder) sendInterleavedRTP(conn net.Conn, channel byte, rtpData []byte) error {
	// Interleaved format: $ + channel + length(2 bytes) + RTP data
	header := make([]byte, 4)
	header[0] = '$'                     // Magic byte
	header[1] = channel                 // Channel number
	header[2] = byte(len(rtpData) >> 8) // Length high byte
	header[3] = byte(len(rtpData))      // Length low byte

	// Send header + data in one write to avoid fragmentation
	fullPacket := append(header, rtpData...)

	if _, err := conn.Write(fullPacket); err != nil {
		return err
	}

	return nil
}
