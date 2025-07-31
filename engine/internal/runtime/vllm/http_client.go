package vllm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Model is a struct that represents a model.
// This mostly follows the OpenAI API format but havs extra field like "parent".
type Model struct {
	ID string `json:"id"`
	// Parent is set to a base model ID if the model is a LoRA adapter..
	Parent *string `json:"parent"`
}

// ListModelsResponse is a struct that represents the response from the list models endpoint.
type ListModelsResponse struct {
	Data []*Model `json:"data"`
}

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

	resp, err := c.sendHTTPRequest(ctx, "POST", "/v1/load_lora_adapter", data)
	if err != nil {
		return -1, fmt.Errorf("send request: %s", err)
	}

	if err := resp.Body.Close(); err != nil {
		return -1, fmt.Errorf("close response body: %s", err)
	}

	return resp.StatusCode, nil
}

// UnloadLoRAAdapter unnloads a LoRA adapter from the given path.
func (c *HTTPClient) UnloadLoRAAdapter(ctx context.Context, loraName string) (int, error) {
	type request struct {
		LoRAName string `json:"lora_name"`
	}

	req := request{
		LoRAName: loraName,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return -1, fmt.Errorf("marshal request: %s", err)
	}

	resp, err := c.sendHTTPRequest(ctx, "POST", "/v1/unload_lora_adapter", data)
	if err != nil {
		return -1, fmt.Errorf("send request: %s", err)
	}

	if err := resp.Body.Close(); err != nil {
		return -1, fmt.Errorf("close response body: %s", err)
	}

	return resp.StatusCode, nil
}

// ListModels lists all models.
func (c *HTTPClient) ListModels(ctx context.Context) (*ListModelsResponse, error) {
	resp, err := c.sendHTTPRequest(ctx, "GET", "/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("send request: %s", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var mresp ListModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mresp); err != nil {
		return nil, fmt.Errorf("decode response: %s", err)
	}

	return &mresp, nil
}

func (c *HTTPClient) sendHTTPRequest(
	ctx context.Context,
	method string,
	path string,
	body []byte,
) (*http.Response, error) {
	url := url.URL{Scheme: "http", Host: c.addr, Path: path}
	hreq, err := http.NewRequestWithContext(ctx, method, url.String(), bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("request creation error: %s", err)
	}
	hreq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("send request: %s", err)
	}

	return resp, nil
}
