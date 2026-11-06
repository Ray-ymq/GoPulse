// selfmonitor-control is socket-activated by systemd, never run in Backend.
package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/selfmonitor"
)

func main() {
	if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
		log.Fatal("exactly one systemd socket is required")
	}
	f := os.NewFile(3, "systemd-socket")
	listener, err := net.FileListener(f)
	_ = f.Close()
	if err != nil {
		log.Fatal("socket unavailable")
	}
	defer func() { _ = listener.Close() }()
	if listener.Addr().Network() != "unix" {
		log.Fatal("Unix socket required")
	}
	server := &http.Server{Handler: selfmonitor.NewController(), ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 6 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 4096}
	log.Fatal(server.Serve(listener))
}
