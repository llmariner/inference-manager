package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/inference-manager/common/pkg/sse"
	"github.com/llmariner/inference-manager/engine/internal/metrics"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"golang.org/x/sync/errgroup"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	completionPath = "/v1/chat/completions"
	embeddingPath  = "/v1/embeddings"

	// statusReportInterval is the interval to report engine status.
	// This needs to be shorter than an idle connection timeout period of
	// the server or the load balancer.
	statusReportInterval = 30 * time.Second

	retryInterval = 10 * time.Second
)

// ModelSyncer syncs models.
type ModelSyncer interface {
	ListSyncedModelIDs() []string
	PullModel(ctx context.Context, modelID string) error
	ListInProgressModels() []string
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

// ListInProgressModels lists all models that are in progress.
func (s *FakeModelSyncer) ListInProgressModels() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nil
}

// PullModel downloads and registers a model from model manager.
func (s *FakeModelSyncer) PullModel(ctx context.Context, modelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelIDs[modelID] = struct{}{}
	return nil
}

// AddressGetter gets an address of a model.
type AddressGetter interface {
	GetLLMAddress(modelID string) (string, error)
}

// NewFixedAddressGetter returns a new FixedAddressGetter.
func NewFixedAddressGetter(addr string) *FixedAddressGetter {
	return &FixedAddressGetter{addr: addr}
}

// FixedAddressGetter is a fixed address getter.
type FixedAddressGetter struct {
	addr string
}

// GetLLMAddress returns a fixed address.
func (g *FixedAddressGetter) GetLLMAddress(modelID string) (string, error) {
	return g.addr, nil
}

// NoopMetricsCollector is a no-op metrics collector.
type NoopMetricsCollector struct{}

// Add does nothing.
func (NoopMetricsCollector) Add(modelID string, v float64) {}

// NewP returns a new processor.
func NewP(
	engineID string,
	client v1.InferenceWorkerServiceClient,
	addrGetter AddressGetter,
	modelSyncer ModelSyncer,
	logger logr.Logger,
	collector metrics.Collector,
	gracefulShutdownTimeout time.Duration,
) *P {
	return &P{
		engineID:    engineID,
		client:      client,
		addrGetter:  addrGetter,
		modelSyncer: modelSyncer,
		logger:      logger,
		metrics:     collector,
		// taskGracePeriod is the grace period to wait for all tasks to complete.
		// Grace period is shorter than the graceful shutdown timeout of 3s for safety.
		// If tasks are not completed within the grace period, the processor forcibly
		// cancels the tasks processing and stops.
		taskGracePeriod: gracefulShutdownTimeout - 3*time.Second,
	}
}

// P processes tasks.
type P struct {
	engineID    string
	client      v1.InferenceWorkerServiceClient
	addrGetter  AddressGetter
	modelSyncer ModelSyncer
	metrics     metrics.Collector

	// lastErr is the last error from run().
	// It is cleared when the registration succeeds.
	lastErr error
	mu      sync.Mutex
	logger  logr.Logger

	taskGracePeriod time.Duration

	leaderElection bool
}

// SetupWithManager sets up the processor with the manager.
func (p *P) SetupWithManager(mgr ctrl.Manager, leaderElection bool) error {
	p.logger = mgr.GetLogger().WithName("processor")
	p.leaderElection = leaderElection
	return mgr.Add(p)
}

// NeedLeaderElection implements LeaderElectionRunnable
func (p *P) NeedLeaderElection() bool {
	// processor is only use leader election when the autoscaler is enabled.
	// This is because the processor collects metrics and use it for scaling.
	// Otherwise, the processor does not update k8s resources except for a
	// new runtime creation, so it does not need leader election. This means
	// that when using an existing runtime, such as during maintenance,
	// requests can be handled quickly without waiting for leader-election.
	return p.leaderElection
}

// Start runs the processor.
//
// TODO(kenji): Gracefully handle an error from the server.
func (p *P) Start(ctx context.Context) error {
	log := p.logger.WithValues("engineID", p.engineID)
	log.Info("Starting processor")
	ctx = ctrl.LoggerInto(ctx, log)
	ctx = auth.AppendWorkerAuthorization(ctx)

	run := func() error {
		streamCtx, streamCancel := context.WithCancel(context.Background())
		streamCtx = ctrl.LoggerInto(streamCtx, ctrl.LoggerFrom(ctx))
		streamCtx = auth.AppendWorkerAuthorization(streamCtx)
		defer streamCancel()
		// Use separate context for the stream to gracefully handle the task requests.
		stream, err := p.client.ProcessTasks(streamCtx)
		if err != nil {
			return err
		}
		defer func() { _ = stream.CloseSend() }()

		eg, ctx := errgroup.WithContext(ctx)
		eg.Go(func() error { return p.sendEngineStatusPeriodically(ctx, stream) })
		eg.Go(func() error { return p.processTasks(ctx, stream) })
		return eg.Wait()
	}

	for {
		if err := run(); err != nil {
			log.Error(err, "Processor error")
			p.mu.Lock()
			p.lastErr = err
			p.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			log.Info("Stopped processor", "ctx", ctx.Err())
			return nil
		case <-time.After(retryInterval):
			log.Info("Retrying processor", "retry-interval", retryInterval)
		}
	}
}

func (p *P) sendEngineStatusPeriodically(
	ctx context.Context,
	stream v1.InferenceWorkerService_ProcessTasksClient,
) error {
	log := ctrl.LoggerFrom(ctx).WithName("status")
	ctx = ctrl.LoggerInto(ctx, log)
	defer log.Info("Stopped status reporter")

	isFirst := true
	for {
		if err := p.sendEngineStatus(stream, true); err != nil {
			return err
		}

		if isFirst {
			isFirst = false
			log.Info("Successfully registered engine")
		}

		// Clear the last error.
		// TODO(kenji): Revisit. This might hide an error in the process task.
		p.mu.Lock()
		p.lastErr = nil
		p.mu.Unlock()

		select {
		case <-stream.Context().Done():
			return nil
		case <-ctx.Done():
			if err := p.sendEngineStatus(stream, false); err != nil {
				return err
			}
			return nil
		case <-time.After(statusReportInterval):
		}
	}
}

func (p *P) processTasks(
	ctx context.Context,
	stream v1.InferenceWorkerService_ProcessTasksClient,
) error {
	log := ctrl.LoggerFrom(ctx).WithName("task")
	ctx = ctrl.LoggerInto(ctx, log)
	defer log.Info("Stopped process handler")

	respCh := make(chan *v1.ProcessTasksResponse)
	errCh := make(chan error)
	doneCh := make(chan struct{})
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				if isConnClosedErr(err) {
					err = fmt.Errorf("connection closed")
				} else {
					err = fmt.Errorf("receive task: %s", err)
				}
				errCh <- err
				return
			}
			respCh <- resp

			select {
			case <-doneCh:
				return
			default:
			}
		}
	}()

	var wg sync.WaitGroup
	for {
		select {
		case resp := <-respCh:
			// Create a goroutine to process the task so that we can receive the
			// next task. llm then might process requests in parallel.
			wg.Add(1)
			go func() {
				defer wg.Done()
				p.metrics.Add(taskModel(resp.NewTask), 1)
				defer p.metrics.Add(taskModel(resp.NewTask), -1)

				log := log.WithValues("taskID", resp.NewTask.Id)
				log.Info("Started processing task")
				if err := p.processTask(ctrl.LoggerInto(ctx, log), stream, resp.NewTask); errors.Is(err, context.Canceled) {
					log.Info("Canceled task", "reason", err)
				} else if err != nil {
					log.Error(err, "Failed to process task")
				} else {
					log.Info("Completed task")
				}
			}()
		case err := <-errCh:
			return err
		case <-ctx.Done():
			log.Info("Stopping and Waiting for all tasks to complete", "grace-period", p.taskGracePeriod)
			wg.Wait()
			close(doneCh)
			return nil
		}
	}
}

type sender interface {
	Context() context.Context
	Send(*v1.ProcessTasksRequest) error
}

func (p *P) processTask(
	ctx context.Context,
	stream sender,
	t *v1.Task,
) error {
	log := ctrl.LoggerFrom(ctx)

	// First pull the model if it is not yet pulled.
	if err := p.modelSyncer.PullModel(ctx, taskModel(t)); err != nil {
		return fmt.Errorf("pull model: %s", err)
	}

	var done atomic.Bool
	taskCtx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		select {
		case <-ctx.Done():
			// cancel the request immediately if the processor does not get first response,
			// otherwise wait for the response within the grace period.
			if done.Load() {
				time.Sleep(p.taskGracePeriod)
			}
		case <-stream.Context().Done():
		case <-taskCtx.Done():
		}
	}()

	req, err := p.buildRequest(taskCtx, t)
	if err != nil {
		return fmt.Errorf("build request: %s", err)
	}

	log.Info("Sending request to the LLM server", "url", req.URL)
	switch req := t.Request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion:
		log.V(1).Info(fmt.Sprintf("Request: %+v", req.GetChatCompletion()))
	case *v1.TaskRequest_Embedding:
		log.V(1).Info(fmt.Sprintf("Request: %+v", req.GetEmbedding()))
	default:
		return fmt.Errorf("unknown request type: %T", req.Request)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if stream.Context().Err() != nil {
			return stream.Context().Err()
		}
		var code int32
		if errors.Is(err, context.Canceled) {
			code = http.StatusServiceUnavailable
		} else {
			log.Error(err, "Failed to send request to the LLM server")
			code = http.StatusInternalServerError
		}
		return p.sendHTTPResponse(stream, t, &v1.HttpResponse{
			StatusCode: code,
			Status:     http.StatusText(int(code)),
			Body:       []byte(fmt.Sprintf("Failed to send request to the LLM server: %s", err)),
		})
	}
	defer func() { _ = resp.Body.Close() }()
	done.Store(true)
	log.Info("Received an initial response from the LLM server", "status", resp.Status)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		log.Info("Received an error response from the LLM server", "statusCode", resp.StatusCode, "status", resp.Status)
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		log.Info(fmt.Sprintf("Error response body: %q", body))

		httpResp := &v1.HttpResponse{
			StatusCode: int32(resp.StatusCode),
			Status:     resp.Status,
			Body:       body,
		}
		if err := p.sendHTTPResponse(stream, t, httpResp); err != nil {
			return err
		}
		return nil
	}

	respHeader := map[string]*v1.HeaderValue{}
	for k, vs := range resp.Header {
		respHeader[k] = &v1.HeaderValue{Values: vs}
	}

	var body []byte
	if !taskStream(t) {
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
	if err := p.sendHTTPResponse(stream, t, httpResp); err != nil {
		return err
	}

	if !taskStream(t) {
		return nil
	}

	scanner := sse.NewScanner(resp.Body)
	for scanner.Scan() {
		b := scanner.Text()
		e := &v1.ServerSentEvent{
			Data: []byte(b + sse.DoubleNewline),
		}
		if err := p.sendServerSentEvent(stream, t, e); err != nil {
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
	if err := p.sendServerSentEvent(stream, t, e); err != nil {
		return err
	}

	return nil
}

func (p *P) buildRequest(ctx context.Context, t *v1.Task) (*http.Request, error) {
	addr, err := p.addrGetter.GetLLMAddress(taskModel(t))
	if err != nil {
		return nil, err
	}

	baseURL := &url.URL{
		Scheme: "http",
		Host:   addr,
	}

	var path string
	var reqBody []byte
	switch req := t.Request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion:
		r := req.GetChatCompletion()
		// Convert the model name as we do the same conversion when creating (fine-tuned) models in Ollama.
		// TODO(kenji): Revisit when we supfport fine-tuning models in vLLM.
		r.Model = ollama.ModelName(r.Model)
		for _, msg := range r.Messages {
			if len(msg.LegacyContent) > 0 {
				msg.LegacyContent = ""
			}
		}
		reqBody, err = json.Marshal(r)
		if err != nil {
			return nil, err
		}

		path = completionPath

	case *v1.TaskRequest_Embedding:
		r := req.GetEmbedding()
		reqBody, err = json.Marshal(r)
		if err != nil {
			return nil, err
		}

		path = embeddingPath
	default:
		return nil, fmt.Errorf("unknown request type: %T", req.Request)
	}

	requestURL := baseURL.JoinPath(path).String()
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

func (p *P) sendEngineStatus(stream sender, ready bool) error {
	req := &v1.ProcessTasksRequest{
		Message: &v1.ProcessTasksRequest_EngineStatus{
			EngineStatus: &v1.EngineStatus{
				EngineId: p.engineID,
				ModelIds: p.modelSyncer.ListSyncedModelIDs(),
				SyncStatus: &v1.EngineStatus_SyncStatus{
					InProgressModelIds: p.modelSyncer.ListInProgressModels(),
				},
				Ready: ready,
			},
		},
	}
	if err := stream.Send(req); err != nil {
		return err
	}
	return nil
}

func (p *P) sendHTTPResponse(
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
	if err := p.sendTaskResult(stream, result); err != nil {
		return fmt.Errorf("send http response: %s", err)
	}
	return nil
}

func (p *P) sendServerSentEvent(
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
	if err := p.sendTaskResult(stream, result); err != nil {
		return fmt.Errorf("send server sent event: %s", err)
	}
	return nil
}

func (p *P) sendTaskResult(
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

// IsReady returns true if the processor is ready. If not,
// it returns a message describing why it is not ready.
func (p *P) IsReady() (bool, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lastErr != nil {
		return false, p.lastErr.Error()
	}
	return true, ""
}

func taskModel(t *v1.Task) string {
	switch req := t.Request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion:
		return req.GetChatCompletion().Model
	case *v1.TaskRequest_Embedding:
		return req.GetEmbedding().Model
	default:
		return ""
	}
}

func taskStream(t *v1.Task) bool {
	switch req := t.Request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion:
		return req.GetChatCompletion().Stream
	default:
		return false
	}
}

func isConnClosedErr(err error) bool {
	return err == io.EOF ||
		// connection error type is defined in the gRPC internal transpot package.
		strings.Contains(err.Error(), "error reading from server: EOF")
}
