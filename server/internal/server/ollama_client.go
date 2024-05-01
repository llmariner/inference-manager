package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type client struct {
	base *url.URL
	http *http.Client
}

func newClient(ollamaServerAddr string) *client {
	return &client{
		base: &url.URL{
			Scheme: "http",
			Host:   ollamaServerAddr,
		},
		http: http.DefaultClient,
	}
}

func (c *client) sendRequest(ctx context.Context, method, path string, data any) ([]byte, error) {
	var buf *bytes.Buffer
	bts, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	buf = bytes.NewBuffer(bts)

	requestURL := c.base.JoinPath(path)
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), buf)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/x-ndjson")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Printf("Failed to close the response body: %s\n", err)
		}
	}()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusBadRequest {
		log.Printf("Received an error response: %+v\n", response)
		return nil, status.Errorf(codes.Code(response.StatusCode), "Status: %s", response.Status)
	}

	resp, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
