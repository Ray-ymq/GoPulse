package control

// Native, bounded data adapters. These run only behind the installation lock
// and maintenance barrier; no live volume or image bytes enter a backup.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/backup"
	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > backup.MaxPayload {
		return 0, errors.New("logical export exceeds format limit")
	}
	return b.Buffer.Write(p)
}
func (c *Controller) pipe(input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(c.ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", append([]string{"--host", c.endpoint}, args...)...)
	isolate(cmd)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/tmp", "DOCKER_CONFIG=/tmp/gopulse-docker-empty"}
	cmd.Stdin = bytes.NewReader(input)
	var out boundedOutput
	cmd.Stdout = &out
	// stderr is intentionally discarded; native tools can echo credentials/data.
	if cmd.Run() != nil {
		clear(out.Bytes())
		return nil, fail(Failed, c.state.Phase, "native data operation failed; private data and raw diagnostics suppressed")
	}
	return out.Bytes(), nil
}
func (c *Controller) container(service string) (Container, error) {
	cs, e := c.owned()
	if e != nil {
		return Container{}, e
	}
	for _, v := range cs {
		if v.Config.Labels["com.docker.compose.service"] == service {
			return v, nil
		}
	}
	return Container{}, fail(NotReady, "data-service", "required owned data service unavailable")
}
func (c *Controller) native(service string, input []byte, args ...string) ([]byte, error) {
	v, e := c.container(service)
	if e != nil {
		return nil, e
	}
	return c.pipe(input, append([]string{"exec", "-i", v.ID}, args...)...)
}
func (c *Controller) request(service, method, path, content string, body []byte) ([]byte, error) {
	v, e := c.container(service)
	if e != nil {
		return nil, e
	}
	var address string
	for _, n := range []string{c.state.Project + "_observability", c.state.Project + "_business"} {
		if a := v.NetworkSettings.Networks[n].IPAddress; net.ParseIP(a) != nil {
			address = a
			break
		}
	}
	ports := map[string]string{"elasticsearch": "9200", "victoriametrics": "8428", "rabbitmq": "15672"}
	if address == "" || ports[service] == "" || !strings.HasPrefix(path, "/") {
		return nil, fail(Ownership, "data-network", "owned internal data endpoint required")
	}
	req, e := http.NewRequestWithContext(c.ctx, method, "http://"+net.JoinHostPort(address, ports[service])+path, bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	if content != "" {
		req.Header.Set("Content-Type", content)
	}
	if service == "rabbitmq" {
		req.SetBasicAuth(c.env["RABBITMQ_USER"], c.env["RABBITMQ_PASSWORD"])
	}
	if service == "victoriametrics" {
		req.SetBasicAuth(c.env["VICTORIAMETRICS_USERNAME"], c.env["VICTORIAMETRICS_PASSWORD"])
	}
	client := &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(req)
	if e != nil {
		return nil, fail(Failed, c.state.Phase, "data API unavailable; raw diagnostics suppressed")
	}
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, backup.MaxPayload+1))
	if e != nil || len(b) > backup.MaxPayload || response.StatusCode < 200 || response.StatusCode >= 300 {
		clear(b)
		return nil, fail(Failed, c.state.Phase, "data API rejected operation; verify scoped health and backup compatibility")
	}
	return b, nil
}
func marshal(v any) []byte { b, _ := json.Marshal(v); return b }
func (c *Controller) sql(query string) ([]byte, error) {
	return c.native("mysql", []byte(query), "sh", "-c", `MYSQL_PWD="$MYSQL_PASSWORD" exec mysql --binary-mode=1 --batch --raw --skip-column-names -u "$MYSQL_USER" "$MYSQL_DATABASE"`)
}
func (c *Controller) mysqlExport() ([]byte, map[string]int64, error) {
	raw, e := c.sql("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME;")
	if e != nil {
		return nil, nil, e
	}
	counts := map[string]int64{}
	for _, name := range strings.Fields(string(raw)) {
		if !identifier.MatchString(name) {
			return nil, nil, fail(Failed, "mysql-schema", "unsupported table identity")
		}
		b, e := c.sql("SELECT COUNT(*) FROM `" + name + "`;")
		if e != nil {
			return nil, nil, e
		}
		n, e := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if e != nil {
			return nil, nil, e
		}
		counts[name] = n
	}
	dump, e := c.native("mysql", nil, "sh", "-c", `MYSQL_PWD="$MYSQL_PASSWORD" exec mysqldump -u "$MYSQL_USER" --single-transaction --skip-lock-tables --no-tablespaces --set-gtid-purged=OFF --hex-blob --order-by-primary --skip-comments --routines --events --triggers "$MYSQL_DATABASE"`)
	return dump, counts, e
}

var searchIdentity = regexp.MustCompile(`^gopulse-[a-z0-9_.-]+$`)

type searchHit struct {
	ID      string          `json:"_id"`
	Source  json.RawMessage `json:"_source"`
	Routing string          `json:"_routing,omitempty"`
}
type searchIndex struct {
	Name      string                     `json:"name"`
	Settings  map[string]json.RawMessage `json:"settings"`
	Mappings  json.RawMessage            `json:"mappings"`
	Aliases   json.RawMessage            `json:"aliases"`
	Documents []searchHit                `json:"documents"`
}
type searchExport struct {
	Schema  int           `json:"schema"`
	Indices []searchIndex `json:"indices"`
}

func (c *Controller) searchExport() ([]byte, map[string]int64, error) {
	raw, e := c.request("elasticsearch", "GET", "/gopulse-*", "", nil)
	if e != nil {
		return nil, nil, e
	}
	var indices map[string]struct {
		Settings struct {
			Index map[string]json.RawMessage `json:"index"`
		} `json:"settings"`
		Mappings json.RawMessage `json:"mappings"`
		Aliases  json.RawMessage `json:"aliases"`
	}
	if json.Unmarshal(raw, &indices) != nil {
		return nil, nil, fail(Failed, "search-export", "invalid search metadata")
	}
	result := searchExport{Schema: 1, Indices: []searchIndex{}}
	counts := map[string]int64{}
	names := make([]string, 0, len(indices))
	for name := range indices {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !searchIdentity.MatchString(name) {
			return nil, nil, fail(Failed, "search-export", "unsupported search index")
		}
		meta := indices[name]
		settings := map[string]json.RawMessage{}
		for _, key := range []string{"number_of_shards", "number_of_replicas", "analysis", "max_result_window", "refresh_interval"} {
			if v, ok := meta.Settings.Index[key]; ok {
				settings[key] = v
			}
		}
		index := searchIndex{name, settings, meta.Mappings, meta.Aliases, []searchHit{}}
		page, e := c.request("elasticsearch", "POST", "/"+name+"/_search?scroll=1m", "application/json", []byte(`{"size":1000,"sort":["_doc"],"query":{"match_all":{}}}`))
		if e != nil {
			return nil, nil, e
		}
		var scroll string
		for {
			var response struct {
				Scroll string `json:"_scroll_id"`
				Hits   struct {
					Hits []searchHit `json:"hits"`
				} `json:"hits"`
			}
			if json.Unmarshal(page, &response) != nil {
				return nil, nil, fail(Failed, "search-export", "invalid search page")
			}
			scroll = response.Scroll
			if len(response.Hits.Hits) == 0 {
				break
			}
			index.Documents = append(index.Documents, response.Hits.Hits...)
			if len(marshal(index)) > backup.MaxPayload/2 {
				return nil, nil, fail(Capacity, "search-export", "search export exceeds bounded format")
			}
			page, e = c.request("elasticsearch", "POST", "/_search/scroll", "application/json", marshal(map[string]string{"scroll": "1m", "scroll_id": scroll}))
			if e != nil {
				return nil, nil, e
			}
		}
		if scroll != "" {
			_, _ = c.request("elasticsearch", "DELETE", "/_search/scroll", "application/json", marshal(map[string][]string{"scroll_id": {scroll}}))
		}
		sort.Slice(index.Documents, func(i, j int) bool { return index.Documents[i].ID < index.Documents[j].ID })
		counts[name] = int64(len(index.Documents))
		result.Indices = append(result.Indices, index)
	}
	return canonicalJSON(marshal(result)), counts, nil
}
func (c *Controller) searchImport(raw []byte) error {
	var data searchExport
	if strictData(raw, &data) != nil || data.Schema != 1 {
		return fail(BackupInvalid, "search-import", "invalid search export")
	}
	for _, index := range data.Indices {
		if !searchIdentity.MatchString(index.Name) {
			return fail(BackupInvalid, "search-import", "invalid index identity")
		}
		if _, e := c.request("elasticsearch", "PUT", "/"+index.Name, "application/json", marshal(map[string]any{"settings": index.Settings, "mappings": index.Mappings, "aliases": index.Aliases})); e != nil {
			return e
		}
		var batch bytes.Buffer
		for _, hit := range index.Documents {
			metadata := map[string]string{"_index": index.Name, "_id": hit.ID}
			if hit.Routing != "" {
				metadata["routing"] = hit.Routing
			}
			batch.Write(marshal(map[string]any{"index": metadata}))
			batch.WriteByte('\n')
			batch.Write(hit.Source)
			batch.WriteByte('\n')
		}
		if batch.Len() > 0 {
			b, e := c.request("elasticsearch", "POST", "/_bulk?refresh=true", "application/x-ndjson", batch.Bytes())
			if e != nil {
				return e
			}
			var result struct {
				Errors bool `json:"errors"`
			}
			if json.Unmarshal(b, &result) != nil || result.Errors {
				return fail(Failed, "search-import", "search bulk import failed")
			}
		}
	}
	return nil
}

type metricSummary struct {
	Series  int64  `json:"series"`
	Samples int64  `json:"samples"`
	Start   int64  `json:"start"`
	End     int64  `json:"end"`
	Digest  string `json:"digest"`
}

func (c *Controller) metricFacts(end time.Time) (metricSummary, error) {
	values := url.Values{"match[]": {`{__name__!=""}`}, "start": {"0"}, "end": {end.UTC().Format(time.RFC3339Nano)}}
	raw, e := c.request("victoriametrics", "POST", "/api/v1/export", "application/x-www-form-urlencoded", []byte(values.Encode()))
	if e != nil {
		return metricSummary{}, e
	}
	// Canonical per-series points, independent of VM's block/line order.
	type line struct {
		Metric map[string]string `json:"metric"`
		Values []float64         `json:"values"`
		Times  []int64           `json:"timestamps"`
	}
	points := map[string]map[int64]float64{}
	d := json.NewDecoder(bytes.NewReader(raw))
	result := metricSummary{}
	for {
		var v line
		e = d.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil || len(v.Values) != len(v.Times) {
			return result, fail(Failed, "metrics-facts", "invalid metrics export")
		}
		key := string(marshal(v.Metric))
		if points[key] == nil {
			points[key] = map[int64]float64{}
		}
		for i, t := range v.Times {
			points[key][t] = v.Values[i]
			if result.Start == 0 || t < result.Start {
				result.Start = t
			}
			if t > result.End {
				result.End = t
			}
		}
	}
	for _, v := range points {
		result.Samples += int64(len(v))
	}
	result.Series = int64(len(points))
	result.Digest = release.Sum(marshal(points))
	return result, nil
}
func (c *Controller) metricExport(end time.Time) ([]byte, metricSummary, error) {
	facts, e := c.metricFacts(end)
	if e != nil {
		return nil, facts, e
	}
	values := url.Values{"match[]": {`{__name__!=""}`}, "start": {"0"}, "end": {end.UTC().Format(time.RFC3339Nano)}}
	raw, e := c.request("victoriametrics", "POST", "/api/v1/export/native", "application/x-www-form-urlencoded", []byte(values.Encode()))
	return raw, facts, e
}
func (c *Controller) waitDrain(check func() (bool, error)) error {
	deadline := time.NewTimer(120 * time.Second)
	defer deadline.Stop()
	for {
		ok, e := check()
		if e != nil {
			return e
		}
		if ok {
			return nil
		}
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-deadline.C:
			return fail(NotReady, "drain", "queues did not drain; maintenance retained; inspect scoped consumers")
		case <-time.After(time.Second):
		}
	}
}
func (c *Controller) rabbitEmpty() (bool, error) {
	v, e := c.container("rabbitmq")
	if e != nil {
		return false, e
	}
	raw, e := c.pipe(nil, "exec", "--user", "rabbitmq", "-i", v.ID, "rabbitmqctl", "-q", "list_queues", "-p", "/", "name", "messages_ready", "messages_unacknowledged", "--formatter", "json")
	if e != nil {
		return false, e
	}
	var queues []struct {
		Name    string `json:"name"`
		Ready   int64  `json:"messages_ready"`
		Unacked int64  `json:"messages_unacknowledged"`
	}
	if json.Unmarshal(raw, &queues) != nil {
		return false, fail(Failed, "rabbit-drain", "invalid authoritative broker queue status")
	}
	for _, q := range queues {
		if q.Ready+q.Unacked != 0 {
			return false, nil
		}
	}
	return true, nil
}
func (c *Controller) rabbitExport() ([]byte, error) {
	raw, e := c.request("rabbitmq", "GET", "/api/definitions/%2F", "", nil)
	if e != nil {
		return nil, e
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil, fail(Failed, "rabbit-export", "invalid topology")
	}
	// Per-vhost portable topology deliberately excludes users/password hashes,
	// permissions, policies and node/cluster metadata.
	out := map[string]json.RawMessage{}
	for _, key := range []string{"queues", "exchanges", "bindings"} {
		v, ok := values[key]
		if !ok {
			return nil, fail(Failed, "rabbit-export", "incomplete topology")
		}
		var entries []json.RawMessage
		if json.Unmarshal(v, &entries) != nil {
			return nil, fail(Failed, "rabbit-export", "invalid topology entries")
		}
		for i := range entries {
			entries[i] = canonicalJSON(entries[i])
		}
		sort.Slice(entries, func(i, j int) bool { return string(entries[i]) < string(entries[j]) })
		out[key] = marshal(entries)
	}
	return canonicalJSON(marshal(out)), nil
}

type kafkaOffset struct {
	Topic     string `json:"topic"`
	Partition int    `json:"partition"`
	Committed int64  `json:"committed"`
	End       int64  `json:"end"`
}
type kafkaState struct {
	Configs       map[string]string `json:"configs"`
	Schema        int               `json:"schema"`
	Topic         string            `json:"topic"`
	Group         string            `json:"group"`
	Offsets       []kafkaOffset     `json:"offsets"`
	RestorePolicy string            `json:"restore_policy"`
}

func (c *Controller) kafkaState() (kafkaState, error) {
	state := kafkaState{Schema: 1, Topic: "gopulse-observability-v1", Group: "gopulse-marshaller-metrics-v1", Offsets: []kafkaOffset{}, RestorePolicy: "drained-new-topic-rebase-zero", Configs: map[string]string{}}
	b, e := c.native("kafka", nil, "/opt/kafka/bin/kafka-consumer-groups.sh", "--bootstrap-server", "127.0.0.1:19092", "--describe", "--group", state.Group)
	if e != nil {
		return state, e
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || f[0] != state.Group {
			continue
		}
		p, ep := strconv.Atoi(f[2])
		offset, eo := strconv.ParseInt(f[3], 10, 64)
		end, ee := strconv.ParseInt(f[4], 10, 64)
		if ep != nil || eo != nil || ee != nil || offset < 0 || end < offset || f[1] != state.Topic {
			return state, fail(NotReady, "kafka-drain", "unsupported or uncommitted consumer topology")
		}
		state.Offsets = append(state.Offsets, kafkaOffset{f[1], p, offset, end})
	}
	if len(state.Offsets) != 1 || state.Offsets[0].Partition != 0 {
		return state, fail(NotReady, "kafka-drain", "expected the supported single-partition consumer topology")
	}
	config, e := c.native("kafka", nil, "/opt/kafka/bin/kafka-configs.sh", "--bootstrap-server", "127.0.0.1:19092", "--describe", "--entity-type", "topics", "--entity-name", state.Topic)
	if e != nil {
		return state, e
	}
	for _, line := range strings.Split(string(config), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		parts := strings.SplitN(fields[0], "=", 2)
		if len(parts) != 2 {
			continue
		}
		if !regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`).MatchString(parts[0]) || len(parts[1]) > 4096 {
			return state, fail(Failed, "kafka-config", "unsupported topic configuration")
		}
		state.Configs[parts[0]] = parts[1]
	}
	return state, nil
}
func (c *Controller) kafkaEmpty() (bool, error) {
	s, e := c.kafkaState()
	if e != nil {
		return false, e
	}
	for _, v := range s.Offsets {
		if v.Committed != v.End {
			return false, nil
		}
	}
	return true, nil
}
func (c *Controller) kafkaImport(data []byte) error {
	var state kafkaState
	if strictData(data, &state) != nil || state.Schema != 1 || state.Topic != "gopulse-observability-v1" || state.Group != "gopulse-marshaller-metrics-v1" || state.RestorePolicy != "drained-new-topic-rebase-zero" || len(state.Offsets) != 1 || state.Offsets[0].Committed != state.Offsets[0].End || state.Offsets[0].Partition != 0 {
		return fail(BackupInvalid, "kafka-import", "unsupported offset topology")
	}
	args := []string{"/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", "127.0.0.1:19092", "--create", "--topic", state.Topic, "--partitions", "1", "--replication-factor", "1"}
	keys := make([]string, 0, len(state.Configs))
	for k := range state.Configs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := state.Configs[key]
		if !regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`).MatchString(key) || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			return fail(BackupInvalid, "kafka-config", "invalid portable topic setting")
		}
		args = append(args, "--config", key+"="+value)
	}
	if _, e := c.native("kafka", nil, args...); e != nil {
		return e
	}
	_, e := c.native("kafka", nil, "/opt/kafka/bin/kafka-consumer-groups.sh", "--bootstrap-server", "127.0.0.1:19092", "--group", state.Group, "--topic", state.Topic, "--reset-offsets", "--to-offset", "0", "--execute")
	return e
}

func canonicalJSON(raw []byte) []byte {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil
	}
	return marshal(value)
}
func searchTimeRange(raw []byte, cutover time.Time) (time.Time, time.Time) {
	start, end := cutover, time.Time{}
	var data searchExport
	if json.Unmarshal(raw, &data) != nil {
		return cutover, cutover
	}
	for _, index := range data.Indices {
		for _, hit := range index.Documents {
			var document map[string]json.RawMessage
			if json.Unmarshal(hit.Source, &document) != nil {
				continue
			}
			for _, name := range []string{"@timestamp", "timestamp", "created_at", "updated_at", "occurred_at"} {
				var value string
				if json.Unmarshal(document[name], &value) != nil {
					continue
				}
				at, e := time.Parse(time.RFC3339Nano, value)
				if e != nil || at.After(cutover) {
					continue
				}
				if at.Before(start) {
					start = at
				}
				if at.After(end) {
					end = at
				}
			}
		}
	}
	if end.IsZero() {
		end = cutover
	}
	return start, end
}
