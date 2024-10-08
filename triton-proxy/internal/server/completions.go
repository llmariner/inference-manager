package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	v1 "github.com/llmariner/inference-manager/api/v1"
)

// CreateChatCompletion creates a chat completion.
func (s *S) CreateChatCompletion(
	w http.ResponseWriter,
	req *http.Request,
	pathParams map[string]string,
) {
	var createReq v1.CreateChatCompletionRequest

	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// TODO(kenji): Use runtime.JSONPb from github.com/grpc-ecosystem/grpc-gateway/v2.
	// That one correctly handles the JSON field names of the snake case.
	if err := json.Unmarshal(reqBody, &createReq); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if createReq.Model == "" {
		http.Error(w, "Model is required", http.StatusBadRequest)
		return
	}

	s.logger.Info("Forwarding the request to Triton Inference Server", "model", createReq.Model)

	genReq := buildEnsembleGenerateRequest(&createReq)
	b, err := json.Marshal(genReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	path := s.tritonBaseURL + "/v2/models/ensemble/generate"
	hreq, err := http.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hreq.Header.Add("Content-Type", "application/json")
	hreq.Header.Add("Accept", "application/json")

	hresp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer func() { _ = hresp.Body.Close() }()

	if hresp.StatusCode < http.StatusOK || hresp.StatusCode >= http.StatusBadRequest {
		body, err := io.ReadAll(hresp.Body)
		if err != nil {
			// Gracefully handle the error.
			s.logger.Error(err, "Failed to read the body")
		}
		s.logger.Info("Received an error response", "code", hresp.StatusCode, "status", hresp.Status, "body", string(body))
		http.Error(w, string(body), hresp.StatusCode)
		return
	}

	// Copy headers.
	for k, v := range hresp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(hresp.StatusCode)

	respBody, err := io.ReadAll(hresp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var genResp ensembleGenerateResponse
	if err := json.Unmarshal(respBody, &genResp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	chanResp := buildChatCompletionResponse(&genResp, createReq.Model)
	b, err = json.Marshal(chanResp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(w, bytes.NewBuffer(b)); err != nil {
		http.Error(w, fmt.Sprintf("Server error: %s", err), http.StatusInternalServerError)
		return
	}
}
