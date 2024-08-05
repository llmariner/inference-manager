package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/common/pkg/models"
	"github.com/llm-operator/inference-manager/common/pkg/sse"
	"github.com/llm-operator/inference-manager/engine/internal/llmkind"
	"github.com/llm-operator/rbac-manager/pkg/auth"
)

const (
	completionPath = "/v1/chat/completions"

	// statusReportInterval is the interval to report engine status.
	// This needs to be shorter than an idle connection timeout period of
	// the server or the load balancer.
	statusReportInterval = 30 * time.Second
)

// ModelSyncer syncs models.
type ModelSyncer interface {
	ListSyncedModelIDs(ctx context.Context) []string
	PullModel(ctx context.Context, modelID string) error
}

// NewFakeModelSyncer returns a FakeModelSyncer.
func NewFakeModelSyncer() *FakeModelSyncer {
	return &FakeModelSyncer{
		modelIDs: map[string]struct{}{},
	}
}

// FakeModelSyncer is a fake implementation of model syncer.
type FakeModelSyncer struct {
	modelIDs map[string]struct{}
	mu       sync.Mutex
}

// ListSyncedModelIDs lists all models that have been synced.
func (s *FakeModelSyncer) ListSyncedModelIDs(ctx context.Context) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id := range s.modelIDs {
		ids = append(ids, id)
	}
	return ids
}

// PullModel downloads and registers a model from model manager.
func (s *FakeModelSyncer) PullModel(ctx context.Context, modelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelIDs[modelID] = struct{}{}
	return nil
}

// NewP returns a new processor.
func NewP(
	engineID string,
	client v1.InferenceWorkerServiceClient,
	llmAddr string,
	llmKind llmkind.K,
	modelSyncer ModelSyncer,
) *P {
	return &P{
		engineID:    engineID,
		client:      client,
		llmAddr:     llmAddr,
		llmKind:     llmKind,
		modelSyncer: modelSyncer,
	}
}

// P processes tasks.
type P struct {
	engineID    string
	client      v1.InferenceWorkerServiceClient
	llmAddr     string
	llmKind     llmkind.K
	modelSyncer ModelSyncer
}

// Run runs the processor.
//
// TODO(kenji): Gracefully handle an error from the server.
func (p *P) Run(ctx context.Context) error {
	ctx = auth.AppendWorkerAuthorization(ctx)
	stream, err := p.client.ProcessTasks(ctx)
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		if err := p.sendEngineStatusPeriodicaly(ctx, stream); err != nil {
			errCh <- fmt.Errorf("send engine status: %s", err)
		}
	}()

	go func() {
		if err := p.processTasks(ctx, stream, errCh); err != nil {
			errCh <- fmt.Errorf("process tasks: %s", err)
		}
	}()
	return <-errCh
}

func (p *P) sendEngineStatusPeriodicaly(
	ctx context.Context,
	stream v1.InferenceWorkerService_ProcessTasksClient,
) error {
	var isFirst bool

	log.Printf("Registering engine: %s\n", p.engineID)
	for {
		if err := p.sendEngineStatus(ctx, stream); err != nil {
			return err
		}

		if isFirst {
			isFirst = false
			log.Printf("Registered engine: %s\n", p.engineID)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(statusReportInterval):
		}
	}
}

func (p *P) processTasks(
	ctx context.Context,
	stream v1.InferenceWorkerService_ProcessTasksClient,
	errCh chan<- error,
) error {
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return fmt.Errorf("connection closed")
		}
		if err != nil {
			return fmt.Errorf("receive task: %s", err)
		}

		// Create a goroutine to process the task so that we can receive the
		// next task. llm then might process requests in parallel.
		go func() {
			log.Printf("Started processing task: %s\n", resp.NewTask.Id)
			if err := p.processTask(ctx, stream, resp.NewTask); err != nil {
				errCh <- fmt.Errorf("process task: %s", err)
				return
			}
			log.Printf("Completed task: %s\n", resp.NewTask.Id)
		}()
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
	// First pull the model if it is not yet pulled.
	if err := p.modelSyncer.PullModel(ctx, t.Request.Model); err != nil {
		return fmt.Errorf("pull model: %s", err)
	}

	req, err := p.buildRequest(ctx, t)
	if err != nil {
		return fmt.Errorf("build request: %s", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// TODO(kenji): Send the error back to the server?
		return fmt.Errorf("send request to llm: %s", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		log.Printf("Received an error response from llm: statusCode=%d, status=%q\n", resp.StatusCode, resp.Status)
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		log.Printf("Error response body: %q\n", body)

		httpResp := &v1.HttpResponse{
			StatusCode: int32(resp.StatusCode),
			Status:     resp.Status,
			Body:       body,
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
	switch p.llmKind {
	case llmkind.Ollama:
		t.Request.Model = models.OllamaModelName(t.Request.Model)
	case llmkind.VLLM:
		modelName, err := models.VLLMModelName(t.Request.Model)
		if err != nil {
			return nil, err
		}
		t.Request.Model = modelName
	default:
		return nil, fmt.Errorf("unsupported serving engine: %q", p.llmKind)
	}

	reqBody, err := json.Marshal(t.Request)
	if err != nil {
		return nil, err
	}

	baseURL := &url.URL{
		Scheme: "http",
		Host:   p.llmAddr,
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
				ModelIds: p.modelSyncer.ListSyncedModelIDs(ctx),
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
		return fmt.Errorf("send task result: %s", err)
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
		return fmt.Errorf("send task result: %s", err)
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
