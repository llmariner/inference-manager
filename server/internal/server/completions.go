package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	auv1 "github.com/llmariner/api-usage/api/v1"
	v1 "github.com/llmariner/inference-manager/api/v1"
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

	contentTypeText = "text"
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
	}()

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

	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError, &usage)
		return
	}

	reqBody, code, err := convertContentStringToArray(reqBody)
	if err != nil {
		httpError(w, err.Error(), code, &usage)
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

	// Include the usage by default. This is a temporary solution as continue.ev does not provide a way to the stream options.
	if createReq.Stream && createReq.StreamOptions == nil {
		createReq.StreamOptions = &v1.CreateChatCompletionRequest_StreamOptions{
			IncludeUsage: true,
		}
	}

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

	if code, err := s.handleTools(ctx, &createReq); err != nil {
		httpError(w, err.Error(), code, &usage)
		return
	}

	resp, err := s.taskSender.SendChatCompletionTask(ctx, userInfo.TenantID, &createReq, req.Header)
	if err != nil {
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

		if _, err := io.Copy(w, bytes.NewBuffer(respBody)); err != nil {
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
	// Clear the tool related fields as we don't want to pass this to Ollama.
	req.ToolChoice = nil
	req.Tools = nil
	return http.StatusOK, nil
}

func (s *S) checkModelAvailability(ctx context.Context, modelID string) (int, error) {
	if _, err := s.modelClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: modelID,
	}); err != nil {
		if status.Code(err) == codes.NotFound {
			return http.StatusBadRequest, fmt.Errorf("model not found: %s", modelID)
		}
		return http.StatusInternalServerError, fmt.Errorf("get model: %s", err)
	}
	return http.StatusOK, nil
}

// convertContentStringToArray converts the content field from a string to an array.
//
// This is a workaround to follow the OpenAI API spec, which cannot be handled by the protobuf.
//
// This function returns the converted request boy, an HTTP status code, and an error.
func convertContentStringToArray(body []byte) ([]byte, int, error) {
	r := map[string]interface{}{}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return nil, http.StatusInternalServerError, err
	}

	msgs, ok := r["messages"]
	if !ok {
		return nil, http.StatusBadRequest, fmt.Errorf("messages is required")
	}
	for _, msg := range msgs.([]interface{}) {
		m := msg.(map[string]interface{})
		content, ok := m["content"]
		if !ok {
			return nil, http.StatusBadRequest, fmt.Errorf("content is required")
		}
		if cs, ok := content.(string); ok {
			m["content"] = []map[string]interface{}{
				{
					"type": contentTypeText,
					"text": cs,
				},
			}
		}
	}

	// Marshal again and then unmarshal.
	body, err := json.Marshal(r)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to marshal the request: %s", err)
	}

	return body, 0, nil
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
