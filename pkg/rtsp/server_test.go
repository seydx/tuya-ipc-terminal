package rtsp

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"tuya-ipc-terminal/pkg/storage"

	"github.com/pion/rtp"
)

func TestGenerateSDPUsesRequestedStreamCodec(t *testing.T) {
	camera := &storage.CameraInfo{
		Skill: `{"videos":[{"streamType":2,"codecType":4,"width":2560,"height":1440},{"streamType":4,"codecType":2,"width":640,"height":360}]}`,
	}
	server := &RTSPServer{}

	tests := []struct {
		resolution string
		wantCodec  string
		wantOther  string
	}{
		{resolution: "hd", wantCodec: "H265/90000", wantOther: "H264/90000"},
		{resolution: "sd", wantCodec: "H264/90000", wantOther: "H265/90000"},
	}

	for _, test := range tests {
		t.Run(test.resolution, func(t *testing.T) {
			sdp := server.generateSDP(camera, "rtsp://localhost/Keller/"+test.resolution, test.resolution)
			if !strings.Contains(sdp, test.wantCodec) {
				t.Errorf("SDP does not advertise requested %s codec %q:\n%s", test.resolution, test.wantCodec, sdp)
			}
			if strings.Contains(sdp, test.wantOther) {
				t.Errorf("SDP unexpectedly advertises other codec %q:\n%s", test.wantOther, sdp)
			}
		})
	}
}

func TestFindCameraByRTSPPathIgnoresCaseWhenNeeded(t *testing.T) {
	cameras := []storage.CameraInfo{{RTSPPath: "/Keller"}}

	if got := findCameraByRTSPPath(cameras, "/keller"); got != &cameras[0] {
		t.Fatalf("case-insensitive path lookup returned %p, want %p", got, &cameras[0])
	}
}

func TestFindCameraByRTSPPathPrefersExactMatch(t *testing.T) {
	cameras := []storage.CameraInfo{{RTSPPath: "/KELLER"}, {RTSPPath: "/Keller"}}

	if got := findCameraByRTSPPath(cameras, "/Keller"); got != &cameras[1] {
		t.Fatalf("path lookup returned %p, want exact match %p", got, &cameras[1])
	}
}

func TestAddClientReturnsWhileWebRTCStartupWaits(t *testing.T) {
	stream := &CameraStream{
		camera:     &storage.CameraInfo{DeviceName: "Keller"},
		clients:    make(map[string]*RTSPClient),
		connecting: true,
		starting:   true,
	}
	startupEntered := make(chan struct{})
	finishStartup := make(chan struct{})
	startupFinished := make(chan struct{})
	go func() {
		stream.startStreamWith(func() error {
			close(startupEntered)
			<-finishStartup
			return nil
		})
		close(startupFinished)
	}()
	<-startupEntered

	clientAdded := make(chan struct{})
	go func() {
		stream.AddClient(&RTSPClient{session: "ha-client"})
		close(clientAdded)
	}()

	select {
	case <-clientAdded:
	case <-time.After(time.Second):
		t.Fatal("adding an RTSP client blocked while WebRTC startup was waiting")
	}

	close(finishStartup)
	select {
	case <-startupFinished:
	case <-time.After(time.Second):
		t.Fatal("WebRTC startup did not finish after being released")
	}
}

func TestRemoveClientDoesNotHoldServerLockDuringStreamCleanup(t *testing.T) {
	stream := &CameraStream{clients: make(map[string]*RTSPClient)}
	stream.mutex.Lock()
	defer stream.mutex.Unlock()

	clientConn, peerConn := net.Pipe()
	defer peerConn.Close()

	const sessionID = "blocked-stream-client"
	server := &RTSPServer{
		clients: map[string]*RTSPClient{
			sessionID: {conn: clientConn, stream: stream},
		},
	}
	removed := make(chan struct{})
	go func() {
		server.removeClient(sessionID)
		close(removed)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if server.mutex.TryLock() {
			_, exists := server.clients[sessionID]
			server.mutex.Unlock()
			if !exists {
				server.addClient(&RTSPClient{session: "unrelated-client"})
				return
			}
		}
		time.Sleep(time.Millisecond)
	}

	t.Fatal("client cleanup kept the server lock while waiting for the stream lock")
}

func TestCleanupInactiveStreamsSynchronizesWithClientActivity(t *testing.T) {
	stream := &CameraStream{
		camera:       &storage.CameraInfo{DeviceName: "Keller"},
		clients:      make(map[string]*RTSPClient),
		starting:     true,
		lastActivity: time.Now(),
	}
	server := &RTSPServer{
		streams: map[string]*CameraStream{"Keller-hd": stream},
	}

	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 1000; i++ {
			stream.AddClient(&RTSPClient{session: "client"})
			stream.RemoveClient("client")
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 1000; i++ {
			server.cleanupInactiveStreams()
		}
	}()
	workers.Wait()
}

func TestUDPForwarderSendsPacketsToRTSPPeerAddress(t *testing.T) {
	peerAddr, err := net.ResolveUDPAddr("udp", "127.0.0.2:0")
	if err != nil {
		t.Fatal(err)
	}
	peer, err := net.ListenUDP("udp", peerAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	peerUDPAddr := peer.LocalAddr().(*net.UDPAddr)
	forwarder := NewRTPForwarder()
	if err := forwarder.AddUDPClient("client", peerUDPAddr.IP.String(), peerUDPAddr.Port, 0); err != nil {
		t.Fatal(err)
	}
	defer forwarder.RemoveClient("client")

	forwarder.ForwardVideoPacket(&rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 95},
		Payload: []byte{1, 2, 3},
	})

	buffer := make([]byte, 1500)
	n, _, err := peer.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("did not receive forwarded RTP packet at RTSP peer address: %v", err)
	}
	var forwarded rtp.Packet
	if err := forwarded.Unmarshal(buffer[:n]); err != nil {
		t.Fatalf("forwarded UDP data is not an RTP packet: %v", err)
	}
	if forwarded.PayloadType != 96 {
		t.Fatalf("forwarded UDP payload type = %d, want SDP payload type 96", forwarded.PayloadType)
	}
}

func TestTCPForwarderUsesSDPVideoPayloadType(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	forwarder := NewRTPForwarder()
	if err := forwarder.AddTCPClient("client", serverConn, 0, 2, 4); err != nil {
		t.Fatal(err)
	}
	defer forwarder.RemoveClient("client")

	go forwarder.ForwardVideoPacket(&rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 95, SequenceNumber: 12, Timestamp: 34, SSRC: 56},
		Payload: []byte{1, 2, 3},
	})

	header := make([]byte, 4)
	if _, err := io.ReadFull(clientConn, header); err != nil {
		t.Fatalf("did not receive interleaved RTP header: %v", err)
	}
	if header[0] != '$' || header[1] != 0 {
		t.Fatalf("interleaved header = %v, want '$' on video channel 0", header)
	}
	packetData := make([]byte, binary.BigEndian.Uint16(header[2:]))
	if _, err := io.ReadFull(clientConn, packetData); err != nil {
		t.Fatalf("did not receive interleaved RTP packet: %v", err)
	}
	var forwarded rtp.Packet
	if err := forwarded.Unmarshal(packetData); err != nil {
		t.Fatalf("forwarded TCP data is not an RTP packet: %v", err)
	}
	if forwarded.PayloadType != 96 {
		t.Fatalf("forwarded TCP payload type = %d, want SDP payload type 96", forwarded.PayloadType)
	}
}

func TestConcurrentAudioAndVideoForwardingIsRaceSafe(t *testing.T) {
	forwarder := NewRTPForwarder()
	forwarder.clients["client"] = &RTPClient{
		sessionID:     "client",
		transportMode: TransportUDP,
	}
	videoPacket := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 95}, Payload: []byte{1}}
	audioPacket := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 0}, Payload: []byte{1}}

	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 1000; i++ {
			forwarder.ForwardVideoPacket(videoPacket)
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 1000; i++ {
			forwarder.ForwardAudioPacket(audioPacket)
		}
	}()
	workers.Wait()

	if forwarder.clients["client"].lastActivity.Load() == 0 {
		t.Fatal("concurrent forwarding did not update client activity")
	}
}
