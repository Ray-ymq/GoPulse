package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/runtimekit/exportercontract"
)

const nativeNodeID = "node-exporter"

func nativeNodeManifest(m Manifest) bool {
	return m.SchemaVersion == 2 && m.ID == nativeNodeID && m.Source == "node" && m.Entrypoint == "bin/node_exporter"
}

func parseNativeNodeRequest(data []byte) (json.RawMessage, json.RawMessage, error) {
	config, secrets, _, err := exportercontract.ReadManagement(data)
	if err != nil {
		return nil, nil, invalidConfig()
	}
	private, err := exportercontract.Object(secrets)
	if err != nil || len(private) != 0 {
		return nil, nil, invalidConfig()
	}
	if _, err := nativeNodeArguments(config, 9127); err != nil {
		return nil, nil, err
	}
	return append(json.RawMessage(nil), config...), json.RawMessage(`{}`), nil
}

func nativeNodeArguments(config json.RawMessage, port int) ([]string, error) {
	fields, err := exportercontract.Object(config)
	if err != nil || port < 1 || port > 65535 {
		return nil, invalidConfig()
	}
	defaults := map[string]string{"rootfs": "/", "procfs": "/proc", "sysfs": "/sys"}
	for name, raw := range fields {
		if _, ok := defaults[name]; !ok {
			return nil, invalidConfig()
		}
		var value string
		if json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) || len(value) > 4096 || !filepath.IsAbs(value) || filepath.Clean(value) != value || strings.ContainsAny(value, "\x00\r\n") {
			return nil, invalidConfig()
		}
		defaults[name] = value
	}
	args := []string{"--web.listen-address=127.0.0.1:" + strconv.Itoa(port), "--web.telemetry-path=/metrics", "--web.disable-exporter-metrics", "--web.max-requests=1", "--collector.disable-defaults"}
	for _, name := range []string{"cpu", "meminfo", "loadavg", "diskstats", "filesystem", "netdev", "stat", "vmstat", "time", "uname"} {
		args = append(args, "--collector."+name)
	}
	for _, name := range []string{"rootfs", "procfs", "sysfs"} {
		args = append(args, "--path."+name+"="+defaults[name])
	}
	return args, nil
}

// checkNativeNode 在临时目录启动官方程序，采集成功后关闭进程，不修改插件状态。
func (c *runtimeCore) checkNativeNode(ctx context.Context, data []byte) error {
	if _, err := c.slot(nativeNodeID); err != nil {
		return err
	}
	config, _, err := parseNativeNodeRequest(data)
	if err != nil {
		return err
	}
	pin, err := c.current(nativeNodeID)
	if err != nil {
		return err
	}
	if !nativeNodeManifest(pin.Manifest) {
		return operationFailed()
	}
	stage, err := os.MkdirTemp("", "gopulse-native-node-check-")
	if err != nil {
		return operationFailed()
	}
	defer func() { _ = os.RemoveAll(stage) }()
	release := filepath.Join(stage, "releases", pin.Manifest.Version)
	if os.MkdirAll(release, 0700) != nil || os.MkdirAll(filepath.Join(stage, "runtime"), 0700) != nil {
		return operationFailed()
	}
	manifest, err := c.catalog.extractVerified(nativeNodeID, pin.Manifest.Version, "", release)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return operationFailed()
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if listener.Close() != nil {
		return operationFailed()
	}
	args, err := nativeNodeArguments(config, port)
	if err != nil {
		return err
	}
	timeout, cancel := context.WithTimeout(ctx, c.cfg.StartupTimeout)
	defer cancel()
	origin := "http://127.0.0.1:" + strconv.Itoa(port)
	process, err := startProcess(timeout, stage, manifest, nil, origin+"/", c.cfg.StartupTimeout, args...)
	if err != nil {
		return operationFailed()
	}
	defer func() {
		_ = terminateProcess(process.record, c.cfg.StopTimeout)
		select {
		case <-process.done:
		case <-time.After(c.cfg.StopTimeout):
		}
	}()
	request, err := http.NewRequestWithContext(timeout, http.MethodGet, origin+"/metrics", nil)
	if err != nil {
		return operationFailed()
	}
	request.Header.Set("Accept", "text/plain; version=0.0.4")
	client := &http.Client{Timeout: c.cfg.StartupTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return operationFailed()
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || c.cfg.ValidateSourceSnapshot == nil || c.cfg.ValidateSourceSnapshot("node", response.StatusCode, body) != nil {
		return operationFailed()
	}
	return nil
}
