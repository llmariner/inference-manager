package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/llm-operator/common/pkg/id"
	v1 "github.com/llm-operator/inference-manager/api/v1"
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

	taskID, err := id.GenerateID("inf_", 22)
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

	resp, err := task.WaitForCompletion()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 4096)
	scanner.Split(split)
	for scanner.Scan() {
		b := scanner.Text()
		if _, err := w.Write([]byte(b + "\n\n")); err != nil {
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

// split tokenizes the input. The arguments are an initial substring of the remaining unprocessed data and a flag,
// atEOF, that reports whether the Reader has no more data to give.
// The return values are the number of bytes to advance the input and the next token to return to the user,
// if any, plus an error, if any.
func split(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF {
		var nextToken []byte
		if len(data) > 0 {
			nextToken = data
		}
		return len(data), nextToken, nil
	}

	// Find a double newline.
	delims := [][]byte{
		[]byte("\r\r"),
		[]byte("\n\n"),
		[]byte("\r\n\r\n"),
	}
	pos := -1
	var dlen int
	for _, d := range delims {
		n := bytes.Index(data, d)
		if pos < 0 || (n >= 0 && n < pos) {
			pos = n
			dlen = len(d)
		}
	}

	if pos >= 0 {
		return pos + dlen, data[0:pos], nil
	}

	return 0, nil, nil
}
