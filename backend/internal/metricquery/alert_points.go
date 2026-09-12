package metricquery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AlertPoints uses the same bounded authenticated client, but exports original
// samples: query_range's lookback interpolation cannot prove freshness or resets.
func (c *Client) AlertPoints(ctx context.Context, metric string, labels map[string]string, from, to time.Time) ([]Point, error) {
	d, ok := definitions[metric]
	if !ok {
		return nil, errors.New("invalid metric")
	}
	expression := QueryExpression(metric)
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	expression = strings.TrimSuffix(expression, "}")
	for _, k := range keys {
		expression += "," + k + "=" + strconv.Quote(labels[k])
	}
	expression += "}"
	form := url.Values{"match[]": {expression}, "start": {formatTime(from)}, "end": {formatTime(to)}, "reduce_mem_usage": {"1"}}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(c.endpoint, "/query_range")+"/export", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.username, c.password)
	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("metrics unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maximumBodyBytes+1))
	if err != nil || len(raw) > maximumBodyBytes {
		return nil, errors.New("invalid samples")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	points := []Point{}
	seen := map[int64]float64{}
	for {
		var row struct {
			Metric     map[string]string `json:"metric"`
			Values     []float64         `json:"values"`
			Timestamps []int64           `json:"timestamps"`
		}
		err = dec.Decode(&row)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("invalid samples")
		}
		if _, _, err = validateLabels(row.Metric, d); err != nil {
			return nil, errors.New("invalid series")
		}
		if len(row.Values) != len(row.Timestamps) {
			return nil, errors.New("invalid samples")
		}
		for _, k := range keys {
			if row.Metric[k] != labels[k] {
				return nil, errors.New("invalid series")
			}
		}
		for i, ts := range row.Timestamps {
			t := time.UnixMilli(ts).UTC()
			v := row.Values[i]
			if math.IsNaN(v) || math.IsInf(v, 0) || t.Before(from) || t.After(to) {
				return nil, errors.New("invalid samples")
			}
			if old, exists := seen[ts]; exists {
				if old != v {
					return nil, errors.New("conflicting samples")
				}
				continue
			}
			seen[ts] = v
			points = append(points, Point{Timestamp: formatTime(t), Value: v})
			if len(points) > maximumPoints {
				return nil, errors.New("too many samples")
			}
		}
	}
	sort.Slice(points, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, points[i].Timestamp)
		b, _ := time.Parse(time.RFC3339Nano, points[j].Timestamp)
		return a.Before(b)
	})
	return points, nil
}
