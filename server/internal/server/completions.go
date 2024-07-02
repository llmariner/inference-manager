package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/llm-operator/common/pkg/id"
	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/common/pkg/sse"
	"github.com/llm-operator/inference-manager/server/internal/infprocessor"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CreateChatCompletion creates a chat completion.
func (s *S) CreateChatCompletion(
	w http.ResponseWriter,
	req *http.Request,
	pathParams map[string]string,
) {
	var createReq v1.CreateChatCompletionRequest
	st := time.Now()
	defer func() {
		s.metricsMonitor.ObserveCompletionLatency(createReq.Model, time.Since(st))
	}()

	if status, _, err := s.reqIntercepter.InterceptHTTPRequest(req); err != nil {
		http.Error(w, err.Error(), status)
		return
	}

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

	// Increment the number of requests for the specified model.
	s.metricsMonitor.UpdateCompletionRequest(createReq.Model, 1)
	defer func() {
		// Decrement the number of requests for the specified model.
		s.metricsMonitor.UpdateCompletionRequest(createReq.Model, -1)
	}()

	// Check if the specified model is available
	ctx := auth.CarryMetadataFromHTTPHeader(req.Context(), req.Header)
	if _, err := s.modelClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: createReq.Model,
	}); err != nil {
		if status.Code(err) == codes.NotFound {
			http.Error(w, fmt.Sprintf("Model not found: %s", createReq.Model), http.StatusBadRequest)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to get model: %s", err), http.StatusInternalServerError)
		return
	}

	taskID, err := id.GenerateID("inf_", 24)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate task ID: %s", err), http.StatusInternalServerError)
	}
	task := &infprocessor.Task{
		ID:     taskID,
		Req:    &createReq,
		Header: req.Header,
		RespCh: make(chan *http.Response),
		ErrCh:  make(chan error),
	}

	s.taskQueue.Enqueue(task)

	resp, err := task.WaitForCompletion(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		http.Error(w, resp.Status, resp.StatusCode)
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
			http.Error(w, fmt.Sprintf("Server error: %s", err), http.StatusInternalServerError)
			return
		}
		return
	}

	// Streaming response.

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	scanner := sse.NewScanner(resp.Body)
	for scanner.Scan() {
		b := scanner.Text()
		if _, err := w.Write([]byte(b + sse.DoubleNewline)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
