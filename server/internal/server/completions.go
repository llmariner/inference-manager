package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	auv1 "github.com/llmariner/api-usage/api/v1"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/common/pkg/api"
	"github.com/llmariner/inference-manager/common/pkg/sse"
	"github.com/llmariner/inference-manager/server/internal/rate"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
	vsv1 "github.com/llmariner/vector-store-manager/api/v1"
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

	usage := newUsageRecord(userInfo, st, "CreateChatCompletion")
	details := &auv1.CreateChatCompletion{}
	usage.Details = &auv1.UsageDetails{
		Message: &auv1.UsageDetails_CreateChatCompletion{
			CreateChatCompletion: details,
		},
	}
	defer func() {
		usage.LatencyMs = int32(time.Since(st).Milliseconds())
		s.usageSetter.AddUsage(&usage)
		s.metricsMonitor.ObserveRequestCount(details.ModelId, userInfo.TenantID, usage.StatusCode)
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

	reqBody, err = api.ConvertCreateChatCompletionRequestToProto(reqBody)
	if err != nil {
		httpError(w, err.Error(), http.StatusBadRequest, &usage)
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

	if len(createReq.Messages) == 0 {
		httpError(w, "Messages are required", http.StatusBadRequest, &usage)
		return
	}

	if c := toolChoiceType(createReq.ToolChoice); c != "" {
		if c != autoToolChoice && c != requiredToolChoice && c != noneToolChoice {
			httpError(w, "Invalid tool choice", http.StatusBadRequest, &usage)
			return
		}
	}
	if c := createReq.ToolChoiceObject; c != nil {
		if c.Type != functionObjectType {
			httpError(w, "Invalid tool choice object", http.StatusBadRequest, &usage)
			return
		}
		if c.Function == nil {
			httpError(w, "Function is required", http.StatusBadRequest, &usage)
			return
		}
		if c.Function.Name == "" {
			httpError(w, "Function name is required", http.StatusBadRequest, &usage)
			return
		}
	}

	// Include the usage by default. This is a temporary solution as continue.dev does not provide a way to configure
	// the stream options (and we cannot collect usages).
	if createReq.Stream && createReq.StreamOptions == nil {
		createReq.StreamOptions = &v1.CreateChatCompletionRequest_StreamOptions{
			IncludeUsage: true,
		}
	}
	// max_tokens is deprecated and replaced by max_completion_tokens, now
	if createReq.MaxCompletionTokens == 0 {
		createReq.MaxCompletionTokens = createReq.MaxTokens
	} else if createReq.MaxTokens != 0 && createReq.MaxCompletionTokens != createReq.MaxTokens {
		httpError(w, "MaxCompletionTokens and MaxTokens are mutually exclusive", http.StatusBadRequest, &usage)
		return
	}
	if createReq.MaxTokens == 0 {
		// MaxCompletionTokens is not supported in Ollama, yet: https://github.com/ollama/ollama/issues/7125
		createReq.MaxTokens = createReq.MaxCompletionTokens
	}
	// Increment the number of requests for the specified model.
	s.metricsMonitor.UpdateCompletionRequest(createReq.Model, 1)
	defer func() {
		// Decrement the number of requests for the specified model.
		s.metricsMonitor.UpdateCompletionRequest(createReq.Model, -1)
	}()

	ctx := auth.CarryMetadataFromHTTPHeader(req.Context(), req.Header)

	if code, err := s.checkModelAvailability(ctx, userInfo.TenantID, createReq.Model); err != nil {
		httpError(w, err.Error(), code, &usage)
		return
	}

	if code, err := s.handleToolsForRAG(ctx, &createReq); err != nil {
		httpError(w, err.Error(), code, &usage)
		return
	}

	resp, ps, err := s.taskSender.SendChatCompletionTask(ctx, userInfo.TenantID, &createReq, dropUnnecessaryHeaders(req.Header))
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
	details.RuntimeTimeToFirstTokenMs = ps.RuntimeTimeToFirstTokenMs()

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
		// Non streaming response. Just copy the response body.

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError, &usage)
			return
		}

		// Unmarshal the response to get usage details.
		// TODO(kenji): Use runtime.JSONPb from github.com/grpc-ecosystem/grpc-gateway/v2.
		// That one correctly handles the JSON field names of the snake case.
		var c v1.ChatCompletion
		if err := json.Unmarshal(respBody, &c); err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError, &usage)
			return
		}
		if u := c.Usage; u != nil {
			details.PromptTokens = u.PromptTokens
			details.CompletionTokens = u.CompletionTokens
		}

		if _, err := w.Write(respBody); err != nil {
			httpError(w, fmt.Sprintf("Server error: %s", err), http.StatusInternalServerError, &usage)
			return
		}

		usage.RuntimeLatencyMs = ps.RuntimeLatencyMs()

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
		if so := createReq.StreamOptions; so != nil && so.IncludeUsage && strings.HasPrefix(resp, "data: ") {
			// Extract the usage information.
			// This works only with vLLM (https://github.com/vllm-project/vllm/pull/5135) as of Oct 3, 2024.
			// Ollama supports need https://github.com/ollama/ollama/issues/5200.
			respD := resp[5:]
			if respD != " [DONE]" {
				// Unmarshal the response to get usage details.
				// TODO(kenji): Use runtime.JSONPb from github.com/grpc-ecosystem/grpc-gateway/v2.
				// That one correctly handles the JSON field names of the snake case.
				var chunk v1.ChatCompletionChunk
				if err := json.Unmarshal([]byte(respD), &chunk); err != nil {
					httpError(w, err.Error(), http.StatusInternalServerError, &usage)
					return
				}
				if u := chunk.Usage; u != nil {
					details.PromptTokens = u.PromptTokens
					details.CompletionTokens = u.CompletionTokens
				}
			}
		}

		if _, err := w.Write([]byte(resp + sse.DoubleNewline)); err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError, &usage)
			return
		}
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError, &usage)
		return
	}

	usage.RuntimeLatencyMs = ps.RuntimeLatencyMs()
}

// handleToolsForRAG inspects the tool and handles specially if there is a RAG tool funciton.
//
// Refer to https://platform.openai.com/docs/guides/function-calling for the details of the tools and the tool choice.
//
// The function returns an HTTP status code and an error.
//
// TODO(kenji): Revisit the entire logic for handling RAG.
func (s *S) handleToolsForRAG(ctx context.Context, req *v1.CreateChatCompletionRequest) (int, error) {
	var tools []*v1.CreateChatCompletionRequest_Tool
	for _, tool := range req.Tools {
		if tool.Type != functionObjectType {
			tools = append(tools, tool)
			continue
		}
		if tool.Function == nil {
			return http.StatusBadRequest, fmt.Errorf("function is required")
		}
		if tool.Function.Name != ragToolName {
			tools = append(tools, tool)
			continue
		}

		if req.ToolChoice == string(noneToolChoice) {
			// Do nothing. Just remove the tool.
			continue
		}

		var ragFunction v1.RagFunction

		b, err := base64.URLEncoding.DecodeString(tool.Function.EncodedParameters)
		if err != nil {
			return http.StatusBadRequest, fmt.Errorf("failed to decode parameters: %s", err)
		}
		if err := json.Unmarshal(b, &ragFunction); err != nil {
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
		req.Messages = msgs
	}
	req.Tools = tools
	if len(req.Tools) == 0 {
		// Clear the tool related fields as we don't want to pass this to Ollama.
		req.ToolChoice = ""
		req.ToolChoiceObject = nil
	}
	return http.StatusOK, nil
}

func (s *S) checkModelAvailability(ctx context.Context, tenantID, modelID string) (int, error) {
	// Skip checking model availability if it is served by nim.
	// NIM container images contain model already, so we don't need to check with model manager
	// and we can skip the activation.
	if _, ok := s.nimModels[modelID]; ok {
		return http.StatusOK, nil
	}

	m, err := s.modelClient.GetModel(ctx, tenantID, &mv1.GetModelRequest{
		Id: modelID,
	})

	if err != nil {
		if status.Code(err) == codes.NotFound {
			return http.StatusBadRequest, fmt.Errorf("model not found: %s", modelID)
		}
		return http.StatusInternalServerError, fmt.Errorf("get model: %s", err)
	}

	if m.ActivationStatus == mv1.ActivationStatus_ACTIVATION_STATUS_ACTIVE {
		return http.StatusOK, nil
	}

	if c := m.Config; c != nil {
		if !c.ClusterAllocationPolicy.EnableOnDemandAllocation {
			return http.StatusBadRequest, fmt.Errorf("model %s is not activated", modelID)
		}
	}

	s.logger.Info("Activating model", "modelID", modelID, "activationStatus", m.ActivationStatus)
	if _, err := s.modelClient.ActivateModel(ctx, tenantID, &mv1.ActivateModelRequest{
		Id: modelID,
	}); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("activate model: %s", err)
	}

	return http.StatusOK, nil
}

func newUsageRecord(ui auth.UserInfo, t time.Time, method string) auv1.UsageRecord {
	return auv1.UsageRecord{
		UserId:       ui.InternalUserID,
		Tenant:       ui.TenantID,
		Organization: ui.OrganizationID,
		Project:      ui.ProjectID,
		ApiKeyId:     ui.APIKeyID,
		ApiMethod:    fmt.Sprintf("/llmariner.chat.server.v1.ChatService/%s", method),
		StatusCode:   http.StatusOK,
		Timestamp:    t.UnixNano(),
	}
}

func httpError(w http.ResponseWriter, error string, code int, usage *auv1.UsageRecord) {
	usage.StatusCode = int32(code)
	http.Error(w, error, code)
}

// drop unnecessary headers that we don't want to pass to engine.
func dropUnnecessaryHeaders(headers http.Header) http.Header {
	// TODO(kenji): Add more.
	ks := []string{
		"Authorization",
		"Origin",
	}
	for _, k := range ks {
		headers.Del(k)
	}
	return headers
}
