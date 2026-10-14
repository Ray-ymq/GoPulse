// Command mysql-protocol-proxy is a bounded MySQL packet proxy used only by
// native acceptance. It understands enough of the classic protocol to select
// one COM_QUERY response, while forwarding every other packet unchanged.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	modeCommitResult    = "commit-result"
	modeCompletionWrite = "completion-write"
	maxPacketPayload    = 64 << 20
)

type status struct {
	Mode             string `json:"mode"`
	QueriesSeen      uint64 `json:"queries_seen"`
	TargetQueries    uint64 `json:"target_queries"`
	UpstreamEffect   uint64 `json:"upstream_effect"`
	ResponseDropped  uint64 `json:"response_dropped"`
	ConnectionClosed uint64 `json:"connection_closed"`
}

type statusStore struct {
	mu sync.Mutex
	status
	armed bool
}

type connectionState struct {
	mu                   sync.Mutex
	armed                bool
	pendingPrepareTarget bool
	preparedTargets      map[uint32]bool
}

func (state *connectionState) setArmed() {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.armed = true
}

func (state *connectionState) isArmed() bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.armed
}

func (state *connectionState) notePrepare(target bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.pendingPrepareTarget = target
}

func (state *connectionState) registerPreparedResponse(payload []byte) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.pendingPrepareTarget {
		state.pendingPrepareTarget = false
		return
	}
	state.pendingPrepareTarget = false
	if len(payload) < 5 || payload[0] != 0x00 {
		return
	}
	if state.preparedTargets == nil {
		state.preparedTargets = make(map[uint32]bool)
	}
	statementID := binary.LittleEndian.Uint32(payload[1:5])
	state.preparedTargets[statementID] = true
}

func (state *connectionState) isPreparedTarget(statementID uint32) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.preparedTargets[statementID]
}

func (store *statusStore) update(update func(*status)) {
	store.mu.Lock()
	defer store.mu.Unlock()
	update(&store.status)
}

func (store *statusStore) snapshot() status {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.status
}

func (store *statusStore) arm() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.armed = true
}

func (store *statusStore) claimTarget() bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.armed {
		return false
	}
	store.armed = false
	store.TargetQueries++
	return true
}

func main() {
	listen := flag.String("listen", "0.0.0.0:3306", "MySQL proxy listener")
	control := flag.String("control", "127.0.0.1:19125", "status HTTP listener")
	upstream := flag.String("upstream", "", "upstream MySQL address")
	mode := flag.String("mode", modeCommitResult, "commit-result or completion-write")
	flag.Parse()
	if *upstream == "" || (*mode != modeCommitResult && *mode != modeCompletionWrite) {
		fmt.Fprintln(os.Stderr, "upstream and a supported mode are required")
		os.Exit(2)
	}
	store := &statusStore{status: status{Mode: *mode}}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = listener.Close() }()
	controlServer := &http.Server{Addr: *control, Handler: statusHandler(store), ReadHeaderTimeout: 2 * time.Second}
	controlListener, err := net.Listen("tcp", *control)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = controlListener.Close() }()
	go func() { _ = controlServer.Serve(controlListener) }()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = controlServer.Shutdown(shutdown)
	}()
	for {
		client, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		go proxy(client, *upstream, *mode, store)
	}
}

func statusHandler(store *statusStore) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ready" {
			writer.WriteHeader(http.StatusOK)
			return
		}
		if request.URL.Path == "/arm" {
			if request.Method != http.MethodPost {
				writer.Header().Set("Allow", http.MethodPost)
				writer.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			store.arm()
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		if request.URL.Path != "/status" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(store.snapshot())
	})
}

func proxy(client net.Conn, upstreamAddress, mode string, store *statusStore) {
	defer func() { _ = client.Close() }()
	upstream, err := net.DialTimeout("tcp", upstreamAddress, 5*time.Second)
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()
	closed := make(chan struct{})
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			store.update(func(value *status) { value.ConnectionClosed++ })
			close(closed)
			_ = client.Close()
			_ = upstream.Close()
		})
	}
	var wait sync.WaitGroup
	connection := &connectionState{}
	wait.Add(2)
	go func() {
		defer wait.Done()
		clientToServer(client, upstream, mode, store, connection)
		closeBoth()
	}()
	go func() {
		defer wait.Done()
		serverToClient(upstream, client, mode, store, connection, closeBoth)
		closeBoth()
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Minute):
		closeBoth()
	}
	wait.Wait()
}

func clientToServer(client, upstream net.Conn, mode string, store *statusStore, connection *connectionState) {
	for {
		payload, sequence, err := readPacket(client)
		if err != nil {
			return
		}
		if len(payload) > 0 {
			target := false
			switch payload[0] {
			case 0x03: // COM_QUERY
				query := strings.ToUpper(strings.TrimSpace(string(payload[1:])))
				target = targetQuery(mode, query)
				store.update(func(value *status) { value.QueriesSeen++ })
			case 0x16: // COM_STMT_PREPARE
				query := strings.ToUpper(strings.TrimSpace(string(payload[1:])))
				connection.notePrepare(targetQuery(mode, query))
			case 0x17: // COM_STMT_EXECUTE
				if len(payload) >= 5 {
					statementID := binary.LittleEndian.Uint32(payload[1:5])
					target = connection.isPreparedTarget(statementID)
				}
			}
			if target && store.claimTarget() {
				connection.setArmed()
			}
		}
		if err := writePacket(upstream, payload, sequence); err != nil {
			return
		}
	}
}

func serverToClient(upstream, client net.Conn, mode string, store *statusStore, connection *connectionState, closeBoth func()) {
	for {
		payload, sequence, err := readPacket(upstream)
		if err != nil {
			return
		}
		connection.registerPreparedResponse(payload)
		if connection.isArmed() && isSuccessfulResponse(payload) {
			store.update(func(value *status) { value.UpstreamEffect++ })
			store.update(func(value *status) { value.ResponseDropped++ })
			closeBoth()
			return
		}
		if err := writePacket(client, payload, sequence); err != nil {
			return
		}
	}
}

func targetQuery(mode, query string) bool {
	query = strings.ToUpper(strings.TrimSpace(query))
	return (mode == modeCommitResult && strings.HasPrefix(query, "COMMIT")) || (mode == modeCompletionWrite && strings.HasPrefix(query, "INSERT") && strings.Contains(query, "BUSINESS_APPLICATION_RESULTS"))
}

func isSuccessfulResponse(payload []byte) bool {
	return len(payload) > 0 && (payload[0] == 0x00 || payload[0] == 0xfe)
}

func readPacket(reader io.Reader) ([]byte, byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, 0, err
	}
	size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if size > maxPacketPayload {
		return nil, 0, errors.New("MySQL packet is too large")
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, 0, err
	}
	return payload, header[3], nil
}

func writePacket(writer io.Writer, payload []byte, sequence byte) error {
	if len(payload) > maxPacketPayload || len(payload) > 0xffffff {
		return errors.New("MySQL packet is too large")
	}
	header := []byte{byte(len(payload)), byte(len(payload) >> 8), byte(len(payload) >> 16), sequence}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}
