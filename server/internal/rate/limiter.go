package rate

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-logr/logr"
)

// NewLimiter returns a new rate limiter.
func NewLimiter(c Config, logger logr.Logger) Limiter {
	log := logger.WithName("rate")
	if !c.Enable {
		log.Info("Rate limiter is disabled")
		return Limiter{store: &noopStore{}}
	}
	var s store
	switch c.StoreType {
	case storeTypeRedis:
		s = newRedisStore(c, log)
	case storeTypeMemory:
		s = newMemoryStore(c, log)
	}
	return Limiter{store: s}
}

// Limiter is a rate limiter.
type Limiter struct {
	store store
}

// Take takes a token from the given key if available.
func (l *Limiter) Take(ctx context.Context, key string) (*Result, error) {
	// TODO(aya): support inference-token limiter
	return l.store.Take(ctx, key, 1)
}

// SetRateLimitHTTPHeaders sets rate limit headers to the response.
func SetRateLimitHTTPHeaders(w http.ResponseWriter, res *Result) {
	if res.Limit == -1 {
		// rate limiter is disabled
		return
	}
	w.Header().Set("X-RateLimit-Limit-Requests", strconv.Itoa(res.Limit))
	w.Header().Set("X-RateLimit-Remaining-Requests", strconv.Itoa(res.Remaining))
	w.Header().Set("X-RateLimit-Reset-Requests", res.ResetAfter.Truncate(time.Second).String())
	if !res.Allowed {
		w.Header().Set("X-RateLimit-RetryAfter", res.RetryAfter.Truncate(time.Second).String())
	}
}
