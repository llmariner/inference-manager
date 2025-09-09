package httputil

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// SendHTTPRequestWithRetry sends a request with retry logic.
func SendHTTPRequestWithRetry(
	ctx context.Context,
	url url.URL,
	httpMethod string,
	data []byte,
	retry func(status int, err error) (bool, error),
	reqTimeout,
	retryInterval time.Duration,
	retryCount int,
) error {
	for attempt := 1; retryCount < 0 || attempt <= retryCount; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, reqTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, httpMethod, url.String(), bytes.NewBuffer(data))
		if err != nil {
			return fmt.Errorf("request creation error: %s", err)
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{}
		resp, err := client.Do(req)
		var status int
		if err == nil {
			if err := resp.Body.Close(); err != nil {
				return fmt.Errorf("failed to close response body: %s", err)
			}
			status = resp.StatusCode
		}
		if ok, err := retry(status, err); err != nil {
			return err
		} else if !ok {
			return nil
		}
		time.Sleep(retryInterval)
	}
	return fmt.Errorf("retry count exceeded")
}
