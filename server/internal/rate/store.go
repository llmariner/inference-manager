package rate

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	// load lua script
	_ "embed"

	"github.com/go-logr/logr"
	"github.com/redis/go-redis/v9"
)

// store is the interface that must be implemented by a rate limit store.
type store interface {
	// Take takes a specified number of tokens from the given key if available.
	Take(ctx context.Context, key string, cost int) (*Result, error)
}

// Result is the result of a Take call.
type Result struct {
	// Allowed is true if the token is available.
	Allowed bool
	// Limit is the maximum number of tokens.
	Limit int
	// Remaining is the number of remaining token.
	Remaining int
	// ResetAfter is the duration until the token is available.
	RetryAfter time.Duration
	// ResetAfter is the duration until the rate limit completely resets.
	ResetAfter time.Duration
}

//go:embed gcra_ratelimit.lua
var luaScript string

// newRedisStore creates a new RedisStore.
func newRedisStore(c Config, logger logr.Logger) store {
	log := logger.WithName("redis")
	log.Info("Initializing redis store...", "interval(sec)", c.intervalSec(), "burst", c.Burst)
	return &redisStore{
		client: redis.NewClient(&redis.Options{
			Addr:     c.Redis.Address,
			Username: c.Redis.Username,
			Password: c.Redis.Password,
			DB:       c.Redis.Database,
		}),
		script:      redis.NewScript(luaScript),
		intervalSec: c.intervalSec(),
		burst:       c.Burst,
		burstOffset: c.burstOffset(),
		logger:      log,
	}
}

// redisStore is a rate limit store backed by Redis.
type redisStore struct {
	client *redis.Client
	script *redis.Script

	intervalSec float64
	burst       int
	burstOffset float64

	logger logr.Logger
}

// Take takes a specified number of tokens from the given key if available.
func (s *redisStore) Take(ctx context.Context, key string, cost int) (*Result, error) {
	res, err := s.script.Run(ctx, s.client,
		[]string{"rate:" + key},            // key
		s.intervalSec, cost, s.burstOffset, // args
	).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to run script: %s", err)
	}

	values := res.([]interface{})
	allowed, err := strconv.ParseBool(values[0].(string))
	if err != nil {
		return nil, fmt.Errorf("failed to parse allowed: %s", err)
	}
	remaining := int(values[1].(int64))
	retryAfterSec, err := strconv.ParseFloat(values[2].(string), 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse retryAfter: %s", err)
	}
	resetAfterSec, err := strconv.ParseFloat(values[3].(string), 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse resetAfter: %s", err)
	}

	r := &Result{
		Allowed:    allowed,
		Limit:      s.burst,
		Remaining:  remaining,
		RetryAfter: time.Duration(retryAfterSec * float64(time.Second)),
		ResetAfter: time.Duration(resetAfterSec * float64(time.Second)),
	}
	s.logger.V(6).Info("RateLimit", "key", key, "allowed", allowed, "remaining", remaining, "retryAfter", r.RetryAfter, "resetAfter", r.ResetAfter)
	return r, nil
}

// newMemoryStore creates a new MemoryStore.
func newMemoryStore(c Config, logger logr.Logger) store {
	log := logger.WithName("memory")
	log.Info("Initializing memory store...", "interval(sec)", c.intervalSec(), "burst", c.Burst)
	return &memoryStore{
		data:        map[string]float64{},
		interval:    c.intervalSec(),
		burst:       c.Burst,
		burstOffset: c.burstOffset(),
		logger:      log,
	}
}

// memoryStore is a rate limit store backed by memory.
type memoryStore struct {
	data map[string]float64
	mu   sync.Mutex

	interval    float64
	burst       int
	burstOffset float64

	logger logr.Logger
}

// Take takes a specified number of tokens from the given key if available.
func (s *memoryStore) Take(ctx context.Context, key string, cost int) (*Result, error) {
	allowed, remaining, retryAfter, resetAfter := s.take(key, s.interval, s.burstOffset, float64(cost))
	r := &Result{
		Allowed:    allowed,
		Limit:      s.burst,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retryAfter * float64(time.Second)),
		ResetAfter: time.Duration(resetAfter * float64(time.Second)),
	}
	s.logger.V(6).Info("RateLimit", "key", key, "allowed", allowed, "remaining", r.Remaining, "retryAfter", r.RetryAfter, "resetAfter", r.ResetAfter)
	return r, nil
}

func (s *memoryStore) take(key string, rate, burstOffset, cost float64) (bool, float64, float64, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	const baseT = 1577836800 // Jan 1, 2020 00:00:00 GMT in epoch seconds
	t := time.Now()
	sec := t.Unix()
	micro := float64(t.UnixMicro() - sec)
	now := float64(sec-baseT) + (micro / 1000000)

	tat, exists := s.data[key]
	if !exists {
		tat = now
	}

	newTat := math.Max(tat, now) + rate*cost
	allowAt := newTat - burstOffset
	diff := now - allowAt
	remaining := diff / rate
	resetAfter := math.Ceil(newTat - now)

	if remaining < 0 {
		retryAfter := diff * -1
		return false, 0, retryAfter, resetAfter
	}

	// TODO(aya): support old data cleanup
	s.data[key] = newTat
	return true, remaining, 0, resetAfter
}

// noopStore is a rate limit store that always allows the request.
type noopStore struct{}

// Take takes a specified number of tokens from the given key if available.
func (s *noopStore) Take(ctx context.Context, key string, cost int) (*Result, error) {
	return &Result{
		Allowed:    true,
		Limit:      -1,
		Remaining:  -1,
		RetryAfter: -1,
		ResetAfter: -1,
	}, nil
}
