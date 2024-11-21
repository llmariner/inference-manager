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
