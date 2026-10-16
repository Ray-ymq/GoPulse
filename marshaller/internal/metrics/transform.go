package metrics

import (
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/Ray-ymq/GoPulse/componentmetrics/metriccontract"
	"sort"
	"strconv"
	"strings"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
)

var ErrOutputTooLarge = errors.New("prometheus import body exceeds limit")

type Transformer struct{ MaxBytes int }

func (t Transformer) Transform(message envelope.Envelope) ([]byte, error) {
	limit := t.MaxBytes
	if limit <= 0 {
		limit = 2 << 20
	}
	samples := append([]envelope.Sample(nil), message.Payload.Samples...)
	sort.Slice(samples, func(i, j int) bool { return envelope.CanonicalKey(samples[i]) < envelope.CanonicalKey(samples[j]) })
	var b strings.Builder
	for _, sample := range samples {
		b.WriteString(sample.Name)
		if componentmetrics.IsComponent(message.Source) {
			fmt.Fprintf(&b, `{source="%s",target_id="%s",producer_kind="component",producer_id="%s"`, message.Source, message.Payload.TargetID, message.Payload.ProducerID)
		} else if message.Source == "mysql" || message.Source == "rabbitmq" || message.Source == "kafka" || message.Source == "elasticsearch" || message.Source == "victoriametrics" {
			fmt.Fprintf(&b, `{source="%s",target_id="%s",producer_kind="exporter_plugin",producer_id="%s"`, message.Source, message.Payload.TargetID, message.Payload.ProducerID)
		} else {
			b.WriteString(`{source="redis",target_id="redis-exporter-local"`)
		}
		keys := make([]string, 0, len(sample.Labels))
		for key := range sample.Labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			b.WriteByte(',')
			b.WriteString(key)
			b.WriteString(`="`)
			b.WriteString(escapeLabel(sample.Labels[key]))
			b.WriteByte('"')
		}
		b.WriteString("} ")
		b.WriteString(strconv.FormatFloat(sample.FloatValue, 'g', -1, 64))
		b.WriteByte(' ')
		b.WriteString(strconv.FormatInt(message.Timestamp.UnixMilli(), 10))
		b.WriteByte('\n')
		if b.Len() > limit {
			return nil, ErrOutputTooLarge
		}
	}
	// Store one descriptor per metric in the same import as its samples.
	// Descriptor labels do not change the identity of existing data series.
	type descriptor struct {
		name, kind string
		keys       map[string]bool
	}
	descriptors := map[string]*descriptor{}
	for _, sample := range samples {
		if !metriccontract.ValidKind(sample.Kind) {
			continue
		}
		d := descriptors[sample.Name]
		if d == nil {
			d = &descriptor{name: sample.Name, kind: sample.Kind, keys: map[string]bool{}}
			descriptors[sample.Name] = d
		}
		for key := range sample.Labels {
			d.keys[key] = true
		}
	}
	names := make([]string, 0, len(descriptors))
	for name := range descriptors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		d := descriptors[name]
		keys := make([]string, 0, len(d.keys))
		for key := range d.keys {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		kind, id := "exporter_plugin", message.Source+"-exporter"
		if componentmetrics.IsComponent(message.Source) {
			kind, id = "component", message.Source
		}
		fmt.Fprintf(&b, `%s{source="%s",target_id="%s",producer_kind="%s",producer_id="%s",metric="%s",kind="%s",unit="%s",label_keys="%s"} 1 %d`+"\n", metriccontract.MetadataName, message.Source, message.Payload.TargetID, kind, id, d.name, d.kind, metriccontract.Unit(d.name, d.kind), strings.Join(keys, ","), message.Timestamp.UnixMilli())
		if b.Len() > limit {
			return nil, ErrOutputTooLarge
		}
	}
	return []byte(b.String()), nil
}
func escapeLabel(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
	return replacer.Replace(value)
}
