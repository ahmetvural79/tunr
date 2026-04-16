package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ahmetvural79/tunr/internal/logger"
)

// UDPProxy forwards UDP datagrams between the relay and a local UDP port.
// Datagrams are forwarded as base64-encoded payloads over the WebSocket control channel.
type UDPProxy struct {
	LocalPort int

	mu          sync.RWMutex
	packetCount int64
	bytesIn     int64
	bytesOut    int64
}

// NewUDPProxy creates a UDP proxy targeting the given local port.
func NewUDPProxy(localPort int) *UDPProxy {
	return &UDPProxy{LocalPort: localPort}
}

// ForwardToLocal sends a datagram to the local UDP service and returns the response.
func (p *UDPProxy) ForwardToLocal(ctx context.Context, data []byte) ([]byte, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", p.LocalPort)

	conn, err := net.DialTimeout("udp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("UDP dial failed: %w", err)
	}
	defer conn.Close()

	// Set deadline for the entire round-trip
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	n, err := conn.Write(data)
	if err != nil {
		return nil, fmt.Errorf("UDP write failed: %w", err)
	}

	p.mu.Lock()
	p.packetCount++
	p.bytesOut += int64(n)
	p.mu.Unlock()

	// Read response
	buf := make([]byte, 65535) // max UDP datagram
	rn, err := conn.Read(buf)
	if err != nil {
		// Some UDP services don't respond (fire-and-forget)
		logger.Debug("UDP read: %v (may be expected)", err)
		return nil, nil
	}

	p.mu.Lock()
	p.bytesIn += int64(rn)
	p.mu.Unlock()

	return buf[:rn], nil
}

// Stats returns packet and byte counters.
func (p *UDPProxy) Stats() (packets int64, bytesIn int64, bytesOut int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.packetCount, p.bytesIn, p.bytesOut
}
