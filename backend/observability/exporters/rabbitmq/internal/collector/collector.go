// Package collector reads a bounded, fixed-vhost management snapshot. No target
// URL, API path or object name is accepted from management API callers.
package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrUnavailable = errors.New("target_unavailable")

const Unavailable = "# TYPE gopulse_rabbitmq_up gauge\ngopulse_rabbitmq_up 0\n"

type Client struct {
	HTTP                       *http.Client
	Origin, Username, Password string
}

func (c *Client) CloseIdleConnections() { c.HTTP.CloseIdleConnections() }
func (c *Client) read(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Origin+path, nil)
	if err != nil {
		return ErrUnavailable
	}
	req.SetBasicAuth(c.Username, c.Password)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Encoding") != "" {
		return ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return ErrUnavailable
	}
	if json.Unmarshal(raw, dst) != nil {
		return ErrUnavailable
	}
	return nil
}

type vhost struct {
	Name  string          `json:"name"`
	Stats json.RawMessage `json:"message_stats"`
}
type scoped struct {
	Vhost string `json:"vhost"`
}
type connection struct {
	Vhost   string  `json:"vhost"`
	State   string  `json:"state"`
	RecvOct *uint64 `json:"recv_oct"`
	SendOct *uint64 `json:"send_oct"`
}
type queue struct {
	Name         string          `json:"name"`
	Vhost        string          `json:"vhost"`
	Consumers    *uint64         `json:"consumers"`
	Ready        *uint64         `json:"messages_ready"`
	Unacked      *uint64         `json:"messages_unacknowledged"`
	Capacity     *float64        `json:"consumer_utilisation"`
	Memory       *uint64         `json:"memory"`
	MessageBytes *uint64         `json:"message_bytes"`
	Stats        json.RawMessage `json:"message_stats"`
}

type node struct {
	Name           string   `json:"name"`
	MemoryUsed     *uint64  `json:"mem_used"`
	MemoryLimit    *uint64  `json:"mem_limit"`
	DiskFree       *uint64  `json:"disk_free"`
	DiskFreeLimit  *uint64  `json:"disk_free_limit"`
	FDUsed         *uint64  `json:"fd_used"`
	FDTotal        *uint64  `json:"fd_total"`
	ProcessesUsed  *uint64  `json:"proc_used"`
	ProcessesTotal *uint64  `json:"proc_total"`
	MemoryAlarm    *bool    `json:"mem_alarm"`
	DiskFreeAlarm  *bool    `json:"disk_free_alarm"`
	IOReadCount    *uint64  `json:"io_read_count"`
	IOWriteCount   *uint64  `json:"io_write_count"`
	IOSyncCount    *uint64  `json:"io_sync_count"`
	IOSeekCount    *uint64  `json:"io_seek_count"`
	IOReopenCount  *uint64  `json:"io_reopen_count"`
	IOReadAvg      *float64 `json:"io_read_avg_time"`
	IOWriteAvg     *float64 `json:"io_write_avg_time"`
	IOSyncAvg      *float64 `json:"io_sync_avg_time"`
	IOSeekAvg      *float64 `json:"io_seek_avg_time"`
	IOReopenAvg    *float64 `json:"io_reopen_avg_time"`
}

func Collect(ctx context.Context, c *Client) (string, error) {
	var vhosts []vhost
	if c.read(ctx, "/api/vhosts", &vhosts) != nil || vhosts == nil {
		return "", ErrUnavailable
	}
	var target *vhost
	for i := range vhosts {
		if vhosts[i].Name == "/" {
			if target != nil {
				return "", ErrUnavailable
			}
			target = &vhosts[i]
		}
	}
	if target == nil {
		return "", ErrUnavailable
	}
	var connections []connection
	var channels []scoped
	var queues []queue
	var nodes []node
	if c.read(ctx, "/api/vhosts/%2F/connections", &connections) != nil || connections == nil || c.read(ctx, "/api/vhosts/%2F/channels", &channels) != nil || channels == nil || c.read(ctx, "/api/queues/%2F", &queues) != nil || queues == nil || c.read(ctx, "/api/nodes?columns=name,mem_used,mem_limit,disk_free,disk_free_limit,fd_used,fd_total,proc_used,proc_total,mem_alarm,disk_free_alarm,io_read_count,io_write_count,io_sync_count,io_seek_count,io_reopen_count,io_read_avg_time,io_write_avg_time,io_sync_avg_time,io_seek_avg_time,io_reopen_avg_time", &nodes) != nil || nodes == nil || len(nodes) == 0 {
		return "", ErrUnavailable
	}
	for _, item := range connections {
		if item.Vhost != "/" {
			return "", ErrUnavailable
		}
	}
	for _, item := range channels {
		if item.Vhost != "/" {
			return "", ErrUnavailable
		}
	}
	if len(connections) > 4096 || len(channels) > 8192 || len(queues) > 1024 || len(nodes) > 128 {
		return "", ErrUnavailable
	}
	values := map[string]uint64{"connections": uint64(len(connections)), "channels": uint64(len(channels)), "queues": uint64(len(queues))}
	var blockedConnections, receivedBytes, sentBytes uint64
	hasConnectionState, completeReceivedBytes, completeSentBytes := false, len(connections) > 0, len(connections) > 0
	for _, item := range connections {
		if item.State != "" {
			hasConnectionState = true
			state := strings.ToLower(item.State)
			if state == "blocked" || state == "blocking" {
				blockedConnections++
			}
		}
		if item.RecvOct == nil {
			completeReceivedBytes = false
		} else if !addUint64(&receivedBytes, *item.RecvOct) {
			return "", ErrUnavailable
		}
		if item.SendOct == nil {
			completeSentBytes = false
		} else if !addUint64(&sentBytes, *item.SendOct) {
			return "", ErrUnavailable
		}
	}
	queueStats := make([]map[string]uint64, len(queues))
	for i, q := range queues {
		if q.Vhost != "/" || q.Consumers == nil || q.Ready == nil || q.Unacked == nil {
			return "", ErrUnavailable
		}
		if !validLabel(q.Name) {
			return "", ErrUnavailable
		}
		for key, n := range map[string]uint64{"consumers": *q.Consumers, "ready": *q.Ready, "unacked": *q.Unacked} {
			if ^uint64(0)-values[key] < n {
				return "", ErrUnavailable
			}
			values[key] += n
		}
		queueStats[i] = messageCounters(q.Stats)
		if queueStats[i] == nil {
			return "", ErrUnavailable
		}
	}
	// RabbitMQ 3.13.3 documents that message_stats fields only appear after
	// activity. This exception is limited to the three counters, never gauges.
	stats := map[string]json.RawMessage{}
	if len(target.Stats) > 0 {
		if string(target.Stats) == "null" || json.Unmarshal(target.Stats, &stats) != nil {
			return "", ErrUnavailable
		}
	}
	for name, key := range map[string]string{"published_total": "publish", "delivered_total": "deliver_get", "acked_total": "ack"} {
		var n uint64
		if raw, ok := stats[key]; ok {
			if string(raw) == "null" || json.Unmarshal(raw, &n) != nil {
				return "", ErrUnavailable
			}
		}
		values[name] = n
	}
	seenNodes := map[string]bool{}
	for _, n := range nodes {
		if n.Name == "" || !validLabel(n.Name) || seenNodes[n.Name] || n.MemoryUsed == nil || n.MemoryLimit == nil || n.DiskFree == nil || n.DiskFreeLimit == nil || n.FDUsed == nil || n.FDTotal == nil || n.ProcessesUsed == nil || n.ProcessesTotal == nil {
			return "", ErrUnavailable
		}
		seenNodes[n.Name] = true
	}
	var b strings.Builder
	b.WriteString("# TYPE gopulse_rabbitmq_up gauge\ngopulse_rabbitmq_up 1\n")
	hasCapacity, hasMemoryAlarm, hasDiskAlarm := false, false, false
	hasQueueMemory, hasQueueMessageBytes := false, false
	for _, q := range queues {
		hasCapacity = hasCapacity || q.Capacity != nil
		hasQueueMemory = hasQueueMemory || q.Memory != nil
		hasQueueMessageBytes = hasQueueMessageBytes || q.MessageBytes != nil
	}
	hasNodeIOOperations, hasNodeIOAverage := false, false
	for _, n := range nodes {
		hasMemoryAlarm = hasMemoryAlarm || n.MemoryAlarm != nil
		hasDiskAlarm = hasDiskAlarm || n.DiskFreeAlarm != nil
		hasNodeIOOperations = hasNodeIOOperations || n.IOReadCount != nil || n.IOWriteCount != nil || n.IOSyncCount != nil || n.IOSeekCount != nil || n.IOReopenCount != nil
		hasNodeIOAverage = hasNodeIOAverage || validAverageMilliseconds(n.IOReadAvg) || validAverageMilliseconds(n.IOWriteAvg) || validAverageMilliseconds(n.IOSyncAvg) || validAverageMilliseconds(n.IOSeekAvg) || validAverageMilliseconds(n.IOReopenAvg)
	}
	fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_queue_messages gauge\n# TYPE gopulse_rabbitmq_queue_consumers gauge\n# TYPE gopulse_rabbitmq_queue_messages_total counter\n")
	if hasQueueMemory {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_queue_memory_bytes gauge\n")
	}
	if hasQueueMessageBytes {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_queue_message_bytes gauge\n")
	}
	if hasCapacity {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_queue_consumer_capacity gauge\n")
	}
	fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_node_memory_used_bytes gauge\n# TYPE gopulse_rabbitmq_node_memory_limit_bytes gauge\n# TYPE gopulse_rabbitmq_node_disk_free_bytes gauge\n# TYPE gopulse_rabbitmq_node_disk_free_limit_bytes gauge\n# TYPE gopulse_rabbitmq_node_file_descriptors_used gauge\n# TYPE gopulse_rabbitmq_node_file_descriptors_limit gauge\n# TYPE gopulse_rabbitmq_node_processes_used gauge\n# TYPE gopulse_rabbitmq_node_processes_limit gauge\n")
	if hasMemoryAlarm {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_node_memory_alarm gauge\n")
	}
	if hasDiskAlarm {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_node_disk_free_alarm gauge\n")
	}
	if hasConnectionState {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_connections_blocked gauge\n")
	}
	if completeReceivedBytes {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_connection_received_bytes_total counter\n")
	}
	if completeSentBytes {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_connection_sent_bytes_total counter\n")
	}
	if hasNodeIOOperations {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_node_io_operations_total counter\n")
	}
	if hasNodeIOAverage {
		fmt.Fprint(&b, "# TYPE gopulse_rabbitmq_node_io_average_duration_seconds gauge\n")
	}
	for _, name := range []string{"connections", "channels", "queues", "consumers", "messages", "published_total", "delivered_total", "acked_total"} {
		kind := "gauge"
		if strings.HasSuffix(name, "_total") {
			kind = "counter"
		}
		fmt.Fprintf(&b, "# TYPE gopulse_rabbitmq_%s %s\n", name, kind)
		if name == "messages" {
			fmt.Fprintf(&b, "gopulse_rabbitmq_messages{state=\"ready\"} %d\ngopulse_rabbitmq_messages{state=\"unacked\"} %d\n", values["ready"], values["unacked"])
		} else {
			fmt.Fprintf(&b, "gopulse_rabbitmq_%s %d\n", name, values[name])
		}
	}
	if hasConnectionState {
		fmt.Fprintf(&b, "gopulse_rabbitmq_connections_blocked %d\n", blockedConnections)
	}
	if completeReceivedBytes {
		fmt.Fprintf(&b, "gopulse_rabbitmq_connection_received_bytes_total %d\n", receivedBytes)
	}
	if completeSentBytes {
		fmt.Fprintf(&b, "gopulse_rabbitmq_connection_sent_bytes_total %d\n", sentBytes)
	}
	for i, q := range queues {
		base := fmt.Sprintf(`{vhost=%q,queue=%q`, "/", q.Name)
		fmt.Fprintf(&b, "gopulse_rabbitmq_queue_messages%s} %d\n", base+`,state="ready"`, *q.Ready)
		fmt.Fprintf(&b, "gopulse_rabbitmq_queue_messages%s} %d\n", base+`,state="unacked"`, *q.Unacked)
		fmt.Fprintf(&b, "gopulse_rabbitmq_queue_consumers%s} %d\n", base, *q.Consumers)
		if q.Memory != nil {
			fmt.Fprintf(&b, "gopulse_rabbitmq_queue_memory_bytes%s} %d\n", base, *q.Memory)
		}
		if q.MessageBytes != nil {
			fmt.Fprintf(&b, "gopulse_rabbitmq_queue_message_bytes%s} %d\n", base, *q.MessageBytes)
		}
		if q.Capacity != nil {
			if *q.Capacity < 0 || *q.Capacity > 1 {
				return "", ErrUnavailable
			}
			fmt.Fprintf(&b, "gopulse_rabbitmq_queue_consumer_capacity%s} %s\n", base, strconv.FormatFloat(*q.Capacity, 'g', -1, 64))
		}
		for _, operation := range []string{"publish", "deliver", "ack", "redeliver"} {
			fmt.Fprintf(&b, "gopulse_rabbitmq_queue_messages_total%s,operation=%q} %d\n", base, operation, queueStats[i][operation])
		}
	}
	for _, n := range nodes {
		labels := fmt.Sprintf(`{node_id=%q}`, n.Name)
		fmt.Fprintf(&b, "gopulse_rabbitmq_node_memory_used_bytes%s %d\n", labels, *n.MemoryUsed)
		fmt.Fprintf(&b, "gopulse_rabbitmq_node_memory_limit_bytes%s %d\n", labels, *n.MemoryLimit)
		fmt.Fprintf(&b, "gopulse_rabbitmq_node_disk_free_bytes%s %d\n", labels, *n.DiskFree)
		fmt.Fprintf(&b, "gopulse_rabbitmq_node_disk_free_limit_bytes%s %d\n", labels, *n.DiskFreeLimit)
		fmt.Fprintf(&b, "gopulse_rabbitmq_node_file_descriptors_used%s %d\n", labels, *n.FDUsed)
		fmt.Fprintf(&b, "gopulse_rabbitmq_node_file_descriptors_limit%s %d\n", labels, *n.FDTotal)
		fmt.Fprintf(&b, "gopulse_rabbitmq_node_processes_used%s %d\n", labels, *n.ProcessesUsed)
		fmt.Fprintf(&b, "gopulse_rabbitmq_node_processes_limit%s %d\n", labels, *n.ProcessesTotal)
		if n.MemoryAlarm != nil {
			fmt.Fprintf(&b, "gopulse_rabbitmq_node_memory_alarm%s %d\n", labels, boolValue(*n.MemoryAlarm))
		}
		if n.DiskFreeAlarm != nil {
			fmt.Fprintf(&b, "gopulse_rabbitmq_node_disk_free_alarm%s %d\n", labels, boolValue(*n.DiskFreeAlarm))
		}
		for _, item := range nodeIOMetrics(n) {
			if item.count != nil {
				fmt.Fprintf(&b, "gopulse_rabbitmq_node_io_operations_total{node_id=%q,operation=%q} %d\n", n.Name, item.operation, *item.count)
			}
			if validAverageMilliseconds(item.average) {
				fmt.Fprintf(&b, "gopulse_rabbitmq_node_io_average_duration_seconds{node_id=%q,operation=%q} %s\n", n.Name, item.operation, strconv.FormatFloat(*item.average/1000, 'g', -1, 64))
			}
		}
	}
	return b.String(), nil
}

type nodeIOMetric struct {
	operation string
	count     *uint64
	average   *float64
}

func nodeIOMetrics(n node) []nodeIOMetric {
	return []nodeIOMetric{
		{operation: "read", count: n.IOReadCount, average: n.IOReadAvg},
		{operation: "write", count: n.IOWriteCount, average: n.IOWriteAvg},
		{operation: "sync", count: n.IOSyncCount, average: n.IOSyncAvg},
		{operation: "seek", count: n.IOSeekCount, average: n.IOSeekAvg},
		{operation: "reopen", count: n.IOReopenCount, average: n.IOReopenAvg},
	}
}

func validAverageMilliseconds(value *float64) bool {
	return value != nil && *value >= 0 && !math.IsNaN(*value) && !math.IsInf(*value, 0)
}

func addUint64(total *uint64, value uint64) bool {
	if ^uint64(0)-*total < value {
		return false
	}
	*total += value
	return true
}

func messageCounters(raw json.RawMessage) map[string]uint64 {
	values := map[string]uint64{"publish": 0, "deliver": 0, "ack": 0, "redeliver": 0}
	if len(raw) == 0 || string(raw) == "null" {
		return values
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(raw, &source) != nil {
		return nil
	}
	for _, key := range []string{"publish", "deliver_get", "ack", "redeliver"} {
		rawValue, ok := source[key]
		if !ok {
			continue
		}
		var value uint64
		if json.Unmarshal(rawValue, &value) != nil {
			return nil
		}
		if key == "deliver_get" {
			values["deliver"] = value
		} else {
			values[key] = value
		}
	}
	return values
}

func validLabel(value string) bool {
	return value != "" && utf8.ValidString(value) && len(value) <= 256 && !strings.ContainsRune(value, 0)
}

func boolValue(value bool) int {
	if value {
		return 1
	}
	return 0
}
