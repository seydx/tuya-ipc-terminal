package utils

import (
	"fmt"
	"net"
	"sync"
)

type PortAllocator struct {
	mu sync.Mutex
}

func NewPortAllocator() *PortAllocator {
	return &PortAllocator{}
}

type UDPPortPair struct {
	RTPListener  *net.UDPConn
	RTCPListener *net.UDPConn
	RTPPort      int
	RTCPPort     int
}

func (p *UDPPortPair) Close() {
	if p.RTPListener != nil {
		p.RTPListener.Close()
	}
	if p.RTCPListener != nil {
		p.RTCPListener.Close()
	}
}

func (pa *PortAllocator) GetConsecutiveUDPPorts(ip net.IP, maxAttempts int) (*UDPPortPair, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	if ip == nil {
		ip = net.IPv4(0, 0, 0, 0)
	}

	for i := 0; i < maxAttempts; i++ {
		// Get a random even port from the OS
		tempListener, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: 0})
		if err != nil {
			continue
		}

		addr := tempListener.LocalAddr().(*net.UDPAddr)
		basePort := addr.Port
		tempListener.Close()

		// Make it even if it's odd
		if basePort%2 == 1 {
			basePort--
		}

		// Try to bind both ports
		rtpListener, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: basePort})
		if err != nil {
			continue
		}

		rtcpListener, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: basePort + 1})
		if err != nil {
			rtpListener.Close()
			continue
		}

		return &UDPPortPair{
			RTPListener:  rtpListener,
			RTCPListener: rtcpListener,
			RTPPort:      basePort,
			RTCPPort:     basePort + 1,
		}, nil
	}

	return nil, fmt.Errorf("failed to allocate consecutive UDP ports after %d attempts", maxAttempts)
}

func (pa *PortAllocator) GetSingleUDPPort(ip net.IP) (*net.UDPConn, int, error) {
	if ip == nil {
		ip = net.IPv4(0, 0, 0, 0)
	}

	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: 0})
	if err != nil {
		return nil, 0, err
	}

	addr := listener.LocalAddr().(*net.UDPAddr)
	return listener, addr.Port, nil
}

func (pa *PortAllocator) GetUDPPortInRange(ip net.IP, minPort, maxPort int) (*net.UDPConn, int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	if ip == nil {
		ip = net.IPv4(0, 0, 0, 0)
	}

	for port := minPort; port <= maxPort; port++ {
		listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: port})
		if err == nil {
			return listener, port, nil
		}
	}

	return nil, 0, fmt.Errorf("no available UDP port in range %d-%d", minPort, maxPort)
}

var DefaultPortAllocator = NewPortAllocator()
