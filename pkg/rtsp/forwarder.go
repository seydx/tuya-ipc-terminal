package rtsp

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/rtp"
)

// TransportMode defines the RTP transport mode
type TransportMode int

const (
	TransportUDP TransportMode = iota
	TransportTCP               // Interleaved RTP over RTSP connection
)

// RTPForwarder handles RTP packet forwarding to RTSP clients
type RTPForwarder struct {
	clients map[string]*RTPClient
	mutex   sync.RWMutex

	// RTP session info
	videoSSRC uint32
	audioSSRC uint32
	videoSeq  uint16
	audioSeq  uint16

	OnBackchannelAudio func(*rtp.Packet)

	// Debug counters
	videoPacketCount uint64
	audioPacketCount uint64
}

// RTPClient represents an RTP client connection
type RTPClient struct {
	sessionID     string
	transportMode TransportMode

	// UDP transport
	videoConn *net.UDPConn
	audioConn *net.UDPConn
	videoAddr *net.UDPAddr
	audioAddr *net.UDPAddr

	// TCP interleaved transport
	tcpConn      net.Conn
	videoChannel byte
	audioChannel byte

	lastActivity time.Time
}

// NewRTPForwarder creates a new RTP forwarder
func NewRTPForwarder() *RTPForwarder {
	return &RTPForwarder{
		clients:   make(map[string]*RTPClient),
		videoSSRC: 0, // Fixed SSRC for video
		audioSSRC: 1, // Fixed SSRC for audio
		videoSeq:  1,
		audioSeq:  1,
	}
}

// AddUDPClient adds a UDP RTP client
func (rf *RTPForwarder) AddUDPClient(sessionID string, videoPort, audioPort int) error {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	// Check if client already exists
	if _, exists := rf.clients[sessionID]; exists {
		fmt.Printf("UDP client %s already exists, updating ports\n", sessionID)
		rf.clients[sessionID].lastActivity = time.Now()
		return nil
	}

	// Create UDP addresses
	videoAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("localhost:%d", videoPort))
	if err != nil {
		return fmt.Errorf("failed to resolve video UDP address: %v", err)
	}

	audioAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("localhost:%d", audioPort))
	if err != nil {
		return fmt.Errorf("failed to resolve audio UDP address: %v", err)
	}

	// Create UDP connections
	videoConn, err := net.DialUDP("udp", nil, videoAddr)
	if err != nil {
		return fmt.Errorf("failed to create video UDP connection: %v", err)
	}

	audioConn, err := net.DialUDP("udp", nil, audioAddr)
	if err != nil {
		videoConn.Close()
		return fmt.Errorf("failed to create audio UDP connection: %v", err)
	}

	client := &RTPClient{
		sessionID:     sessionID,
		transportMode: TransportUDP,
		videoConn:     videoConn,
		audioConn:     audioConn,
		videoAddr:     videoAddr,
		audioAddr:     audioAddr,
		lastActivity:  time.Now(),
	}

	rf.clients[sessionID] = client

	fmt.Printf("Added UDP RTP client %s (video:%d, audio:%d)\n", sessionID, videoPort, audioPort)
	return nil
}

// AddTCPClient adds a TCP interleaved RTP client
func (rf *RTPForwarder) AddTCPClient(sessionID string, conn net.Conn, videoChannel, audioChannel byte) error {
	rf.mutex.Lock()
	defer rf.mutex.Unlock()

	// Check if client already exists, update it
	if existingClient, exists := rf.clients[sessionID]; exists {
		fmt.Printf("TCP client %s already exists, updating channels (video:%d->%d, audio:%d->%d)\n",
			sessionID, existingClient.videoChannel, videoChannel, existingClient.audioChannel, audioChannel)
		existingClient.videoChannel = videoChannel
		existingClient.audioChannel = audioChannel
		existingClient.lastActivity = time.Now()
		return nil
	}

	client := &RTPClient{
		sessionID:     sessionID,
		transportMode: TransportTCP,
		tcpConn:       conn,
		videoChannel:  videoChannel,
		audioChannel:  audioChannel,
		lastActivity:  time.Now(),
	}

	rf.clients[sessionID] = client

	fmt.Printf("Added TCP RTP client %s (video channel:%d, audio channel:%d)\n",
		sessionID, videoChannel, audioChannel)
	return nil
}

// RemoveClient removes an RTP client
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
		}
		// TCP connection is managed by RTSP handler, don't close here

		delete(rf.clients, sessionID)
		fmt.Printf("Removed RTP client %s\n", sessionID)
	}
}

// ForwardVideoPacket forwards a video RTP packet to all clients
func (rf *RTPForwarder) ForwardVideoPacket(packet *rtp.Packet) {
	rf.mutex.RLock()
	defer rf.mutex.RUnlock()

	rf.videoPacketCount++

	if len(rf.clients) == 0 {
		if rf.videoPacketCount%100 == 0 { // Log every 100th packet
			fmt.Printf("No RTP clients available for video packet #%d\n", rf.videoPacketCount)
		}
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
		fmt.Printf("Error marshaling video RTP packet: %v\n", err)
		return
	}

	// Forward to all clients
	for sessionID, client := range rf.clients {
		client.lastActivity = time.Now()

		if client.transportMode == TransportUDP {
			if client.videoConn != nil {
				if _, err := client.videoConn.Write(data); err != nil {
					fmt.Printf("Error forwarding video packet to UDP client %s: %v\n", sessionID, err)
				}
			}
		} else if client.transportMode == TransportTCP {
			if client.tcpConn != nil {
				if err := rf.sendInterleavedRTP(client.tcpConn, client.videoChannel, data); err != nil {
					fmt.Printf("Error forwarding video packet to TCP client %s: %v\n", sessionID, err)
				}
			}
		}
	}
}

// ForwardAudioPacket forwards an audio RTP packet to all clients
func (rf *RTPForwarder) ForwardAudioPacket(packet *rtp.Packet) {
	rf.mutex.RLock()
	defer rf.mutex.RUnlock()

	rf.audioPacketCount++

	if len(rf.clients) == 0 {
		if rf.audioPacketCount%100 == 0 { // Log every 100th packet
			fmt.Printf("No RTP clients available for audio packet #%d\n", rf.audioPacketCount)
		}
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
		fmt.Printf("Error marshaling audio RTP packet: %v\n", err)
		return
	}

	// Forward to all clients
	for sessionID, client := range rf.clients {
		client.lastActivity = time.Now()

		if client.transportMode == TransportUDP {
			if client.audioConn != nil {
				if _, err := client.audioConn.Write(data); err != nil {
					fmt.Printf("Error forwarding audio packet to UDP client %s: %v\n", sessionID, err)
				}
			}
		} else if client.transportMode == TransportTCP {
			if client.tcpConn != nil {
				if err := rf.sendInterleavedRTP(client.tcpConn, client.audioChannel, data); err != nil {
					fmt.Printf("Error forwarding audio packet to TCP client %s: %v\n", sessionID, err)
				} else if rf.audioPacketCount <= 5 {
					fmt.Printf("Successfully sent audio packet #%d to TCP client %s on channel %d\n",
						rf.audioPacketCount, sessionID, client.audioChannel)
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
		} else if client.tcpConn != nil {
			client.tcpConn.Close()
		}
	}

	rf.clients = make(map[string]*RTPClient)
	fmt.Println("Stopped RTP forwarder and closed all connections")
}

// sendInterleavedRTP sends RTP packet over TCP interleaved
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

// GetClientCount returns the number of active clients
func (rf *RTPForwarder) GetClientCount() int {
	rf.mutex.RLock()
	defer rf.mutex.RUnlock()
	return len(rf.clients)
}

// GetStats returns forwarding statistics
func (rf *RTPForwarder) GetStats() (uint64, uint64, int) {
	rf.mutex.RLock()
	defer rf.mutex.RUnlock()
	return rf.videoPacketCount, rf.audioPacketCount, len(rf.clients)
}

// CleanupInactiveClients removes clients that haven't been active
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
			}
			delete(rf.clients, sessionID)
			fmt.Printf("Cleaned up inactive RTP client %s\n", sessionID)
		}
	}
}
