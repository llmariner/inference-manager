package metrics

import (
	"sync"
)

// NewClient creates a new collection of metrics.
func NewClient() *Client {
	return &Client{
		metrics: make(map[string]float64),
	}
}

// Client is a collection of metrics.
type Client struct {
	// TODO: rethink collection method
	// This client collects simple counter values for now
	// it might be nice to return average value in a window for scaling stability.
	metrics map[string]float64
	mu      sync.RWMutex
}

// Add adds a value to a given metric.
func (c *Client) Add(modelID string, v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ci := c.metrics[modelID]
	c.metrics[modelID] = ci + v
}

// Get returns the value of a given metric.
func (c *Client) Get(modelID string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metrics[modelID]
}
