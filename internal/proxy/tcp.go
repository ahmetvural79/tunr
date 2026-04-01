package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// TCPProxy forwards raw TCP connections between clients and a local port.
type TCPProxy struct {
	LocalPort int
	Listener  net.Listener

	// Bytes forwarded (atomic via mutex for simplicity)
	mu          sync.RWMutex
	bytesIn     int64
	bytesOut    int64
	connCount   int64
	activeConns sync.WaitGroup
}

// NewTCPProxy creates a new TCP proxy.
func NewTCPProxy(localPort int) *TCPProxy {
	return &TCPProxy{LocalPort: localPort}
}

// Listen starts listening for incoming connections from the relay.
func (p *TCPProxy) AcceptLoop(ctx context.Context, accept func(conn net.Conn)) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	p.Listener = ln

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				continue
			}
		}

		p.mu.Lock()
		p.connCount++
		p.mu.Unlock()
		p.activeConns.Add(1)

		go func() {
			defer p.activeConns.Done()
			accept(conn)
		}()
	}
}

// Close shuts down the TCP proxy and all active connections.
func (p *TCPProxy) Close() error {
	if p.Listener != nil {
		p.Listener.Close()
	}
	p.activeConns.Wait()
	return nil
}

// Stats returns traffic stats.
func (p *TCPProxy) Stats() (conns int64, bytesIn int64, bytesOut int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.connCount, p.bytesIn, p.bytesOut
}

// BidirectionalCopy copies data between two connections in both directions.
func BidirectionalCopy(ctx context.Context, dst io.ReadWriteCloser, src io.ReadWriteCloser) error {
	errCh := make(chan error, 2)

	go func() {
		_, err := io.Copy(dst, src)
		src.Close()
		dst.Close()
		errCh <- err
	}()

	go func() {
		_, err := io.Copy(src, dst)
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		dst.Close()
		src.Close()
		return ctx.Err()
	case err := <-errCh:
		if err != nil && err != io.EOF {
			return err
		}
		return nil
	}
}

// DialLocal connects to the local service with a timeout.
func DialLocal(ctx context.Context, localPort int) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
}
