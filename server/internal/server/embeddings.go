package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/server/internal/infprocessor"
	"github.com/llm-operator/rbac-manager/pkg/auth"
)

// CreateEmbedding creates an embedding.
func (s *S) CreateEmbedding(
	w http.ResponseWriter,
	req *http.Request,
	pathParams map[string]string,
) {
	var createReq v1.CreateEmbeddingRequest
	st := time.Now()
	defer func() {
		s.metricsMonitor.ObserveEmbeddingLatency(createReq.Model, time.Since(st))
	}()

	statusCode, userInfo, err := s.reqIntercepter.InterceptHTTPRequest(req)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
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

	if createReq.Input == "" {
		http.Error(w, "Input is required", http.StatusBadRequest)
		return
	}

	if createReq.Model == "" {
		http.Error(w, "Model is required", http.StatusBadRequest)
		return
	}

	s.metricsMonitor.UpdateEmbeddingRequest(createReq.Model, 1)
	defer func() {
		s.metricsMonitor.UpdateEmbeddingRequest(createReq.Model, -1)
	}()

	ctx := auth.CarryMetadataFromHTTPHeader(req.Context(), req.Header)

	if code, err := s.checkModelAvailability(ctx, createReq.Model); err != nil {
		http.Error(w, err.Error(), code)
		return
	}

	task, err := infprocessor.NewEmbeddingTask(userInfo.TenantID, &createReq, req.Header, s.logger)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create a task: %s", err), http.StatusInternalServerError)
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
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			// Gracefully handle the error.
			s.logger.Error(err, "Failed to read the body")
		}
		s.logger.Info("Received an error response", "code", resp.StatusCode, "status", resp.Status, "body", string(body))
		http.Error(w, string(body), resp.StatusCode)
		return
	}

	// Copy headers.
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		http.Error(w, fmt.Sprintf("Server error: %s", err), http.StatusInternalServerError)
		return
	}
	s.logger.Info("Embedding creation completed", "model", createReq.Model, "duration", time.Since(st))
}
