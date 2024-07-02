package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/common/pkg/models"
	"github.com/llm-operator/inference-manager/common/pkg/sse"
	"github.com/llm-operator/rbac-manager/pkg/auth"
)

const (
	completionPath = "/v1/chat/completions"
)

type modelLister interface {
	ListSyncedModelIDs(ctx context.Context) []string
}

// NewP returns a new processor.
func NewP(
	engineID string,
	client v1.InferenceWorkerServiceClient,
	ollamaAddr string,
	modelLister modelLister,
) *P {
	return &P{
		engineID:    engineID,
		client:      client,
		ollamaAddr:  ollamaAddr,
		modelLister: modelLister,
	}
}

// P processes tasks.
type P struct {
	engineID    string
	client      v1.InferenceWorkerServiceClient
	ollamaAddr  string
	modelLister modelLister
}

// Run runs the processor.
func (p *P) Run(ctx context.Context) error {
	auth.AppendWorkerAuthorization(ctx)
	stream, err := p.client.ProcessTasks(ctx)
	if err != nil {
		return err
	}

	log.Printf("Registering engine: %s\n", p.engineID)
	if err := p.sendEngineStatus(ctx, stream); err != nil {
		return err
	}

	// TODO(kenji): Periodically send engine status.

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := p.processTask(ctx, stream, resp.NewTask); err != nil {
			return err
		}
	}
}

type sender interface {
	Send(*v1.ProcessTasksRequest) error
}

func (p *P) processTask(
	ctx context.Context,
	stream sender,
	t *v1.Task,
) error {
	log.Printf("Processing task: %s\n", t.Id)

	req, err := p.buildRequest(ctx, t)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// TODO(kenji): Send the error back to the server?
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		httpResp := &v1.HttpResponse{
			StatusCode: int32(resp.StatusCode),
			Status:     resp.Status,
		}
		if err := p.sendHTTPResponse(ctx, stream, t, httpResp); err != nil {
			return err
		}
		return nil
	}

	respHeader := map[string]*v1.HeaderValue{}
	for k, vs := range resp.Header {
		respHeader[k] = &v1.HeaderValue{Values: vs}
	}

	var body []byte
	if !t.Request.Stream {
		// Non streaming response. Just copy the response body.
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
	}

	httpResp := &v1.HttpResponse{
		StatusCode: int32(resp.StatusCode),
		Status:     resp.Status,
		Header:     respHeader,
		Body:       body,
	}
	if err := p.sendHTTPResponse(ctx, stream, t, httpResp); err != nil {
		return err
	}

	if !t.Request.Stream {
		return nil
	}

	scanner := sse.NewScanner(resp.Body)
	for scanner.Scan() {
		b := scanner.Text()
		e := &v1.ServerSentEvent{
			Data: []byte(b + sse.DoubleNewline),
		}
		if err := p.sendServerSentEvent(ctx, stream, t, e); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		// TODO(kenji): Send the error back to the server?
		return err
	}

	e := &v1.ServerSentEvent{
		IsLastEvent: true,
	}
	if err := p.sendServerSentEvent(ctx, stream, t, e); err != nil {
		return err
	}

	return nil
}

func (p *P) buildRequest(ctx context.Context, t *v1.Task) (*http.Request, error) {
	t.Request.Model = models.OllamaModelName(t.Request.Model)
	reqBody, err := json.Marshal(t.Request)
	if err != nil {
		return nil, err
	}

	baseURL := &url.URL{
		Scheme: "http",
		Host:   p.ollamaAddr,
	}
	requestURL := baseURL.JoinPath(completionPath).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	// Copy headers.
	for k, vs := range t.Header {
		for _, v := range vs.Values {
			req.Header.Add(k, v)
		}
	}
	return req, nil
}

func (p *P) sendEngineStatus(ctx context.Context, stream sender) error {
	req := &v1.ProcessTasksRequest{
		Message: &v1.ProcessTasksRequest_EngineStatus{
			EngineStatus: &v1.EngineStatus{
				EngineId: p.engineID,
				ModelIds: p.modelLister.ListSyncedModelIDs(ctx),
			},
		},
	}
	if err := stream.Send(req); err != nil {
		return err
	}

	return nil
}

func (p *P) sendHTTPResponse(
	ctx context.Context,
	stream sender,
	t *v1.Task,
	resp *v1.HttpResponse,
) error {
	result := &v1.TaskResult{
		TaskId: t.Id,
		Message: &v1.TaskResult_HttpResponse{
			HttpResponse: resp,
		},
	}
	if err := p.sendTaskResult(ctx, stream, result); err != nil {
		return err
	}
	return nil
}

func (p *P) sendServerSentEvent(
	ctx context.Context,
	stream sender,
	t *v1.Task,
	e *v1.ServerSentEvent,
) error {
	result := &v1.TaskResult{
		TaskId: t.Id,
		Message: &v1.TaskResult_ServerSentEvent{
			ServerSentEvent: e,
		},
	}
	if err := p.sendTaskResult(ctx, stream, result); err != nil {
		return err
	}
	return nil
}

func (p *P) sendTaskResult(
	ctx context.Context,
	stream sender,
	result *v1.TaskResult,
) error {
	req := &v1.ProcessTasksRequest{
		Message: &v1.ProcessTasksRequest_TaskResult{
			TaskResult: result,
		},
	}
	if err := stream.Send(req); err != nil {
		return err
	}
	return nil
}
