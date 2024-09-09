package metrics

import (
	"sync"
	"time"
)

// Collector is the interface for collecting metrics.
type Collector interface {
	Add(modelID string, v float64)
}

// Provider is the interface for providing metric values.
type Provider interface {
	Get(modelID string) float64
}

// NewClient creates a new collection of metrics.
func NewClient(window time.Duration) *Client {
	// TODO(aya): revisit the window size. (e.g., use two different windows for stable & burst)
	return &Client{
		window:  window,
		metrics: make(map[string]*windowBucket),
	}
}

// Client is a collection of metrics.
type Client struct {
	window time.Duration

	metrics map[string]*windowBucket
	mu      sync.RWMutex
}

// Add adds a value to a given metric.
func (c *Client) Add(modelID string, v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	wb, ok := c.metrics[modelID]
	if !ok {
		wb = newWindowBucket(c.window)
		c.metrics[modelID] = wb
	}
	wb.add(time.Now(), v)
}

// Get returns the value of a given metric.
func (c *Client) Get(modelID string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	wb, ok := c.metrics[modelID]
	if !ok {
		return 0
	}
	return wb.average(time.Now())
}

// precision is the granularity of the window bucket.
const precision = time.Second

func newWindowBucket(window time.Duration) *windowBucket {
	window = window.Truncate(precision)
	return &windowBucket{
		buckets: make([]float64, int(window/precision)),
		window:  window,
	}
}

type windowBucket struct {
	buckets []float64

	window time.Duration

	lastIndex     int
	lastUpdatedAt time.Time
}

// add adds a value to the window bucket.
func (w *windowBucket) add(t time.Time, v float64) {
	t = t.Truncate(precision)

	if w.lastUpdatedAt.IsZero() {
		w.lastUpdatedAt = t
		w.buckets[0] = v
		return
	}

	if t.Before(w.lastUpdatedAt) {
		return
	}
	if t.Equal(w.lastUpdatedAt) {
		w.buckets[w.lastIndex] += v
		return
	}

	d := t.Sub(w.lastUpdatedAt)
	lastVal := w.buckets[w.lastIndex]
	bucketNum := len(w.buckets)

	if d > w.window {
		// If the last update was more than the window, update all the buckets
		// with the last value except the first one (current value).
		w.lastIndex = 0
		w.lastUpdatedAt = t
		w.buckets[0] = lastVal + v

		for i := 1; i < bucketNum; i++ {
			w.buckets[i] = lastVal
		}
		return
	}

	// If the last update was less than the window, update the buckets between
	// the last index and the new index with the last value, then set the new value.
	idx := (w.lastIndex + int(d/precision)) % bucketNum
	if idx > w.lastIndex {
		for i := w.lastIndex + 1; i < idx; i++ {
			w.buckets[i] = lastVal
		}
	} else {
		for i := w.lastIndex + 1; i < bucketNum; i++ {
			w.buckets[i] = lastVal
		}
		for i := 0; i < idx; i++ {
			w.buckets[i] = lastVal
		}
	}
	w.lastIndex = idx
	w.lastUpdatedAt = t
	w.buckets[idx] = lastVal + v
}

// Average returns the average of the values in the window before the given time.
func (w *windowBucket) average(t time.Time) float64 {
	t = t.Truncate(precision)
	if w.lastUpdatedAt.IsZero() {
		return 0
	}

	winStartAt := t.Add(-w.window)
	if w.lastUpdatedAt.Equal(winStartAt) || w.lastUpdatedAt.Before(winStartAt) {
		// the value in the window are the same, so the average is the last value.
		return w.buckets[w.lastIndex]
	}

	bucketNum := len(w.buckets)
	lastVal := w.buckets[w.lastIndex]
	d := t.Sub(w.lastUpdatedAt) / precision
	idx := (w.lastIndex + int(d)) % bucketNum

	// calculating the sum from the last update to time t.
	sum := lastVal * float64(d)
	// get the sum from the start of the window to the last update.
	if idx < w.lastIndex {
		for i := idx; i < w.lastIndex; i++ {
			sum += w.buckets[i]
		}
	} else {
		for i := idx; i < bucketNum; i++ {
			sum += w.buckets[i]
		}
		for i := 0; i < w.lastIndex; i++ {
			sum += w.buckets[i]
		}
	}
	return sum / float64(bucketNum)
}
