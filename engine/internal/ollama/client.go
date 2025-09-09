package ollama

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	"github.com/llmariner/inference-manager/engine/internal/httputil"
)

const (
	requestTimeout = 3 * time.Second
	retryInterval  = 2 * time.Second
)

// NewClient creates a new Ollama client with the given address.
func NewClient(addr string, log logr.Logger) *Client {
	return &Client{
		addr: addr,
		log:  log,
	}
}

// Client is a struct that represents a client for the Ollama API.
type Client struct {
	addr string
	log  logr.Logger
}

// Show sends a request to the Ollama API to show the status of a model with the given ID.
func (c *Client) Show(ctx context.Context, modelID string) error {
	showURL := url.URL{Scheme: "http", Host: c.addr, Path: "/api/show"}
	data := fmt.Appendf([]byte{}, `{"model": "%s"}`, modelID)
	if err := httputil.SendHTTPRequestWithRetry(ctx, showURL, "POST", data, func(status int, err error) (bool, error) {
		if err != nil {
			c.log.V(2).Error(err, "Failed to check model status", "url", showURL, "retry-interval", retryInterval)
			return true, nil
		}
		if status != http.StatusOK {
			c.log.V(2).Info("Model is not ready yet", "status", status, "retry-interval", retryInterval)
			return true, nil
		}
		return false, nil
	}, requestTimeout, retryInterval, -1); err != nil {
		return fmt.Errorf("failed to check model: %s", err)
	}

	return nil
}

// DeleteModel deletes the model with the given ID.
func (c *Client) DeleteModel(ctx context.Context, modelID string) error {
	deleteURL := url.URL{Scheme: "http", Host: c.addr, Path: "/api/delete"}
	data := fmt.Appendf([]byte{}, `{"model": "%s"}`, modelID)

	if err := httputil.SendHTTPRequestWithRetry(ctx, deleteURL, "DELETE", data, func(status int, err error) (bool, error) {
		if err != nil {
			c.log.V(2).Error(err, "Failed to delete model", "url", deleteURL, "retry-interval", retryInterval)
			return true, nil
		}

		if status != http.StatusOK && status != http.StatusNotFound {
			return true, nil
		}
		return false, nil
	}, requestTimeout, retryInterval, -1); err != nil {
		return fmt.Errorf("failed to delete model: %s", err)
	}

	return nil
}
