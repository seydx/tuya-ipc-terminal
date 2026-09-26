package rtsp

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	videoSSRC atomic.Uint32
	audioSSRC atomic.Uint32

	// Packet count
	firstVideoPacket atomic.Bool
	firstAudioPacket atomic.Bool

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
	videoRTPPort          int // Client's video receiving port
	audioRTPPort          int // Client's audio receiving port
	backchannelClientPort int // Client's backchannel sending port

	// UDP backchannel listeners (server side)
	backchannelListener     *net.UDPConn // Server's RTP listener for backchannel
	backchannelRTCPListener *net.UDPConn // Server's RTCP listener for backchannel
	backchannelServerPort   int          // Server's RTP listening port
	backchannelRTCPPort     int          // Server's RTCP listening port

	// TCP interleaved transport
	tcpConn             net.Conn
	videoRTPChannel     byte
	audioRTPChannel     byte
	backAudioRTPChannel byte

	lastActivity atomic.Int64
}

func NewRTPForwarder() *RTPForwarder {
	forwarder := &RTPForwarder{clients: make(map[string]*RTPClient)}
	forwarder.audioSSRC.Store(1)
	forwarder.firstVideoPacket.Store(true)
	forwarder.firstAudioPacket.Store(true)
	return forwarder
}

func (rf *RTPForwarder) AddUDPClient(sessionID, clientHost string, videoRTPPort, audioRTPPort int) error {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	client, exists := rf.clients[sessionID]
	if !exists {
		client = &RTPClient{
			sessionID:     sessionID,
			transportMode: TransportUDP,
		}
	}

	if videoRTPPort > 0 && client.videoConn == nil {
		videoConn, videoAddr, err := dialUDPClient(clientHost, videoRTPPort)
		if err != nil {
			return fmt.Errorf("failed to create video UDP connection: %w", err)
		}
		client.videoAddr = videoAddr
		client.videoConn = videoConn
	}

	if audioRTPPort > 0 && client.audioConn == nil {
		audioConn, audioAddr, err := dialUDPClient(clientHost, audioRTPPort)
		if err != nil {
			if !exists && client.videoConn != nil {
				client.videoConn.Close()
			}
			return fmt.Errorf("failed to create audio UDP connection: %w", err)
		}

		client.audioAddr = audioAddr
		client.audioConn = audioConn
	}

	client.videoRTPPort = videoRTPPort
	client.audioRTPPort = audioRTPPort
	client.lastActivity.Store(time.Now().UnixNano())
	rf.clients[sessionID] = client

	core.Logger.Trace().Msgf("Added UDP RTP client %s at %s (video port:%d, audio port:%d)",
		sessionID, clientHost, videoRTPPort, audioRTPPort)
	return nil
}

func dialUDPClient(host string, port int) (*net.UDPConn, *net.UDPAddr, error) {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve UDP address %s: %w", address, err)
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to dial UDP address %s: %w", address, err)
	}
	return conn, udpAddr, nil
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

func (rf *RTPForwarder) AddTCPClient(sessionID string, conn net.Conn, videoRTPChannel, audioRTPChannel, backAudioRTPChannel byte) error {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	// Check if client already exists, update it
	if existingClient, exists := rf.clients[sessionID]; exists {
		core.Logger.Trace().Msgf("TCP client %s already exists, updating channels (video:%d->%d, audio:%d->%d, back:%d->%d)",
			sessionID, existingClient.videoRTPChannel, videoRTPChannel, existingClient.audioRTPChannel, audioRTPChannel, existingClient.backAudioRTPChannel, backAudioRTPChannel)
		existingClient.videoRTPChannel = videoRTPChannel
		existingClient.audioRTPChannel = audioRTPChannel
		existingClient.backAudioRTPChannel = backAudioRTPChannel
		existingClient.lastActivity.Store(time.Now().UnixNano())
		return nil
	}

	client := &RTPClient{
		sessionID:           sessionID,
		transportMode:       TransportTCP,
		tcpConn:             conn,
		videoRTPChannel:     videoRTPChannel,
		audioRTPChannel:     audioRTPChannel,
		backAudioRTPChannel: backAudioRTPChannel,
	}
	client.lastActivity.Store(time.Now().UnixNano())

	rf.clients[sessionID] = client

	core.Logger.Trace().Msgf("Added TCP RTP client %s (video channel:%d, audio channel:%d, back audio channel:%d)",
		sessionID, videoRTPChannel, audioRTPChannel, backAudioRTPChannel)
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

	// The RTSP SDP advertises video as payload type 96 for both H.264 and H.265.
	// Tuya's HEVC data channel uses PT 95, which RTSP clients discard as unknown.
	forwardedPacket := *packet
	forwardedPacket.PayloadType = 96

	data, err := forwardedPacket.Marshal()
	if err != nil {
		core.Logger.Error().Err(err).Msg("Error marshaling video RTP packet")
		return
	}

	// Forward to all clients
	for sessionID, client := range rf.clients {
		client.lastActivity.Store(time.Now().UnixNano())

		if client.transportMode == TransportUDP {
			if client.videoConn != nil {
				if _, err := client.videoConn.Write(data); err != nil {
					core.Logger.Error().Err(err).Msgf("Error forwarding video packet to UDP client %s", sessionID)
				} else if rf.firstVideoPacket.CompareAndSwap(true, false) {
					core.Logger.Trace().Msgf("Successfully sent first video RTP packet to UDP client %s on port %d (PT %d->96, SSRC %d, sequence %d, timestamp %d, %d bytes)",
						sessionID, client.videoRTPPort, packet.PayloadType, packet.SSRC, packet.SequenceNumber, packet.Timestamp, len(data))
				}
			}
		} else if client.transportMode == TransportTCP {
			if client.tcpConn != nil {
				if err := rf.sendInterleavedRTP(client.tcpConn, client.videoRTPChannel, data); err != nil {
					core.Logger.Error().Err(err).Msgf("Error forwarding video packet to TCP client %s", sessionID)
				} else if rf.firstVideoPacket.CompareAndSwap(true, false) {
					core.Logger.Trace().Msgf("Successfully sent first video RTP packet to TCP client %s on channel %d (PT %d->96, SSRC %d, sequence %d, timestamp %d, %d bytes)",
						sessionID, client.videoRTPChannel, packet.PayloadType, packet.SSRC, packet.SequenceNumber, packet.Timestamp, len(data))
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

	// Serialize packet
	data, err := packet.Marshal()
	if err != nil {
		core.Logger.Error().Err(err).Msg("Error marshaling audio RTP packet")
		return
	}

	// Forward to all clients
	for sessionID, client := range rf.clients {
		client.lastActivity.Store(time.Now().UnixNano())

		if client.transportMode == TransportUDP {
			if client.audioConn != nil {
				if _, err := client.audioConn.Write(data); err != nil {
					core.Logger.Error().Err(err).Msgf("Error forwarding audio packet to UDP client %s", sessionID)
				} else if rf.firstAudioPacket.CompareAndSwap(true, false) {
					core.Logger.Trace().Msgf("Successfully sent first audio packet to UDP client %s on port %d",
						sessionID, client.audioRTPPort)
				}
			}
		} else if client.transportMode == TransportTCP {
			if client.tcpConn != nil {
				if err := rf.sendInterleavedRTP(client.tcpConn, client.audioRTPChannel, data); err != nil {
					core.Logger.Error().Err(err).Msgf("Error forwarding audio packet to TCP client %s", sessionID)
				} else if rf.firstAudioPacket.CompareAndSwap(true, false) {
					core.Logger.Trace().Msgf("Successfully sent first audio packet to TCP client %s on channel %d",
						sessionID, client.audioRTPChannel)
				}
			}
		}
	}
}

func (rf *RTPForwarder) Stop() {
	// Reset SSRCs
	rf.videoSSRC.Store(0)
	rf.audioSSRC.Store(1)

	// Reset first packet flags
	rf.firstVideoPacket.Store(true)
	rf.firstAudioPacket.Store(true)

	// Clear all clients
	for sessionID := range rf.clients {
		rf.RemoveClient(sessionID)
	}

	core.Logger.Trace().Msg("RTPForwarder stopped and all clients cleared")
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
		if now.Sub(time.Unix(0, client.lastActivity.Load())) > timeout {
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
