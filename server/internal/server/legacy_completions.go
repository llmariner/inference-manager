package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
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

// CreateCompletion creates a (legacy) completion.
//
// The implementation is similar to CreateChatCompletion, but this has extra logic
// for converting a legacy request to a non-legacy request (and vice versa for response).
//
// TODO(kenji): Avoid code duplication CreateChatCompletion.
func (s *S) CreateCompletion(
	w http.ResponseWriter,
	req *http.Request,
	pathParams map[string]string,
) {
	var createReq v1.CreateCompletionRequest
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

	taskID, err := id.GenerateID("inf_", 24)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate task ID: %s", err), http.StatusInternalServerError)
		return
	}
	task := &infprocessor.Task{
		ID:        taskID,
		TenantID:  userInfo.TenantID,
		Req:       toCreateChatCompletionRequest(&createReq),
		Header:    req.Header,
		RespCh:    make(chan *http.Response),
		ErrCh:     make(chan error),
		CreatedAt: time.Now(),
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
			log.Printf("Failed toread the body: %s\n", err)
		}
		log.Printf("Received an error response: statusCode=%d, status=%q, body=%q\n", resp.StatusCode, resp.Status, string(body))
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

	if !createReq.Stream {
		// Non streaming response.

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// TODO(kenji): Use runtime.JSONPb from github.com/grpc-ecosystem/grpc-gateway/v2.
		// That one correctly handles the JSON field names of the snake case.
		var cc v1.ChatCompletion
		if err := json.Unmarshal(respBody, &cc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		c := toCompletion(&cc)
		b, err := json.Marshal(&c)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if _, err := io.Copy(w, bytes.NewBuffer(b)); err != nil {
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
		resp := scanner.Text()
		if !strings.HasPrefix(resp, "data: ") {
			// TODO(kenji): Handle other case.
			continue
		}

		respD := resp[5:]
		if respD == " [DONE]" {
			break
		}

		var chunk v1.ChatCompletionChunk
		if err := json.Unmarshal([]byte(respD), &chunk); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		c := toCompletionChunk(&chunk)
		bs, err := json.Marshal(&c)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := w.Write([]byte(string(bs) + sse.DoubleNewline)); err != nil {
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

func toCreateChatCompletionRequest(req *v1.CreateCompletionRequest) *v1.CreateChatCompletionRequest {
	return &v1.CreateChatCompletionRequest{
		Messages: []*v1.CreateChatCompletionRequest_Message{
			{
				Content: req.Prompt,
				Role:    "user",
			},
		},
		Model: req.Model,
		// No support of BestOf and Echo in the non-legacy request.
		FrequencyPenalty: req.FrequencyPenalty,
		LogitBias:        req.LogitBias,
		// The legacy request has Logprobs, but it corresponds to TopLogprobs in the non-lgeacy request.
		Logprobs:        false,
		TopLogprobs:     req.Logprobs,
		MaxTokens:       req.MaxTokens,
		N:               req.N,
		PresencePenalty: req.PresencePenalty,
		// No ResponseFormat in the legacy request.
		Seed:   req.Seed,
		Stop:   req.Stop,
		Stream: req.Stream,
		// No StreamOptions and Suffix in the non-legacy request.
		Temperature: req.Temperature,
		TopP:        req.TopP,
		// No Tools and ToolChoice in the legacy request.
		User: req.User,
	}
}

func toCompletion(c *v1.ChatCompletion) *v1.Completion {
	var choices []*v1.Completion_Choice
	for _, ch := range c.Choices {
		choices = append(choices, &v1.Completion_Choice{
			FinishReason: ch.FinishReason,
			Index:        ch.Index,
			Text:         ch.Message.Content,
			// TODO(kenji): Convert logProblem.
		})
	}

	return &v1.Completion{
		Id:                c.Id,
		Choices:           choices,
		Created:           c.Created,
		SystemFingerprint: c.SystemFingerprint,
		Object:            "text_completion",
		Usage:             c.Usage,
	}
}

func toCompletionChunk(c *v1.ChatCompletionChunk) *v1.Completion {
	var choices []*v1.Completion_Choice
	for _, ch := range c.Choices {
		choices = append(choices, &v1.Completion_Choice{
			FinishReason: ch.FinishReason,
			Index:        ch.Index,
			Text:         ch.Delta.Content,
			// TODO(kenji): Convert logProblem.
		})
	}

	return &v1.Completion{
		Id:                c.Id,
		Choices:           choices,
		Created:           c.Created,
		SystemFingerprint: c.SystemFingerprint,
		Object:            "text_completion",
		Usage:             c.Usage,
	}
}
