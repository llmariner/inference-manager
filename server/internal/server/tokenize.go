package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	auv1 "github.com/llmariner/api-usage/api/v1"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/common/pkg/api"
	"github.com/llmariner/inference-manager/server/internal/rate"
	"github.com/llmariner/rbac-manager/pkg/auth"
)

// Tokenize tokenizes the input text.
func (s *S) Tokenize(
	w http.ResponseWriter,
	req *http.Request,
	pathParams map[string]string,
) {
	var createReq v1.TokenizeRequest
	st := time.Now()

	statusCode, userInfo, err := s.reqIntercepter.InterceptHTTPRequest(req)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	usage := newUsageRecord(userInfo, st, "Tokenize")
	details := &auv1.Tokenize{}
	usage.Details = &auv1.UsageDetails{
		Message: &auv1.UsageDetails_Tokenize{
			Tokenize: details,
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

	reqBody, err = api.ConvertTokenizeRequestToProto(reqBody)
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

	ctx := auth.CarryMetadataFromHTTPHeader(req.Context(), req.Header)

	if code, err := s.checkModelAvailability(ctx, userInfo.TenantID, createReq.Model); err != nil {
		httpError(w, err.Error(), code, &usage)
		return
	}

	resp, _, err := s.taskSender.SendTokenizeTask(ctx, userInfo.TenantID, &createReq, dropUnnecessaryHeaders(req.Header))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			httpError(w, "Request canceled", clientClosedRequestStatusCode, &usage)
			return
		}
		httpError(w, err.Error(), http.StatusInternalServerError, &usage)
		return
	}
	defer func() { _ = resp.Body.Close() }()

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError, &usage)
		return
	}

	if _, err := w.Write(respBody); err != nil {
		httpError(w, fmt.Sprintf("Server error: %s", err), http.StatusInternalServerError, &usage)
		return
	}
}
