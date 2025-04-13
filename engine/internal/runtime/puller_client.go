package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

type pullerClient struct {
	addr string
}

// pullModel sends a request to the puller server to pull a model.
func (c *pullerClient) pullModel(ctx context.Context, modelID string) error {
	const (
		requestTimeout = 3 * time.Second
		retryInterval  = 2 * time.Second
	)

	log := ctrl.LoggerFrom(ctx)

	pullURL := url.URL{Scheme: "http", Host: c.addr, Path: "/pull"}
	pullData := fmt.Appendf([]byte{}, `{"modelID": "%s"}`, modelID)
	if err := sendHTTPRequestWithRetry(ctx, pullURL, pullData, func(status int, err error) (bool, error) {
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
