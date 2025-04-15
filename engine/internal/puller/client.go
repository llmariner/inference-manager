package puller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/llmariner/inference-manager/engine/internal/httputil"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	requestTimeout = 3 * time.Second
	retryInterval  = 2 * time.Second
)

// NewClient creates a new puller client.
func NewClient(addr string) *Client {
	return &Client{
		addr: addr,
	}
}

// Client is a client for the puller server.
type Client struct {
	addr string
}

// PullModel sends a request to the puller server to pull a model.
func (c *Client) PullModel(ctx context.Context, modelID string) error {
	log := ctrl.LoggerFrom(ctx)
	pullURL := url.URL{Scheme: "http", Host: c.addr, Path: "/pull"}

	req := &pullModelRequest{
		ModelID: modelID,
	}
	pullData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal pull model request: %s", err)
	}

	// TODO(kenji): Consider movinng the retry logic to a caller.
	if err := httputil.SendHTTPRequestWithRetry(ctx, pullURL, pullData, func(status int, err error) (bool, error) {
		if err != nil {
			log.V(2).Error(err, "Failed to pull model", "url", pullURL, "retry-interval", retryInterval)
			return true, nil
		}
		if status != http.StatusAccepted {
			return false, fmt.Errorf("unexpected status code: %d", status)
		}
		return false, nil
	}, requestTimeout, retryInterval, 3); err != nil {
		return fmt.Errorf("failed to pull model: %s", err)
	}

	return nil
}

// GetModel sends a request to the puller server to get the status of a model.
func (c *Client) GetModel(ctx context.Context, modelID string) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	getURL := url.URL{
		Scheme: "http",
		Host:   c.addr,
		Path:   fmt.Sprintf("/models/%s", modelID),
	}
	req, err := http.NewRequestWithContext(reqCtx, "GET", getURL.String(), bytes.NewBuffer(nil))
	if err != nil {
		return -1, fmt.Errorf("request creation error: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return -1, fmt.Errorf("request error: %s", err)
	}

	if err := resp.Body.Close(); err != nil {
		return -1, fmt.Errorf("close response body: %s", err)
	}

	return resp.StatusCode, nil
}
