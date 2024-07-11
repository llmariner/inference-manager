package server

import (
	"context"
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
	vsv1 "github.com/llm-operator/vector-store-manager/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type toolChoiceType string

const (
	functionObjectType string = "function"
	ragToolName        string = "rag"

	autoToolChoice     toolChoiceType = "auto"
	requiredToolChoice toolChoiceType = "required"
	noneToolChoice     toolChoiceType = "none"
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

	if code, err := s.handleTools(ctx, &createReq); err != nil {
		http.Error(w, err.Error(), code)
		return
	}

	taskID, err := id.GenerateID("inf_", 24)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate task ID: %s", err), http.StatusInternalServerError)
	}
	task := &infprocessor.Task{
		ID:       taskID,
		TenantID: userInfo.TenantID,
		Req:      &createReq,
		Header:   req.Header,
		RespCh:   make(chan *http.Response),
		ErrCh:    make(chan error),
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

// handleTools uses tools to process and modify the request messages.
// Refer to https://platform.openai.com/docs/guides/function-calling for the details of the tools and the tool choice.
//
// The function returns an HTTP status code and an error.
func (s *S) handleTools(ctx context.Context, req *v1.CreateChatCompletionRequest) (int, error) {
	if req.ToolChoice == nil || req.ToolChoice.Choice == string(noneToolChoice) {
		return http.StatusOK, nil
	}
	if req.ToolChoice.Type != functionObjectType {
		return http.StatusBadRequest, fmt.Errorf("unsupported tool choice type: %s", req.ToolChoice.Type)
	}

	var messages []*v1.CreateChatCompletionRequest_Message
	for _, tool := range req.Tools {
		if tool.Type != functionObjectType {
			return http.StatusBadRequest, fmt.Errorf("unsupported tool type: %s", tool.Type)
		}
		if tool.Function == nil {
			return http.StatusBadRequest, fmt.Errorf("function is required")
		}
		switch tool.Function.Name {
		case ragToolName:
			var ragFunction v1.RagFunction
			if err := json.Unmarshal([]byte(tool.Function.Parameters), &ragFunction); err != nil {
				return http.StatusBadRequest, err
			}
			if ragFunction.VectorStoreName == "" {
				return http.StatusBadRequest, fmt.Errorf("vector store name is required")
			}
			// TODO(kenji): Check if the request is allowed to access the specified vector store.
			vstore, err := s.vsClient.GetVectorStoreByName(ctx, &vsv1.GetVectorStoreByNameRequest{
				Name: ragFunction.VectorStoreName,
			})
			if err != nil {
				if status.Code(err) == codes.NotFound {
					return http.StatusBadRequest, fmt.Errorf("vector store not found: %s", ragFunction.VectorStoreName)
				}
				return http.StatusInternalServerError, fmt.Errorf("failed to get vector store: %s", err)
			}

			msgs, err := s.rewriter.ProcessMessages(ctx, vstore, req.Messages)
			if err != nil {
				return http.StatusInternalServerError, err
			}
			messages = append(messages, msgs...)
		default:
			return http.StatusBadRequest, fmt.Errorf("unsupported function name: %s", tool.Function.Name)
		}
	}
	req.Messages = messages
	return http.StatusOK, nil
}
