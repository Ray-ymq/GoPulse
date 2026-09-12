package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

var version = "development"
var revision = "unknown"

func run(args []string) error {
	if len(args) == 0 || (args[0] != "version" && args[0] != "manifest") {
		return errors.New("implemented commands: version, manifest")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "JSON output")
	path := fs.String("manifest", "", "release manifest path")
	socket := fs.String("docker-socket", "", "optional Docker socket for read-only server architecture preflight")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	var m *release.Manifest
	if *path != "" {
		data, err := os.ReadFile(*path)
		if err != nil {
			return err
		}
		m, err = release.Parse(data)
		if err != nil {
			return err
		}
		if err = m.CheckAssets(filepath.Dir(*path)); err != nil {
			return err
		}
		if err = m.CheckTool(version, revision, runtime.GOARCH, "linux", runtime.GOARCH); err != nil {
			return err
		}
	}
	if args[0] == "manifest" && m == nil {
		return errors.New("--manifest required")
	}
	if *socket != "" {
		if m == nil {
			return errors.New("server preflight requires --manifest")
		}
		client := http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", *socket)
		}}}
		resp, err := client.Get("http://docker/version")
		if err != nil {
			return errors.New("Docker server unavailable")
		}
		defer resp.Body.Close()
		var server struct {
			OS   string `json:"Os"`
			Arch string `json:"Arch"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&server) != nil {
			return errors.New("invalid Docker server version")
		}
		if err = m.CheckTool(version, revision, runtime.GOARCH, server.OS, server.Arch); err != nil {
			return err
		}
	}
	if args[0] == "manifest" {
		return json.NewEncoder(os.Stdout).Encode(m)
	}
	result := map[string]string{"version": version, "revision": revision, "platform": runtime.GOOS + "/" + runtime.GOARCH, "compose_schema": "1"}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	fmt.Printf("GoPulse %s (%s) %s\n", version, revision, result["platform"])
	return nil
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
