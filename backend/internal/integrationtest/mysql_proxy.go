//go:build integration

package integrationtest

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// MySQLFaultProxy drops one server reply after the server has executed a
// selected command. It keeps the production driver's one-second I/O timeout.
type MySQLFaultProxy struct {
	listener net.Listener
	target   string
	mu       sync.Mutex
	query    string
	rollback bool
	faults   int
	closed   bool
	conns    map[net.Conn]struct{}
	workers  sync.WaitGroup
	accepted chan struct{}
	stopped  chan struct{}
}

func NewMySQLFaultProxy(t *testing.T, target string) *MySQLFaultProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxy := &MySQLFaultProxy{listener: listener, target: target, conns: make(map[net.Conn]struct{}), accepted: make(chan struct{}), stopped: make(chan struct{})}
	go func() {
		defer close(proxy.accepted)
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			proxy.workers.Add(1)
			go proxy.serve(client)
		}
	}()
	t.Cleanup(func() {
		proxy.mu.Lock()
		proxy.closed = true
		close(proxy.stopped)
		_ = listener.Close()
		for conn := range proxy.conns {
			_ = conn.Close()
		}
		proxy.mu.Unlock()
		<-proxy.accepted
		proxy.workers.Wait()
	})
	return proxy
}

func (proxy *MySQLFaultProxy) Port() int { return proxy.listener.Addr().(*net.TCPAddr).Port }

// LoseNextReply optionally replaces COMMIT with ROLLBACK to prove that a
// failed commit without persisted facts must never be reported as successful.
func (proxy *MySQLFaultProxy) LoseNextReply(query string, rollback bool) {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	proxy.query, proxy.rollback, proxy.faults = query, rollback, 0
}

func (proxy *MySQLFaultProxy) Faults() int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return proxy.faults
}

func (proxy *MySQLFaultProxy) claim(query string) (bool, bool) {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	if proxy.query == "" || !strings.EqualFold(strings.TrimSpace(query), proxy.query) {
		return false, false
	}
	proxy.query = ""
	proxy.faults++
	return true, proxy.rollback
}

func (proxy *MySQLFaultProxy) track(conn net.Conn) bool {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	if proxy.closed {
		_ = conn.Close()
		return false
	}
	proxy.conns[conn] = struct{}{}
	return true
}

func (proxy *MySQLFaultProxy) serve(client net.Conn) {
	defer proxy.workers.Done()
	defer client.Close()
	if !proxy.track(client) {
		return
	}
	server, err := net.DialTimeout("tcp", proxy.target, time.Second)
	if err != nil {
		return
	}
	defer server.Close()
	if !proxy.track(server) {
		return
	}
	defer func() {
		proxy.mu.Lock()
		delete(proxy.conns, client)
		delete(proxy.conns, server)
		proxy.mu.Unlock()
	}()
	var loseReply atomic.Bool
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			packet, err := readMySQLPacket(client)
			if err != nil {
				return
			}
			if len(packet) > 4 && packet[4] == 3 { // COM_QUERY
				if claimed, rollback := proxy.claim(string(packet[5:])); claimed {
					loseReply.Store(true)
					if rollback {
						packet = append(packet[:4], append([]byte{3}, []byte("ROLLBACK")...)...)
						binary.LittleEndian.PutUint32(packet[:4], uint32(len(packet)-4))
					}
				}
			}
			if _, err := server.Write(packet); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			packet, err := readMySQLPacket(server)
			if err != nil {
				return
			}
			if loseReply.Swap(false) {
				// The upstream ACK has arrived, so COMMIT/ROLLBACK is complete.
				select {
				case <-time.After(1100 * time.Millisecond):
				case <-proxy.stopped:
				}
				return
			}
			if _, err := client.Write(packet); err != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}

func readMySQLPacket(conn net.Conn) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if size > 1<<20 {
		return nil, io.ErrShortBuffer
	}
	packet := make([]byte, 4+size)
	copy(packet, header)
	_, err := io.ReadFull(conn, packet[4:])
	return packet, err
}
