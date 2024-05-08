package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	v1 "github.com/llm-operator/inference-manager/api/v1"
)

// CreateChatCompletion creates a chat completion.
func (s *S) CreateChatCompletion(
	w http.ResponseWriter,
	req *http.Request,
	pathParams map[string]string,
) {
	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var createReq v1.CreateChatCompletionRequest
	// TODO(kenji): Use runtime.JSONPb from github.com/grpc-ecosystem/grpc-gateway/v2.
	// That one correctly handles the JSON field names of the snake case.
	if err := json.Unmarshal(reqBody, &createReq); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// TODO(kenji): Check if the specified model is available
	// for the tenant.

	// Forward the request to the Ollama server.
	baseURL := &url.URL{
		Scheme: "http",
		Host:   s.ollamaServerAddr,
	}
	requestURL := baseURL.JoinPath("/v1/chat/completions").String()
	freq, err := http.NewRequestWithContext(req.Context(), http.MethodPost, requestURL, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Copy headers.
	for k, v := range req.Header {
		for _, vv := range v {
			freq.Header.Add(k, vv)
		}
	}

	resp, err := http.DefaultClient.Do(freq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		http.Error(w, fmt.Sprintf("Status: %s", resp.Status), resp.StatusCode)
		return
	}

	// Copy headers.
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if !createReq.Stream {
		// Non streaming response. Just copy the response body.
		if _, err := io.Copy(w, resp.Body); err != nil {
			log.Printf("Failed to proxy request: %s", err)
			http.Error(w, fmt.Sprintf("Server error: %s", err), http.StatusInternalServerError)
			return
		}
		return
	}

	// Streaming response.

	// TODO(kenji): Replace this to use scanner.
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Failed to proxy request: %s", err)
		http.Error(w, fmt.Sprintf("Server error: %s", err), http.StatusInternalServerError)
		return
	}

}
