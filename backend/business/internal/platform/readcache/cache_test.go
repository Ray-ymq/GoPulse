package readcache

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

const testEpoch = "037d8739-b32f-4cfd-a84d-2701b3427370"

type fakeRedis struct {
	mu      sync.Mutex
	values  map[string]string
	expires time.Time
	reads   chan struct{}
	fail    bool
}

func (f *fakeRedis) MGet(_ context.Context, keys []string) ([]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reads != nil {
		f.reads <- struct{}{}
	}
	if f.fail {
		return nil, errors.New("Redis 不可用")
	}
	hits := make([]any, len(keys))
	for i, key := range keys {
		if value, ok := f.values[key]; ok {
			hits[i] = value
		}
	}
	return hits, nil
}

func (f *fakeRedis) SetMany(_ context.Context, values map[string]string, expires time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	maps.Copy(f.values, values)
	f.expires = expires
	return nil
}

func (f *fakeRedis) DeleteMany(_ context.Context, keys []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, key := range keys {
		delete(f.values, key)
	}
	return nil
}

func TestFetchOnlyLoadsMissesAndAcceptsZero(t *testing.T) {
	ctx := context.Background()
	redis := &fakeRedis{values: make(map[string]string)}
	cache := New(NewScope(redis, nil, time.Second, 1, nil), "test", 5*time.Second, func(_ int, value *int) bool { return value != nil })
	redis.values[cache.key(testEpoch, 1)] = "0"
	redis.values[cache.key(testEpoch, 2)] = "null"
	redis.values[cache.key("another-epoch", 3)] = "7"

	two, three := 2, 3
	source := func(_ context.Context, ids []int) (map[int]*int, error) {
		if !reflect.DeepEqual(ids, []int{2, 3}) {
			t.Fatalf("回源 ID = %v，预期为 [2 3]", ids)
		}
		return map[int]*int{2: &two, 3: &three}, nil
	}
	values, err := cache.Fetch(ctx, testEpoch, []int{1, 2, 3}, source)
	if err != nil || len(values) != 3 || *values[1] != 0 || *values[2] != 2 || *values[3] != 3 {
		t.Fatalf("缓存结果 = %v，错误 = %v", values, err)
	}
	cache.Invalidate(ctx, testEpoch, 2)
	if _, present := redis.values[cache.key(testEpoch, 2)]; present {
		t.Fatal("失效后 Redis 仍有旧值")
	}
}

func TestFetchCoalescesAnOverlappingBatchAndKeepsOriginalExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		redis := &fakeRedis{values: make(map[string]string), reads: make(chan struct{}, 3)}
		cache := New(NewScope(redis, nil, time.Second, 1, nil), "test", 5*time.Second, func(_ int, _ int) bool { return true })
		started := make(chan time.Time, 1)
		release := make(chan struct{})
		first := make(chan error, 1)
		go func() {
			_, err := cache.Fetch(ctx, testEpoch, []int{1}, func(_ context.Context, _ []int) (map[int]int, error) {
				started <- time.Now()
				<-release
				time.Sleep(20 * time.Millisecond)
				return map[int]int{1: 1}, nil
			})
			first <- err
		}()
		start := <-started
		<-redis.reads
		second := make(chan error, 1)
		go func() {
			values, err := cache.Fetch(ctx, testEpoch, []int{1}, func(_ context.Context, _ []int) (map[int]int, error) {
				return nil, errors.New("重复回源")
			})
			if err == nil && values[1] != 1 {
				err = errors.New("合并请求缺少值")
			}
			second <- err
		}()
		<-redis.reads
		synctest.Wait()
		close(release)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		if redis.expires.After(start.Add(5 * time.Second)) {
			t.Fatalf("过期时间与回源开始时间加 TTL 不一致，差值 %s", redis.expires.Sub(start.Add(5*time.Second)))
		}
	})
}

func TestFetchFallsBackWhenRedisFails(t *testing.T) {
	redis := &fakeRedis{values: make(map[string]string), fail: true}
	cache := New(NewScope(redis, nil, time.Second, 1, nil), "test", time.Second, func(_ int, _ int) bool { return true })
	value, found, err := cache.FetchOne(context.Background(), testEpoch, 1, func(context.Context) (int, bool, error) {
		return 42, true, nil
	})
	if value != 42 || !found || err != nil || len(redis.values) != 0 {
		t.Fatalf("回源结果 = %d，命中 = %t，错误 = %v", value, found, err)
	}
}
