package metrics

import (
	"errors"
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
		b.WriteString(`{source="redis",target_id="redis-exporter-local"`)
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
	return []byte(b.String()), nil
}
func escapeLabel(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
	return replacer.Replace(value)
}
