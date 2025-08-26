package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	auv1 "github.com/llmariner/api-usage/api/v1"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
)

// CreateAudioTranscription creates a new audio transcription.
//
// TODO(kenji): Support all parameters defined in
// https://platform.openai.com/docs/api-reference/audio/createTranscription
func (s *S) CreateAudioTranscription(
	w http.ResponseWriter,
	req *http.Request,
	pathParams map[string]string,
) {
	var createReq v1.CreateAudioTranscriptionRequest
	st := time.Now()
	defer func() {
		s.metricsMonitor.ObserveAudioTranscriptionLatency(createReq.Model, time.Since(st))
	}()

	statusCode, userInfo, err := s.reqIntercepter.InterceptHTTPRequest(req)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	usage := newUsageRecord(userInfo, st, "CreateAudioTranscription")
	details := &auv1.CreateAudioTranscription{}
	usage.Details = &auv1.UsageDetails{
		Message: &auv1.UsageDetails_CreateAudioTranscription{
			CreateAudioTranscription: details,
		},
	}
	defer func() {
		usage.LatencyMs = int32(time.Since(st).Milliseconds())
		s.usageSetter.AddUsage(&usage)
		s.metricsMonitor.ObserveRequestCount(details.ModelId, userInfo.TenantID, usage.StatusCode)
	}()

	createReq.Model = req.FormValue("model")
	if createReq.Model == "" {
		httpError(w, "model is required", http.StatusBadRequest, &usage)
		return
	}
	details.ModelId = createReq.Model

	// TODO(kenji): Validate the value of "language" parameter.
	createReq.Language = req.FormValue("language")

	createReq.Prompt = req.FormValue("prompt")

	if rf := req.FormValue("response_format"); rf != "" {
		validFormats := map[string]bool{
			"json":         true,
			"text":         true,
			"srt":          true,
			"verbose_json": true,
			"vtt":          true,
		}
		if !validFormats[rf] {
			httpError(w, fmt.Sprintf("invalid response_format: %s", rf), http.StatusBadRequest, &usage)
			return
		}
		createReq.ResponseFormat = rf
	}

	if t := req.FormValue("temperature"); t != "" {
		tmp, err := strconv.ParseFloat(t, 64)
		if err != nil {
			httpError(w, fmt.Sprintf("invalid temperature value: %s", err), http.StatusBadRequest, &usage)
			return
		}
		if tmp < 0 || tmp > 1 {
			httpError(w, fmt.Sprintf("temperature must be between 0 and 1, but got %v", tmp), http.StatusBadRequest, &usage)
			return
		}

		createReq.Temperature = tmp
	}

	if t := req.FormValue("stream"); t != "" {
		if t != "true" && t != "false" {
			httpError(w, "stream must be true or false", http.StatusBadRequest, &usage)
			return
		}
		// TODO(kenji): Support "Stream"
		if t == "true" {
			httpError(w, "streaming is not supported yet", http.StatusNotImplemented, &usage)
			return
		}
	}

	file, header, err := req.FormFile("file")
	if err != nil {
		if err == http.ErrMissingFile {
			httpError(w, "file is required", http.StatusBadRequest, &usage)
			return
		}
		httpError(w, err.Error(), http.StatusBadRequest, &usage)
		return
	}
	defer func() {
		_ = file.Close()
	}()
	if header.Size == 0 {
		httpError(w, "file is empty", http.StatusBadRequest, &usage)
		return
	}
	createReq.Filename = header.Filename
	createReq.File, err = io.ReadAll(file)
	if err != nil {
		httpError(w, fmt.Sprintf("failed to read file: %s", err), http.StatusInternalServerError, &usage)
		return
	}

	// Increment the number of requests for the specified model.
	s.metricsMonitor.UpdateAudioTranscriptionRequest(createReq.Model, 1)
	defer func() {
		// Decrement the number of requests for the specified model.
		s.metricsMonitor.UpdateAudioTranscriptionRequest(createReq.Model, -1)
	}()

	ctx := auth.CarryMetadataFromHTTPHeader(req.Context(), req.Header)

	if code, err := s.checkModelAvailability(ctx, userInfo.TenantID, createReq.Model); err != nil {
		httpError(w, err.Error(), code, &usage)
		return
	}

	resp, ps, err := s.taskSender.SendAudioTranscriptionTask(ctx, userInfo.TenantID, &createReq, dropUnnecessaryHeaders(req.Header))
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError, &usage)
		return
	}

	// TODO(kenji): Set usage details.

	if _, err := io.Copy(w, bytes.NewBuffer(respBody)); err != nil {
		httpError(w, fmt.Sprintf("Failed to copy response body: %s", err), http.StatusInternalServerError, &usage)
		return
	}

	usage.RuntimeLatencyMs = ps.RuntimeLatencyMs()
}
