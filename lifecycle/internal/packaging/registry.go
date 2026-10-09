package packaging

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Registry is the loopback registry one candidate build owns.
type Registry struct {
	Name      string
	Namespace string
}

// StartRegistry starts a uniquely named loopback registry on a free port and
// waits until it serves the registry API. `make package` needs no manual
// preparation, and only the container created here is ever removed.
func StartRegistry() (*Registry, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("cannot reserve a loopback port for the candidate registry")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, err
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return nil, err
	}
	registry := &Registry{
		Name:      "gopulse-package-registry-" + hex.EncodeToString(suffix),
		Namespace: fmt.Sprintf("127.0.0.1:%d/gopulse", port),
	}
	start := command("", "docker", "run", "-d", "--name", registry.Name,
		"-p", fmt.Sprintf("127.0.0.1:%d:5000", port), RegistryImage)
	start.Stdout = os.Stdout
	if err := start.Run(); err != nil {
		return nil, fmt.Errorf("cannot start the candidate registry: %w", err)
	}
	if err := registry.wait(port); err != nil {
		registry.Stop()
		return nil, err
	}
	return registry, nil
}

// wait polls the registry API until it answers or the bounded wait expires.
func (r *Registry) wait(port int) error {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/v2/", port)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, err := (&http.Client{Timeout: 2 * time.Second}).Get(endpoint)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("candidate registry %s did not become ready", r.Name)
}

// Stop removes the registry container this process created, and nothing else.
func (r *Registry) Stop() {
	if r == nil || !strings.HasPrefix(r.Name, "gopulse-package-registry-") {
		return
	}
	remove := command("", "docker", "rm", "-f", "-v", r.Name)
	remove.Stdout = os.Stdout
	if err := remove.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "gopulse-package: warning: cannot remove the candidate registry %s: %v\n", r.Name, err)
	}
}
