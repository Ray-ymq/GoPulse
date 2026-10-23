package metrics

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
	"github.com/Ray-ymq/GoPulse/runtimekit/metriccontract"
)

var ErrOutputTooLarge = errors.New("prometheus import body exceeds limit")

type Transformer struct{ MaxBytes int }

type renderedSample struct {
	name      string
	kind      string
	labels    map[string]string
	value     float64
	timestamp int64
}

func (t Transformer) Transform(message envelope.Envelope) ([]byte, error) {
	limit := t.MaxBytes
	if limit <= 0 {
		limit = 2 << 20
	}
	samples, err := samplesFor(message)
	if err != nil {
		return nil, &envelope.PermanentError{Code: "invalid_metric_batch"}
	}
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].timestamp != samples[j].timestamp {
			return samples[i].timestamp < samples[j].timestamp
		}
		return metriccontract.Key(samples[i].name, samples[i].labels) < metriccontract.Key(samples[j].name, samples[j].labels)
	})

	var b strings.Builder
	for _, sample := range samples {
		b.WriteString(sample.name)
		writeIdentityLabels(&b, message)
		keys := sortedKeys(sample.labels)
		for _, key := range keys {
			b.WriteByte(',')
			b.WriteString(key)
			b.WriteString(`="`)
			b.WriteString(escapeLabel(sample.labels[key]))
			b.WriteByte('"')
		}
		b.WriteString("} ")
		b.WriteString(strconv.FormatFloat(sample.value, 'g', -1, 64))
		b.WriteByte(' ')
		b.WriteString(strconv.FormatInt(sample.timestamp, 10))
		b.WriteByte('\n')
		if b.Len() > limit {
			return nil, ErrOutputTooLarge
		}
	}

	// Store one descriptor per metric in the same import as its samples. The
	// descriptor identity is derived from the outer source; producer metadata is
	// no longer required in the wire payload.
	type descriptor struct {
		name, kind string
		keys       map[string]bool
	}
	descriptors := map[string]*descriptor{}
	for _, sample := range samples {
		if !metriccontract.ValidKind(sample.kind) {
			continue
		}
		d := descriptors[sample.name]
		if d == nil {
			d = &descriptor{name: sample.name, kind: sample.kind, keys: map[string]bool{}}
			descriptors[sample.name] = d
		}
		for key := range sample.labels {
			d.keys[key] = true
		}
	}
	names := make([]string, 0, len(descriptors))
	for name := range descriptors {
		names = append(names, name)
	}
	sort.Strings(names)
	identity := identityFor(message)
	for _, name := range names {
		d := descriptors[name]
		keys := make([]string, 0, len(d.keys))
		for key := range d.keys {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, `%s{source="%s",target_id="%s",producer_kind="%s",producer_id="%s",metric="%s",kind="%s",unit="%s",label_keys="%s"} 1 %d`+"\n", metriccontract.MetadataName, escapeLabel(identity.source), escapeLabel(identity.target), escapeLabel(identity.kind), escapeLabel(identity.id), escapeLabel(d.name), escapeLabel(d.kind), escapeLabel(metriccontract.Unit(d.name, d.kind)), escapeLabel(strings.Join(keys, ",")), message.Timestamp.UnixMilli())
		if b.Len() > limit {
			return nil, ErrOutputTooLarge
		}
	}
	return []byte(b.String()), nil
}

func samplesFor(message envelope.Envelope) ([]renderedSample, error) {
	if len(message.Payload.Data) > 0 {
		var samples []renderedSample
		for _, item := range message.Payload.Data {
			for name, number := range item.Metrics {
				value, err := number.Float64()
				if err != nil {
					return nil, err
				}
				labels := make(map[string]string, len(item.Dimension))
				for key, label := range item.Dimension {
					labels[key] = label
				}
				timestamp := item.Timestamp
				if timestamp == 0 {
					timestamp = message.Timestamp.UnixMilli()
				}
				samples = append(samples, renderedSample{name: name, kind: metricKind(message.Source, name), labels: labels, value: value, timestamp: timestamp})
			}
		}
		return samples, nil
	}

	samples := make([]renderedSample, 0, len(message.Payload.Samples))
	for _, sample := range message.Payload.Samples {
		value := sample.FloatValue
		if value == 0 && sample.Value != "" {
			parsed, err := sample.Value.Float64()
			if err != nil {
				return nil, err
			}
			value = parsed
		}
		labels := make(map[string]string, len(sample.Labels))
		for key, label := range sample.Labels {
			labels[key] = label
		}
		samples = append(samples, renderedSample{name: sample.Name, kind: sample.Kind, labels: labels, value: value, timestamp: message.Timestamp.UnixMilli()})
	}
	return samples, nil
}

type identity struct{ source, target, kind, id string }

func identityFor(message envelope.Envelope) identity {
	source := message.Source
	if source == "" {
		switch {
		case message.Payload.TargetID == "redis-exporter-local":
			source = "redis"
		case strings.HasSuffix(message.Payload.ProducerID, "-exporter"):
			source = strings.TrimSuffix(message.Payload.ProducerID, "-exporter")
		case strings.HasSuffix(message.Payload.PluginID, "-exporter"):
			source = strings.TrimSuffix(message.Payload.PluginID, "-exporter")
		}
	}
	if source == "" {
		source = "redis"
	}

	target := message.Payload.TargetID
	if target == "" {
		target = source + "-exporter-local"
	}
	kind := message.Payload.ProducerKind
	if kind == "" {
		kind = "exporter_plugin"
	}
	id := message.Payload.ProducerID
	if id == "" {
		id = source + "-exporter"
	}
	return identity{source: source, target: target, kind: kind, id: id}
}

func writeIdentityLabels(b *strings.Builder, message envelope.Envelope) {
	identity := identityFor(message)
	if identity.source == "redis" {
		fmt.Fprintf(b, `{source="%s",target_id="%s"`, escapeLabel(identity.source), escapeLabel(identity.target))
		return
	}
	fmt.Fprintf(b, `{source="%s",target_id="%s",producer_kind="%s",producer_id="%s"`, escapeLabel(identity.source), escapeLabel(identity.target), escapeLabel(identity.kind), escapeLabel(identity.id))
}

func metricKind(source, name string) string {
	if strings.HasSuffix(name, "_total") || strings.HasSuffix(name, "_count") || strings.HasSuffix(name, "_bucket") {
		return "counter"
	}
	return "gauge"
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func escapeLabel(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
	return replacer.Replace(value)
}
