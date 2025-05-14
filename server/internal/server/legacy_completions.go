package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	auv1 "github.com/llmariner/api-usage/api/v1"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/common/pkg/sse"
	"github.com/llmariner/inference-manager/server/internal/rate"
	"github.com/llmariner/rbac-manager/pkg/auth"
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

	usage := newUsageRecord(userInfo, st, "CreateCompletion")
	details := &auv1.CreateCompletion{}
	usage.Details = &auv1.UsageDetails{
		Message: &auv1.UsageDetails_CreateCompletion{
			CreateCompletion: details,
		},
	}
	defer func() {
		usage.LatencyMs = int32(time.Since(st).Milliseconds())
		s.usageSetter.AddUsage(&usage)
	}()

	if !userInfo.ExcludedFromRateLimiting {
		res, err := s.ratelimiter.Take(req.Context(), userInfo.APIKeyID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rate.SetRateLimitHTTPHeaders(w, res)
		if !res.Allowed {
			httpError(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests, &usage)
			return
		}
	}

	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError, &usage)
		return
	}

	// TODO(kenji): Use runtime.JSONPb from github.com/grpc-ecosystem/grpc-gateway/v2.
	// That one correctly handles the JSON field names of the snake case.
	if err := json.Unmarshal(reqBody, &createReq); err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError, &usage)
		return
	}

	if createReq.Model == "" {
		httpError(w, "Model is required", http.StatusBadRequest, &usage)
		return
	}
	details.ModelId = createReq.Model

	// Increment the number of requests for the specified model.
	s.metricsMonitor.UpdateCompletionRequest(createReq.Model, 1)
	defer func() {
		// Decrement the number of requests for the specified model.
		s.metricsMonitor.UpdateCompletionRequest(createReq.Model, -1)
	}()

	ctx := auth.CarryMetadataFromHTTPHeader(req.Context(), req.Header)

	if code, err := s.checkModelAvailability(ctx, createReq.Model); err != nil {
		httpError(w, err.Error(), code, &usage)
		return
	}

	resp, err := s.taskSender.SendChatCompletionTask(ctx, userInfo.TenantID, toCreateChatCompletionRequest(&createReq), dropUnnecessaryHeaders(req.Header))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			httpError(w, "Request canceled", clientClosedRequestStatusCode, &usage)
			return
		}
		httpError(w, err.Error(), http.StatusInternalServerError, &usage)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	details.TimeToFirstTokenMs = int32(time.Since(st).Milliseconds())

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			// Gracefully handle the error.
			s.logger.Error(err, "Failed to read the body")
		}
		s.logger.Info("Received an error response", "code", resp.StatusCode, "status", resp.Status, "body", string(body))
		httpError(w, string(body), resp.StatusCode, &usage)
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
			httpError(w, err.Error(), http.StatusInternalServerError, &usage)
			return
		}

		// TODO(kenji): Use runtime.JSONPb from github.com/grpc-ecosystem/grpc-gateway/v2.
		// That one correctly handles the JSON field names of the snake case.
		var cc v1.ChatCompletion
		if err := json.Unmarshal(respBody, &cc); err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError, &usage)
			return
		}
		c := toCompletion(&cc)

		if u := c.Usage; u != nil {
			details.PromptTokens = u.PromptTokens
			details.CompletionTokens = u.CompletionTokens
		}

		b, err := json.Marshal(&c)
		if err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError, &usage)
			return
		}

		if _, err := io.Copy(w, bytes.NewBuffer(b)); err != nil {
			httpError(w, fmt.Sprintf("Server error: %s", err), http.StatusInternalServerError, &usage)
			return
		}
		return
	}

	// Streaming response.

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, "SSE not supported", http.StatusInternalServerError, &usage)
		return
	}

	scanner := sse.NewScanner(resp.Body)
	for scanner.Scan() {
		resp := scanner.Text()
		if !strings.HasPrefix(resp, "data: ") {
			// TODO(kenji): Handle other case.
			if _, err := w.Write([]byte(resp + sse.DoubleNewline)); err != nil {
				httpError(w, err.Error(), http.StatusInternalServerError, &usage)
				return
			}
			continue
		}

		respD := resp[5:]
		if respD == " [DONE]" {
			if _, err := w.Write([]byte(resp + sse.DoubleNewline)); err != nil {
				httpError(w, err.Error(), http.StatusInternalServerError, &usage)
				return
			}
			break
		}

		var chunk v1.ChatCompletionChunk
		if err := json.Unmarshal([]byte(respD), &chunk); err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError, &usage)
			return
		}

		c := toCompletionChunk(&chunk)

		// Extract the usage information.n
		// This works only with vLLM (https://github.com/vllm-project/vllm/pull/5135) as of Oct 3, 2024.
		// Ollama supports need https://github.com/ollama/ollama/issues/5200.
		if u := c.Usage; u != nil {
			details.PromptTokens = u.PromptTokens
			details.CompletionTokens = u.CompletionTokens
		}

		bs, err := json.Marshal(&c)
		if err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError, &usage)
			return
		}
		if _, err := w.Write([]byte("data: " + string(bs) + sse.DoubleNewline)); err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError, &usage)
			return
		}
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError, &usage)
		return
	}
}

func toCreateChatCompletionRequest(req *v1.CreateCompletionRequest) *v1.CreateChatCompletionRequest {
	var streamOptions *v1.CreateChatCompletionRequest_StreamOptions
	if req.Stream {
		// Always include the usage in the streaming repsonse.
		streamOptions = &v1.CreateChatCompletionRequest_StreamOptions{
			IncludeUsage: true,
		}
	}

	return &v1.CreateChatCompletionRequest{
		Messages: []*v1.CreateChatCompletionRequest_Message{
			{
				Role: "user",
				Content: []*v1.CreateChatCompletionRequest_Message_Content{
					{
						Type: "text",
						Text: req.Prompt,
					},
				},
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
		Seed:          req.Seed,
		Stop:          req.Stop,
		Stream:        req.Stream,
		StreamOptions: streamOptions,
		// No Suffix in the non-legacy request.
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
