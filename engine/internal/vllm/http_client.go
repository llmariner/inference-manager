package vllm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// NewHTTPClient creates a new HTTP client with the given address.
func NewHTTPClient(addr string) *HTTPClient {
	return &HTTPClient{
		addr: addr,
	}
}

// HTTPClient is a struct that represents an HTTP client.
type HTTPClient struct {
	addr string
}

// LoadLoRAAdapter loads a LoRA adapter from the given path.
func (c *HTTPClient) LoadLoRAAdapter(ctx context.Context, loraName, loraPath string) (int, error) {
	type request struct {
		LoRAName string `json:"lora_name"`
		LoRAPath string `json:"lora_path"`
	}

	req := request{
		LoRAName: loraName,
		LoRAPath: loraPath,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return -1, fmt.Errorf("marshal request: %s", err)
	}

	url := url.URL{Scheme: "http", Host: c.addr, Path: "/v1/load_lora_adapter"}
	hreq, err := http.NewRequestWithContext(ctx, "POST", url.String(), bytes.NewBuffer(data))
	if err != nil {
		return -1, fmt.Errorf("request creation error: %s", err)
	}
	hreq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(hreq)
	if err != nil {
		return -1, fmt.Errorf("send request: %s", err)
	}

	if err := resp.Body.Close(); err != nil {
		return -1, fmt.Errorf("close response body: %s", err)
	}

	return resp.StatusCode, nil
}
