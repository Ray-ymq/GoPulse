// Package spool owns Monitor's durable Kafka hand-off. A queue is deliberately
// scoped to one message type so a full metrics cache cannot evict logs or block
// event delivery.
package spool

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/publisher"
	"github.com/Ray-ymq/GoPulse/runtimekit"
	bolt "go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"
)

const (
	GiB                 int64 = 1 << 30
	DefaultCapacity           = GiB
	messageBudgetRatio        = 75
	defaultRetryMin           = 250 * time.Millisecond
	defaultRetryMax           = 5 * time.Second
	minimumDatabaseSize       = 1 << 20
)

type Kind string

const (
	Metrics Kind = "metrics"
	Logs    Kind = "logs"
	Events  Kind = "events"
)

func (k Kind) valid() bool { return k == Metrics || k == Logs || k == Events }

type Config struct {
	Dir            string
	MetricsMaxSize int64
	LogsMaxSize    int64
	EventsMaxSize  int64
	RetryMin       time.Duration
	RetryMax       time.Duration
	Sender         publisher.Sender
	Now            func() time.Time
	Jitter         func(time.Duration) time.Duration
	Logger         *slog.Logger
}

type Entry struct {
	Sequence   uint64    `json:"sequence"`
	MessageID  string    `json:"message_id"`
	Type       string    `json:"type"`
	Body       []byte    `json:"body"`
	EnqueuedAt time.Time `json:"enqueued_at"`
	New        bool      `json:"new"`
}

type QueueStats struct {
	Kind          Kind   `json:"kind"`
	Pending       int64  `json:"pending"`
	OccupiedBytes int64  `json:"occupied_bytes"`
	CapacityBytes int64  `json:"capacity_bytes"`
	FileBytes     int64  `json:"file_bytes"`
	Enqueued      uint64 `json:"enqueued"`
	Sent          uint64 `json:"sent"`
	Failed        uint64 `json:"failed"`
	Evicted       uint64 `json:"evicted"`
	WriteBlocked  bool   `json:"write_blocked"`
	ReasonCode    string `json:"reason_code"`
}

type Status struct {
	Component  string       `json:"component"`
	Instance   string       `json:"instance"`
	ObservedAt time.Time    `json:"observed_at"`
	Queues     []QueueStats `json:"queues"`
}

var (
	ErrClosed      = errors.New("spool is closed")
	ErrTooLarge    = errors.New("spool message exceeds the effective capacity")
	ErrCorrupt     = errors.New("spool database is corrupt")
	ErrNotStarted  = errors.New("spool sender is not started")
	ErrQueueBusy   = errors.New("spool queue has no evictable record")
	messageBucket  = []byte("messages")
	metadataBucket = []byte("metadata")
	keyNextSeq     = []byte("next_sequence")
	keyUsedBytes   = []byte("used_bytes")
	keyEnqueued    = []byte("enqueued")
	keySent        = []byte("sent")
	keyFailed      = []byte("failed")
	keyEvicted     = []byte("evicted")
)

type Queue struct {
	kind     Kind
	path     string
	maxSize  int64
	capacity int64
	db       *bolt.DB
	mu       sync.Mutex
	closed   bool
	blocked  string
	inFlight map[uint64]struct{}
}

func openQueue(dir string, kind Kind, maxSize int64) (*Queue, error) {
	if !kind.valid() {
		return nil, errors.New("invalid spool kind")
	}
	if maxSize <= 0 {
		maxSize = DefaultCapacity
	}
	if maxSize < minimumDatabaseSize {
		return nil, fmt.Errorf("spool max size must be at least %d bytes", minimumDatabaseSize)
	}
	path := filepath.Join(dir, string(kind)+".db")
	db, err := bolt.Open(path, 0600, &bolt.Options{MaxSize: int(maxSize), NoSync: false})
	if err != nil {
		// Never remove or recreate a failed database. A corrupt or unwritable
		// file must remain available for operator recovery.
		return nil, fmt.Errorf("open %s spool: %w", kind, err)
	}
	q := &Queue{kind: kind, path: path, maxSize: maxSize, capacity: maxSize * messageBudgetRatio / 100, db: db, inFlight: make(map[uint64]struct{})}
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(messageBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(metadataBucket); err != nil {
			return err
		}
		meta := tx.Bucket(metadataBucket)
		if meta.Get(keyNextSeq) == nil {
			if err := meta.Put(keyNextSeq, u64(1)); err != nil {
				return err
			}
		}
		// Entries left by a previous process are old work after restart. This
		// transaction deliberately keeps the records and only changes the
		// scheduling marker; a crash never turns the queue into a new empty DB.
		messages := tx.Bucket(messageBucket)
		cursor := messages.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			var entry Entry
			if err := json.Unmarshal(value, &entry); err != nil {
				return fmt.Errorf("%w: decode %s sequence %x", ErrCorrupt, kind, key)
			}
			if !entry.New {
				continue
			}
			entry.New = false
			encoded, err := json.Marshal(entry)
			if err != nil {
				return err
			}
			if err := messages.Put(key, encoded); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize %s spool: %w", kind, err)
	}
	return q, nil
}

func (q *Queue) close() error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true
	q.mu.Unlock()
	return q.db.Close()
}

func (q *Queue) enqueue(message publisher.Message, now time.Time) (Entry, error) {
	if len(message.Body) == 0 || message.ID == "" || message.Type == "" {
		return Entry{}, errors.New("invalid spool message")
	}
	entry := Entry{MessageID: message.ID, Type: message.Type, Body: append([]byte(nil), message.Body...), EnqueuedAt: now.UTC(), New: true}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return Entry{}, ErrClosed
	}
	err := q.db.Update(func(tx *bolt.Tx) error {
		messages := tx.Bucket(messageBucket)
		meta := tx.Bucket(metadataBucket)
		sequence := readU64(meta.Get(keyNextSeq))
		if sequence == 0 {
			sequence = 1
		}
		entry.Sequence = sequence
		encoded, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		entryBytes := int64(len(encoded) + 16)
		if entryBytes > q.capacity {
			return ErrTooLarge
		}
		used := readU64(meta.Get(keyUsedBytes))
		for used+uint64(entryBytes) > uint64(q.capacity) {
			cursor := messages.Cursor()
			var key, value []byte
			for key, value = cursor.First(); key != nil; key, value = cursor.Next() {
				if _, ok := q.inFlight[readU64(key)]; ok {
					continue
				}
				break
			}
			if key == nil {
				return ErrQueueBusy
			}
			oldSize := int64(len(value) + 16)
			if err := messages.Delete(key); err != nil {
				return err
			}
			if oldSize > 0 && uint64(oldSize) <= used {
				used -= uint64(oldSize)
			} else {
				used = 0
			}
			if err := increment(meta, keyEvicted); err != nil {
				return err
			}
		}
		if used+uint64(entryBytes) > uint64(q.capacity) {
			return ErrTooLarge
		}
		if err := messages.Put(u64(sequence), encoded); err != nil {
			return err
		}
		if err := meta.Put(keyNextSeq, u64(sequence+1)); err != nil {
			return err
		}
		if err := meta.Put(keyUsedBytes, u64(used+uint64(len(encoded)+16))); err != nil {
			return err
		}
		return increment(meta, keyEnqueued)
	})
	if err != nil {
		q.blocked = classifyWriteError(err)
		return Entry{}, err
	}
	q.blocked = ""
	return entry, nil
}

func (q *Queue) next(preferNew bool) (Entry, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return Entry{}, false, ErrClosed
	}
	return q.find(preferNew)
}

func (q *Queue) claim(preferNew bool) (Entry, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return Entry{}, false, ErrClosed
	}
	entry, found, err := q.find(preferNew)
	if err == nil && found {
		q.inFlight[entry.Sequence] = struct{}{}
	}
	return entry, found, err
}

func (q *Queue) find(preferNew bool) (Entry, bool, error) {
	var result Entry
	var found bool
	err := q.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(messageBucket)
		if preferNew {
			cursor := bucket.Cursor()
			for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
				if _, ok := q.inFlight[readU64(key)]; ok {
					continue
				}
				var candidate Entry
				if err := json.Unmarshal(value, &candidate); err != nil {
					return fmt.Errorf("%w: decode %s sequence %x", ErrCorrupt, q.kind, key)
				}
				if candidate.New {
					result, found = candidate, true
					return nil
				}
			}
			return nil
		}
		cursor := bucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			if _, ok := q.inFlight[readU64(key)]; ok {
				continue
			}
			if err := json.Unmarshal(value, &result); err != nil {
				return fmt.Errorf("%w: decode %s entry", ErrCorrupt, q.kind)
			}
			found = true
			return nil
		}
		return nil
	})
	return result, found, err
}

func (q *Queue) delete(entry Entry, sent bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrClosed
	}
	err := q.db.Update(func(tx *bolt.Tx) error {
		messages := tx.Bucket(messageBucket)
		meta := tx.Bucket(metadataBucket)
		key := u64(entry.Sequence)
		value := messages.Get(key)
		if value == nil {
			return nil
		}
		var current Entry
		if err := json.Unmarshal(value, &current); err != nil {
			return fmt.Errorf("%w: decode %s entry", ErrCorrupt, q.kind)
		}
		if current.MessageID != entry.MessageID {
			return nil
		}
		if err := messages.Delete(key); err != nil {
			return err
		}
		used := readU64(meta.Get(keyUsedBytes))
		entrySize := uint64(len(value) + 16)
		if entrySize >= used {
			used = 0
		} else {
			used -= entrySize
		}
		if err := meta.Put(keyUsedBytes, u64(used)); err != nil {
			return err
		}
		if sent {
			return increment(meta, keySent)
		}
		return nil
	})
	if err != nil {
		q.blocked = classifyWriteError(err)
	} else {
		delete(q.inFlight, entry.Sequence)
		q.blocked = ""
	}
	return err
}

func (q *Queue) release(entry Entry) {
	q.mu.Lock()
	delete(q.inFlight, entry.Sequence)
	q.mu.Unlock()
}

func (q *Queue) markFailed() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrClosed
	}
	err := q.db.Update(func(tx *bolt.Tx) error { return increment(tx.Bucket(metadataBucket), keyFailed) })
	if err != nil {
		q.blocked = classifyWriteError(err)
	} else {
		q.blocked = ""
	}
	return err
}

func (q *Queue) stats() (QueueStats, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return QueueStats{}, ErrClosed
	}
	var stats QueueStats
	stats.Kind, stats.CapacityBytes, stats.WriteBlocked, stats.ReasonCode = q.kind, q.capacity, q.blocked != "", q.blocked
	if info, err := os.Stat(q.path); err == nil {
		stats.FileBytes = info.Size()
	}
	err := q.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(metadataBucket)
		bucket := tx.Bucket(messageBucket)
		stats.Pending = int64(bucket.Stats().KeyN)
		stats.OccupiedBytes = int64(readU64(meta.Get(keyUsedBytes)))
		stats.Enqueued = readU64(meta.Get(keyEnqueued))
		stats.Sent = readU64(meta.Get(keySent))
		stats.Failed = readU64(meta.Get(keyFailed))
		stats.Evicted = readU64(meta.Get(keyEvicted))
		return nil
	})
	return stats, err
}

func (q *Queue) pathName() string { return q.path }

type Manager struct {
	mu                 sync.RWMutex
	queues             map[Kind]*Queue
	sender             publisher.Sender
	now                func() time.Time
	jitter             func(time.Duration) time.Duration
	retryMin, retryMax time.Duration
	logger             *slog.Logger
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	started            bool
	closed             bool
	newMu              sync.Mutex
	newSent            map[Kind]int
}

func New(cfg Config) (*Manager, error) {
	if cfg.Dir == "" {
		return nil, errors.New("spool directory is required")
	}
	if !filepath.IsAbs(cfg.Dir) {
		return nil, errors.New("spool directory must be absolute")
	}
	if cfg.Sender == nil {
		return nil, errors.New("spool sender is required")
	}
	if cfg.RetryMin <= 0 {
		cfg.RetryMin = defaultRetryMin
	}
	if cfg.RetryMax < cfg.RetryMin {
		cfg.RetryMax = defaultRetryMax
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Jitter == nil {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		var rngMu sync.Mutex
		cfg.Jitter = func(d time.Duration) time.Duration {
			if d <= 0 {
				return 0
			}
			rngMu.Lock()
			defer rngMu.Unlock()
			return time.Duration(rng.Int63n(int64(d/2) + 1))
		}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if err := os.MkdirAll(cfg.Dir, 0750); err != nil {
		return nil, fmt.Errorf("create spool directory: %w", err)
	}
	limits := map[Kind]int64{Metrics: cfg.MetricsMaxSize, Logs: cfg.LogsMaxSize, Events: cfg.EventsMaxSize}
	queues := make(map[Kind]*Queue, 3)
	for _, kind := range []Kind{Metrics, Logs, Events} {
		q, err := openQueue(cfg.Dir, kind, limits[kind])
		if err != nil {
			for _, opened := range queues {
				_ = opened.close()
			}
			return nil, err
		}
		queues[kind] = q
	}
	return &Manager{queues: queues, sender: cfg.Sender, now: cfg.Now, jitter: cfg.Jitter, retryMin: cfg.RetryMin, retryMax: cfg.RetryMax, logger: cfg.Logger, newSent: make(map[Kind]int)}, nil
}

func (m *Manager) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("spool context is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.started {
		return nil
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true
	for _, kind := range []Kind{Metrics, Logs, Events} {
		m.wg.Add(1)
		go m.run(kind)
	}
	return nil
}

func (m *Manager) Publish(ctx context.Context, message envelope.Envelope) error {
	if message.Type != "metrics" {
		return &publisher.PermanentError{Code: "unsupported_envelope"}
	}
	body, err := json.Marshal(message)
	if err != nil {
		return &publisher.PermanentError{Code: "serialization_failed"}
	}
	return m.enqueue(ctx, publisher.Message{ID: message.MessageID, Type: message.Type, Body: body})
}

func (m *Manager) PublishRaw(ctx context.Context, messageID string, value any) error {
	message, err := publisher.MessageFromRaw(messageID, value)
	if err != nil {
		return err
	}
	return m.enqueue(ctx, message)
}

// EnqueueRaw lets the Monitor event recorder report success immediately after
// the durable local transaction, without adding a second in-memory queue.
func (m *Manager) EnqueueRaw(ctx context.Context, messageID string, value any) error {
	return m.PublishRaw(ctx, messageID, value)
}

func (m *Manager) enqueue(ctx context.Context, message publisher.Message) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	kind := Kind(message.Type)
	q := m.queues[kind]
	if q == nil {
		return &publisher.PermanentError{Code: "unsupported_envelope"}
	}
	m.mu.RLock()
	closed := m.closed
	m.mu.RUnlock()
	if closed {
		return ErrClosed
	}
	_, err := q.enqueue(message, m.now())
	return err
}

func (m *Manager) run(kind Kind) {
	defer m.wg.Done()
	q := m.queues[kind]
	backoff := m.retryMin
	for {
		if m.ctx.Err() != nil {
			return
		}
		preferNew := m.chooseNew(kind)
		entry, found, err := q.claim(preferNew)
		if err != nil {
			m.logger.Error("spool read failed", "module", "delivery", "event", "spool_read_failed", "kind", string(kind), "reason", "spool_corrupt")
			return
		}
		if !found {
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		err = m.sender.Send(m.ctx, publisher.Message{ID: entry.MessageID, Type: entry.Type, Body: entry.Body})
		if err == nil || isPermanent(err) {
			if err != nil {
				m.logger.Warn("spool record discarded", "module", "delivery", "event", "record_discarded", "kind", string(kind), "reason", safeErrorCode(err))
			}
			if deleteErr := q.delete(entry, err == nil); deleteErr != nil {
				m.logger.Error("spool acknowledgement cleanup failed", "module", "delivery", "event", "spool_write_failed", "kind", string(kind), "reason", "disk_write_failed")
				q.release(entry)
				if errors.Is(deleteErr, ErrCorrupt) {
					return
				}
				_ = q.markFailed()
				select {
				case <-m.ctx.Done():
					return
				case <-time.After(backoff + m.jitter(backoff)):
				}
				if backoff < m.retryMax {
					backoff *= 2
					if backoff > m.retryMax {
						backoff = m.retryMax
					}
				}
				continue
			}
			backoff = m.retryMin
			continue
		}
		q.release(entry)
		_ = q.markFailed()
		select {
		case <-m.ctx.Done():
			return
		case <-time.After(backoff + m.jitter(backoff)):
		}
		if backoff < m.retryMax {
			backoff *= 2
			if backoff > m.retryMax {
				backoff = m.retryMax
			}
		}
	}
}

func (m *Manager) chooseNew(kind Kind) bool {
	stats, err := m.queues[kind].stats()
	if err != nil || stats.Pending == 0 {
		return false
	}
	// The queue's first record is old/new; inspect it without mutating it.
	newEntry, hasNew, err := m.queues[kind].next(true)
	if err != nil || !hasNew {
		return false
	}
	oldEntry, hasOld, err := m.queues[kind].next(false)
	if err != nil {
		return false
	}
	if !hasOld || oldEntry.Sequence == newEntry.Sequence {
		return true
	}
	m.newMu.Lock()
	defer m.newMu.Unlock()
	if m.newSent[kind] >= 3 {
		m.newSent[kind] = 0
		return false
	}
	m.newSent[kind]++
	return true
}

func (m *Manager) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		if m.cancel != nil {
			m.cancel()
		}
	}
	started := m.started
	m.mu.Unlock()
	if started {
		done := make(chan struct{})
		go func() { m.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	var first error
	for _, q := range m.queues {
		if err := q.close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (m *Manager) Status() Status {
	now := m.now().UTC()
	result := Status{Component: "monitor", Instance: runtimekit.InstanceID("monitor"), ObservedAt: now, Queues: make([]QueueStats, 0, 3)}
	for _, kind := range []Kind{Metrics, Logs, Events} {
		stats, err := m.queues[kind].stats()
		if err != nil {
			stats = QueueStats{Kind: kind, CapacityBytes: m.queues[kind].capacity, WriteBlocked: true, ReasonCode: "spool_unavailable"}
		}
		result.Queues = append(result.Queues, stats)
	}
	return result
}

func (m *Manager) Path(kind Kind) string {
	if q := m.queues[kind]; q != nil {
		return q.pathName()
	}
	return ""
}

func increment(bucket *bolt.Bucket, key []byte) error {
	value := readU64(bucket.Get(key))
	return bucket.Put(key, u64(value+1))
}
func u64(value uint64) []byte {
	buffer := make([]byte, 8)
	binary.BigEndian.PutUint64(buffer, value)
	return buffer
}
func readU64(value []byte) uint64 {
	if len(value) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}
func classifyWriteError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrTooLarge) {
		return "message_too_large"
	}
	if errors.Is(err, ErrQueueBusy) {
		return "queue_busy"
	}
	if errors.Is(err, berrors.ErrDatabaseNotOpen) || errors.Is(err, berrors.ErrInvalid) {
		return "spool_corrupt"
	}
	if errors.Is(err, berrors.ErrMaxSizeReached) || errors.Is(err, syscall.ENOSPC) {
		return "disk_full"
	}
	return "disk_write_failed"
}
func isPermanent(err error) bool {
	var target interface{ Permanent() bool }
	return errors.As(err, &target) && target.Permanent()
}
func safeErrorCode(err error) string {
	var permanentErr *publisher.PermanentError
	if errors.As(err, &permanentErr) && permanentErr.Code != "" {
		return permanentErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "transport_failed"
}
