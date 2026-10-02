// Package retention owns the small, explicit lifecycle contract used by the
// Marshaller's observation stores.  It deliberately deals in UTC calendar
// dates rather than durations so a retention period has the same meaning at
// every instant of a UTC day.
package retention

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DateLayout  = "2006.01.02"
	MinDays     = 1
	MaxDays     = 90
	DefaultDays = 7
)

type Stream string

const (
	Logs   Stream = "logs"
	Events Stream = "events"
)

type Policy struct {
	Stream        Stream
	Prefix        string
	Alias         string
	RetentionDays int
	MinDays       int
	MaxDays       int
	LegacyIndices map[string]bool
}

type Config struct {
	Logs            Policy
	Events          Policy
	Cycle           time.Duration
	BatchIndices    int
	RequestTimeout  time.Duration
	RoundTimeout    time.Duration
	RetryMin        time.Duration
	RetryMax        time.Duration
	MaxRetries      int
	CatchupDeadline time.Duration
}

func DefaultConfig() Config {
	return Config{
		Logs:   Policy{Stream: Logs, Prefix: "gopulse-logs-v1-", Alias: "gopulse-logs-v1-read", RetentionDays: DefaultDays, MinDays: MinDays, MaxDays: MaxDays},
		Events: Policy{Stream: Events, Prefix: "gopulse-events-v1-", Alias: "gopulse-events-v1-read", RetentionDays: DefaultDays, MinDays: MinDays, MaxDays: MaxDays},
		Cycle:  60 * time.Second, BatchIndices: 16, RequestTimeout: 3 * time.Second,
		RoundTimeout: 15 * time.Second, RetryMin: 250 * time.Millisecond, RetryMax: 5 * time.Second,
		MaxRetries: 3, CatchupDeadline: 60 * time.Second,
	}
}

func (p Policy) Validate() error {
	if p.Stream != Logs && p.Stream != Events {
		return errors.New("retention stream must be logs or events")
	}
	if p.Prefix == "" || p.Alias == "" || p.RetentionDays < p.MinDays || p.RetentionDays > p.MaxDays || p.MinDays < MinDays || p.MaxDays > MaxDays || p.MinDays > p.MaxDays {
		return errors.New("invalid retention policy")
	}
	if p.Stream == Logs && (p.Prefix != "gopulse-logs-v1-" || p.Alias != "gopulse-logs-v1-read") {
		return errors.New("logs retention identity is fixed")
	}
	if p.Stream == Events && (p.Prefix != "gopulse-events-v1-" || p.Alias != "gopulse-events-v1-read") {
		return errors.New("events retention identity is fixed")
	}
	return nil
}

func (c Config) Validate() error {
	if err := c.Logs.Validate(); err != nil {
		return err
	}
	if err := c.Events.Validate(); err != nil {
		return err
	}
	if c.Cycle < 10*time.Second || c.Cycle > 24*time.Hour || c.BatchIndices < 1 || c.BatchIndices > 128 || c.RequestTimeout < 100*time.Millisecond || c.RequestTimeout > 10*time.Second || c.RoundTimeout <= 0 || c.RoundTimeout > time.Minute || c.RetryMin < 10*time.Millisecond || c.RetryMin > 10*time.Second || c.RetryMax < 100*time.Millisecond || c.RetryMax > time.Minute || c.RetryMax < c.RetryMin || c.MaxRetries < 0 || c.MaxRetries > 8 || c.CatchupDeadline <= 0 || c.CatchupDeadline > 10*time.Minute {
		return errors.New("invalid retention lifecycle budget")
	}
	return nil
}

func UTCDate(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// Cutoff is the first date that must be retained.  A date equal to the
// cutoff is retained; only dates strictly before it are eligible for deletion.
func (p Policy) Cutoff(now time.Time) time.Time {
	return UTCDate(now).AddDate(0, 0, -(p.RetentionDays - 1))
}

func ParseIndexDate(index string, p Policy) (time.Time, error) {
	if !strings.HasPrefix(index, p.Prefix) {
		return time.Time{}, errors.New("index prefix is not owned")
	}
	suffix := strings.TrimPrefix(index, p.Prefix)
	if len(suffix) != len("2006.01.02") {
		return time.Time{}, errors.New("index date has invalid length")
	}
	date, err := time.ParseInLocation(DateLayout, suffix, time.UTC)
	if err != nil || date.Format(DateLayout) != suffix {
		return time.Time{}, errors.New("index date is invalid")
	}
	return date, nil
}

type Decision struct {
	IndexDate time.Time
	Eligible  bool
	Reason    string
}

func (p Policy) Classify(index string, now time.Time) Decision {
	date, err := ParseIndexDate(index, p)
	if err != nil {
		if strings.HasPrefix(index, p.Prefix) {
			return Decision{Reason: "invalid_date"}
		}
		return Decision{Reason: "not_owned"}
	}
	if date.Before(p.Cutoff(now)) {
		return Decision{IndexDate: date, Eligible: true, Reason: "expired"}
	}
	return Decision{IndexDate: date, Reason: "not_expired"}
}

func (p Policy) ExpiredTimestamp(timestamp, now time.Time) bool {
	return UTCDate(timestamp).Before(p.Cutoff(now))
}

func (p Policy) ValidateTimestamp(timestamp, now time.Time) error {
	if timestamp.IsZero() {
		return errors.New("retention timestamp is required")
	}
	if p.ExpiredTimestamp(timestamp, now) {
		return fmt.Errorf("%s timestamp is outside retention", p.Stream)
	}
	return nil
}

func (p Policy) IndexDate(timestamp time.Time) string { return UTCDate(timestamp).Format(DateLayout) }

func PermanentCode(stream Stream) string {
	if stream == Events {
		return "expired_event_retention"
	}
	return "expired_log_retention"
}
