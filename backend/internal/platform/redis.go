package platform

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	goredis "github.com/redis/go-redis/v9"
)

var ErrRedisKeyNotFound = errors.New("redis key not found")

type Redis struct {
	client         *goredis.Client
	evalSHABatcher *redisScriptBatcher
	recovery       *redisRecoveryGate
}

func NewRedis(cfg config.RedisConfig) *Redis {
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 16
	}
	poolTimeout := cfg.PoolTimeout
	if poolTimeout <= 0 {
		poolTimeout = cfg.OperationTimeout
	}
	if poolTimeout <= 0 {
		poolTimeout = time.Second
	}
	redisClient := goredis.NewClient(&goredis.Options{
		Addr:         net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     poolSize,
		PoolTimeout:  poolTimeout,
		MaxRetries:   0,
		DialTimeout:  poolTimeout,
		ReadTimeout:  poolTimeout,
		WriteTimeout: poolTimeout,
	})
	return &Redis{
		client:         redisClient,
		evalSHABatcher: newRedisScriptBatcher(redisClient),
		recovery:       newRedisRecoveryGate(defaultRedisRecoveryCooldown),
	}
}

func (client *Redis) Check(ctx context.Context) error {
	err := client.call(ctx, func() error { return client.client.Ping(ctx).Err() })
	componentmetrics.Dependency("redis", err)
	return err
}

func (client *Redis) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := client.call(ctx, func() error {
		var err error
		value, err = client.client.Get(ctx, key).Result()
		return err
	})
	observedErr := err
	if errors.Is(err, goredis.Nil) {
		observedErr = nil
	}
	componentmetrics.Dependency("redis", observedErr)
	if errors.Is(err, goredis.Nil) {
		return "", ErrRedisKeyNotFound
	}
	return value, err
}

func (client *Redis) Set(ctx context.Context, key, value string, expiration time.Duration) error {
	err := client.call(ctx, func() error { return client.client.Set(ctx, key, value, expiration).Err() })
	componentmetrics.Dependency("redis", err)
	return err
}

func (client *Redis) Delete(ctx context.Context, key string) error {
	err := client.call(ctx, func() error { return client.client.Del(ctx, key).Err() })
	componentmetrics.Dependency("redis", err)
	return err
}

// Eval executes one bounded Lua script. Higher-level adapters own the script
// text and key contract; exposing only Eval keeps the platform client small
// while allowing shared limiters to use Redis TIME atomically.
func (client *Redis) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	var value any
	err := client.call(ctx, func() error {
		var err error
		value, err = client.client.Eval(ctx, script, keys, args...).Result()
		return err
	})
	componentmetrics.Dependency("redis", err)
	return value, err
}

func (client *Redis) EvalSha(ctx context.Context, sha string, keys []string, args ...any) (any, error) {
	var value any
	err := client.call(ctx, func() error {
		var err error
		if client.evalSHABatcher != nil {
			value, err = client.evalSHABatcher.submit(ctx, sha, keys, args)
		} else {
			value, err = client.client.EvalSha(ctx, sha, keys, args...).Result()
		}
		return err
	})
	componentmetrics.Dependency("redis", err)
	return value, err
}

func (client *Redis) ScriptLoad(ctx context.Context, script string) (string, error) {
	var value string
	err := client.call(ctx, func() error {
		var err error
		value, err = client.client.ScriptLoad(ctx, script).Result()
		return err
	})
	componentmetrics.Dependency("redis", err)
	return value, err
}

func (client *Redis) TTL(ctx context.Context, key string) (time.Duration, error) {
	var value time.Duration
	err := client.call(ctx, func() error {
		var err error
		value, err = client.client.TTL(ctx, key).Result()
		return err
	})
	componentmetrics.Dependency("redis", err)
	return value, err
}

func (client *Redis) call(ctx context.Context, operation func() error) error {
	if client.recovery == nil {
		return operation()
	}
	return client.recovery.call(ctx, operation)
}

func (client *Redis) Close() error {
	if client.evalSHABatcher != nil {
		client.evalSHABatcher.close()
	}
	return client.client.Close()
}

const (
	redisScriptBatchWindow = 50 * time.Microsecond
	redisScriptBatchMax    = 32
	redisScriptBatchQueue  = 256
)

type redisScriptBatchJob struct {
	ctx    context.Context
	sha    string
	keys   []string
	args   []any
	result chan redisScriptBatchResult
}

type redisScriptBatchResult struct {
	value any
	err   error
}

// redisScriptBatcher only batches independent EVALSHA commands. Redis still
// executes every command atomically and returns one result per request; the
// batch merely removes repeated socket write/read cycles when a protected
// request burst reaches one Backend process.
type redisScriptBatcher struct {
	client *goredis.Client
	queue  chan redisScriptBatchJob
	stop   chan struct{}
	done   chan struct{}
	mu     sync.RWMutex
	closed bool
}

func newRedisScriptBatcher(client *goredis.Client) *redisScriptBatcher {
	batcher := &redisScriptBatcher{
		client: client,
		queue:  make(chan redisScriptBatchJob, redisScriptBatchQueue),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go batcher.run()
	return batcher
}

func (batcher *redisScriptBatcher) submit(ctx context.Context, sha string, keys []string, args []any) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	job := redisScriptBatchJob{
		ctx:    ctx,
		sha:    sha,
		keys:   append([]string(nil), keys...),
		args:   append([]any(nil), args...),
		result: make(chan redisScriptBatchResult, 1),
	}
	batcher.mu.RLock()
	if batcher.closed {
		batcher.mu.RUnlock()
		return nil, errors.New("redis script batcher is closed")
	}
	select {
	case batcher.queue <- job:
		batcher.mu.RUnlock()
	case <-ctx.Done():
		batcher.mu.RUnlock()
		return nil, ctx.Err()
	case <-batcher.stop:
		batcher.mu.RUnlock()
		return nil, errors.New("redis script batcher is closed")
	}
	select {
	case result := <-job.result:
		return result.value, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (batcher *redisScriptBatcher) run() {
	defer close(batcher.done)
	for {
		var first redisScriptBatchJob
		select {
		case first = <-batcher.queue:
		case <-batcher.stop:
			batcher.failQueued()
			return
		}
		batch := []redisScriptBatchJob{first}
		timer := time.NewTimer(redisScriptBatchWindow)
	collect:
		for len(batch) < redisScriptBatchMax {
			select {
			case job := <-batcher.queue:
				batch = append(batch, job)
			case <-timer.C:
				break collect
			case <-batcher.stop:
				break collect
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		batcher.execute(batch)
	}
}

func (batcher *redisScriptBatcher) execute(batch []redisScriptBatchJob) {
	active := make([]redisScriptBatchJob, 0, len(batch))
	for _, job := range batch {
		if err := job.ctx.Err(); err != nil {
			job.result <- redisScriptBatchResult{err: err}
			continue
		}
		active = append(active, job)
	}
	if len(active) == 0 {
		return
	}
	operationContext, cancel := redisScriptBatchContext(active)
	defer cancel()
	pipeline := batcher.client.Pipeline()
	commands := make([]*goredis.Cmd, len(active))
	for index, job := range active {
		commands[index] = pipeline.EvalSha(operationContext, job.sha, job.keys, job.args...)
	}
	_, execErr := pipeline.Exec(operationContext)
	for index, job := range active {
		err := commands[index].Err()
		if err == nil {
			err = execErr
		}
		job.result <- redisScriptBatchResult{value: commands[index].Val(), err: err}
	}
}

func redisScriptBatchContext(batch []redisScriptBatchJob) (context.Context, context.CancelFunc) {
	var deadline time.Time
	for _, job := range batch {
		if candidate, ok := job.ctx.Deadline(); ok && (deadline.IsZero() || candidate.Before(deadline)) {
			deadline = candidate
		}
	}
	if deadline.IsZero() {
		return context.WithCancel(context.Background())
	}
	return context.WithDeadline(context.Background(), deadline)
}

func (batcher *redisScriptBatcher) failQueued() {
	for {
		select {
		case job := <-batcher.queue:
			job.result <- redisScriptBatchResult{err: errors.New("redis script batcher is closed")}
		default:
			return
		}
	}
}

func (batcher *redisScriptBatcher) close() {
	batcher.mu.Lock()
	if !batcher.closed {
		batcher.closed = true
		close(batcher.stop)
	}
	batcher.mu.Unlock()
	<-batcher.done
}
