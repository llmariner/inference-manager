package rate

import (
	"context"
	"testing"
	"time"

	testutl "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/stretchr/testify/assert"
)

func TestMemoryStore_Limited(t *testing.T) {
	const cost = 1
	c := Config{
		Rate:   1,
		Period: 10 * time.Second,
		Burst:  12,
	}
	st := newMemoryStore(c, testutl.NewTestLogger(t))

	for i := 0; i < c.Burst; i++ {
		res, err := st.Take(context.Background(), "key-1", cost)
		assert.NoError(t, err)
		assert.True(t, res.Allowed)
		assert.Equal(t, c.Burst-i-1, res.Remaining)
		assert.Equal(t, 0.0, res.RetryAfter.Seconds())
	}
	// no remaining burst capacity
	res, err := st.Take(context.Background(), "key-1", cost)
	assert.NoError(t, err)
	assert.False(t, res.Allowed)
	assert.Equal(t, 0, res.Remaining)
	assert.InDelta(t, c.intervalSec(), res.RetryAfter.Seconds(), 0.01, res.RetryAfter)

	// different key
	res, err = st.Take(context.Background(), "key-2", cost)
	assert.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Equal(t, c.Burst-1, res.Remaining)
	assert.Equal(t, 0.0, res.RetryAfter.Seconds())
}

func TestMemoryStore_RefillRate(t *testing.T) {
	// Tokens must refill at the configured rate measured against the wall
	// clock, including across a whole-second boundary. This guards against the
	// internal clock gaining an extra ~1s every time the Unix second ticks
	// over, which would make the limiter leak far more tokens than configured
	// (and report an inflated RetryAfter/ResetAfter).
	const cost = 1
	c := Config{
		Rate:   20,
		Period: time.Second, // intervalSec = 0.05s per token
		Burst:  30,
	}
	st := newMemoryStore(c, testutl.NewTestLogger(t))

	// Land ~400ms into a second so the sleep below crosses exactly one
	// whole-second boundary.
	target := time.Now().Truncate(time.Second).Add(time.Second + 400*time.Millisecond)
	time.Sleep(time.Until(target))

	// Exhaust the burst capacity.
	for i := 0; i < c.Burst; i++ {
		res, err := st.Take(context.Background(), "key", cost)
		assert.NoError(t, err)
		assert.True(t, res.Allowed)
	}
	res, err := st.Take(context.Background(), "key", cost)
	assert.NoError(t, err)
	assert.False(t, res.Allowed)

	// Wait 700ms of real time (crossing one second boundary). At 0.05s per
	// token that refills ~14 tokens, so the remaining count after taking one
	// must stay well below the burst capacity.
	time.Sleep(700 * time.Millisecond)
	res, err = st.Take(context.Background(), "key", cost)
	assert.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.LessOrEqual(t, res.Remaining, 20)
}

func TestMemoryStore_Refill(t *testing.T) {
	const cost = 1
	c := Config{
		Rate:   1,
		Period: 100 * time.Millisecond,
		Burst:  1,
	}
	st := newMemoryStore(c, testutl.NewTestLogger(t))

	res, err := st.Take(context.Background(), "key", cost)
	assert.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Equal(t, 0, res.Remaining)
	assert.Equal(t, 0.0, res.RetryAfter.Seconds())

	res, err = st.Take(context.Background(), "key", cost)
	assert.NoError(t, err)
	assert.False(t, res.Allowed)
	assert.Equal(t, 0, res.Remaining)

	// wait refill
	time.Sleep(res.RetryAfter)
	res, err = st.Take(context.Background(), "key", cost)
	assert.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Equal(t, 0, res.Remaining)
	assert.Equal(t, 0.0, res.RetryAfter.Seconds())
}
