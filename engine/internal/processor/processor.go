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
	"github.com/llmariner/inference-manager/common/pkg/api"
	"github.com/llmariner/inference-manager/common/pkg/sse"
	"github.com/llmariner/inference-manager/engine/internal/metrics"
	"github.com/llmariner/inference-manager/engine/internal/ollama"
	"github.com/llmariner/inference-manager/engine/internal/runtime"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"google.golang.org/grpc"
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

	goAwayDelay = 3 * time.Second

	// Increase the max receive message size to 100MB to support large tasks (e.g., chat completion with image data).
	maxRecvMsgSize = 100 * 10e6

	runtimeRequestMaxRetries = 3
)

var errGoAway = errors.New("go away")

// ModelSyncer syncs models.
type ModelSyncer interface {
	ListSyncedModels() []runtime.ModelRuntimeInfo
	PullModel(ctx context.Context, modelID string) error
	ListInProgressModels() []runtime.ModelRuntimeInfo
	DeleteModel(ctx context.Context, modelID string) error
}

// AddressGetter gets an address of a model.
type AddressGetter interface {
	GetLLMAddress(modelID string) (string, error)
	BlacklistLLMAddress(modelID, address string) error
}

type stream interface {
	Context() context.Context
	Send(*v1.ProcessTasksRequest) error
	Recv() (*v1.ProcessTasksResponse, error)
	CloseSend() error
}

// ProcessTasksClient is a client for the ProcessTasks RPC.
type ProcessTasksClient interface {
	ProcessTasks(ctx context.Context, opts ...grpc.CallOption) (v1.InferenceWorkerService_ProcessTasksClient, error)
}

type processTasksClientFactory interface {
	Create() (ProcessTasksClient, func(), error)
}

// NewP returns a new processor.
func NewP(
	engineID string,
	clientFactory processTasksClientFactory,
	addrGetter AddressGetter,
	modelSyncer ModelSyncer,
	logger logr.Logger,
	collector metrics.Collector,
	gracefulShutdownTimeout time.Duration,
) *P {
	return &P{
		engineID: engineID,

		clientFactory: clientFactory,

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
	engineID string

	clientFactory processTasksClientFactory

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

	for {
		if err := p.run(ctx); err != nil {
			log.Error(err, "Processor error")
			p.mu.Lock()
			p.lastErr = err
			p.mu.Unlock()

			if errors.Is(err, errGoAway) {
				log.Info("Processor stopped due to go away. Reconnecting immediately")
				continue
			}
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

func (p *P) run(ctx context.Context) error {
	// Use separate context for the stream to gracefully handle the task requests.
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()

	streamCtx = ctrl.LoggerInto(streamCtx, ctrl.LoggerFrom(ctx))
	streamCtx = auth.AppendWorkerAuthorization(streamCtx)

	// Create a new GRPC client so that a new TCP connection is established.
	// This is especially useful when a GoAway request is received and a new connection
	// is needed to be established to other server.
	client, cleanup, err := p.clientFactory.Create()
	if err != nil {
		return err
	}
	defer cleanup()

	stream, err := client.ProcessTasks(streamCtx, grpc.MaxCallRecvMsgSize(maxRecvMsgSize))
	if err != nil {
		return err
	}
	defer func() { _ = stream.CloseSend() }()

	ctx, cancel := context.WithCancel(ctx)

	errCh := make(chan error)
	go func() {
		errCh <- p.sendEngineStatusPeriodically(ctx, stream)
	}()
	go func() {
		errCh <- p.processTasks(ctx, stream)
	}()

	// Wait for the first error from either sendEngineStatusPeriodically or processTasks.
	// Then cancel the context to stop both goroutines.
	err = <-errCh
	p.logger.Error(err, "Processor error")
	cancel()
	<-errCh
	return err
}

func (p *P) sendEngineStatusPeriodically(
	ctx context.Context,
	stream stream,
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
			return ctx.Err()
		case <-time.After(statusReportInterval):
		}
	}
}

func (p *P) processTasks(
	ctx context.Context,
	stream stream,
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

	var (
		wg        sync.WaitGroup
		taskCount atomic.Int32
	)
	goAwayCh := make(chan struct{})
	for {
		select {
		case resp := <-respCh:
			// Create a goroutine to process the task so that we can receive the
			// next task. llm then might process requests in parallel.
			wg.Add(1)
			go func() {
				p.metrics.Add(taskModel(resp.NewTask), 1)
				taskCount.Add(1)
				defer func() {
					taskCount.Add(-1)
					p.metrics.Add(taskModel(resp.NewTask), -1)
					wg.Done()
				}()

				log := log.WithValues("taskID", resp.NewTask.Id)
				log.Info("Started processing task")
				// TODO(kenji): Consider set the context timeout based on the task's deadline.
				if err := p.processTask(ctrl.LoggerInto(ctx, log), stream, resp.NewTask, goAwayCh); errors.Is(err, context.Canceled) {
					log.Info("Canceled task", "reason", err)
				} else if err != nil {
					log.Error(err, "Failed to process task")
				} else {
					log.Info("Completed task")
				}
			}()
		case err := <-errCh:
			return err
		case <-goAwayCh:
			log.Info("Received the go-away request")
			// Add delay for tasks that might be just scheduled to this engine.
			time.Sleep(goAwayDelay)
			log.Info("Stopping and waiting for all tasks to complete for the go-away request", "taskCount", taskCount.Load())
			wg.Wait()
			close(doneCh)
			return errGoAway
		case <-ctx.Done():
			log.Info("Stopping and waiting for all tasks to complete", "grace-period", p.taskGracePeriod)
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
	goAwayCh chan struct{},
) error {
	switch req := t.Request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion, *v1.TaskRequest_Embedding:
		return p.sendRequestToRuntime(ctx, stream, t)
	case *v1.TaskRequest_GoAway:
		return p.goAway(ctx, stream, t, goAwayCh)
	case *v1.TaskRequest_Heartbeat:
		return p.heartbeat(ctx, stream, t)
	default:
		// Do not return an error to be able to release the server without
		// updating the engine.
		return p.handleUnimplemented(ctx, stream, t)
	}
}

func (p *P) sendRequestToRuntime(
	ctx context.Context,
	stream sender,
	t *v1.Task,
) error {
	log := ctrl.LoggerFrom(ctx)

	sendErrResponse := func(code int, body string) {
		if e := p.sendHTTPResponse(stream, t, &v1.HttpResponse{
			StatusCode: int32(code),
			Status:     http.StatusText(code),
			Body:       []byte(body),
		}); e != nil {
			log.Error(e, "Failed to send error response")
		}
	}

	// First pull the model if it is not yet pulled.
	if err := p.modelSyncer.PullModel(ctx, taskModel(t)); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, runtime.ErrRequestCanceled) {
			code = http.StatusServiceUnavailable
		}
		sendErrResponse(code, err.Error())
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

	resp, code, err := p.sendHTTPRequestToRuntime(taskCtx, stream, t, log)
	if err != nil {
		if stream.Context().Err() != nil {
			return stream.Context().Err()
		}
		sendErrResponse(code, fmt.Sprintf("Failed to send request to the LLM server: %s", err))
		return err
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

func (p *P) sendHTTPRequestToRuntime(
	ctx context.Context,
	stream sender,
	t *v1.Task,
	log logr.Logger,
) (*http.Response, int, error) {
	var attempt int
	model := taskModel(t)
	for {
		addr, err := p.addrGetter.GetLLMAddress(model)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}

		req, err := buildRequest(ctx, t, addr, log)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}

		log.Info("Sending request to the LLM server", "url", req.URL)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, http.StatusServiceUnavailable, err
			}

			attempt++
			log.Error(err, "Failed to send request to the LLM server. Blacklisting", "attempt", attempt, "addr", addr)

			// TODO(kenji): Retry only when there are more than one replica for the model.

			if err := p.addrGetter.BlacklistLLMAddress(model, addr); err != nil {
				return nil, http.StatusInternalServerError, err
			}
			if attempt >= runtimeRequestMaxRetries {
				return nil, http.StatusInternalServerError, err
			}
			continue
		}

		// Success.
		return resp, 0, nil
	}
}

func buildRequest(ctx context.Context, t *v1.Task, addr string, log logr.Logger) (*http.Request, error) {
	baseURL := &url.URL{
		Scheme: "http",
		Host:   addr,
	}

	var path string
	var reqBody []byte
	switch req := t.Request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion:
		r := req.GetChatCompletion()
		log.V(1).Info(fmt.Sprintf("Request: %+v", r))
		// Convert the model name as we do the same conversion when creating (fine-tuned) models in Ollama.
		// TODO(kenji): Revisit when we supfport fine-tuning models in vLLM.
		r.Model = ollama.ModelName(r.Model)
		var err error
		reqBody, err = json.Marshal(r)
		if err != nil {
			return nil, err
		}

		reqBody, err = api.ConvertCreateChatCompletionRequestToOpenAI(reqBody)
		if err != nil {
			return nil, err
		}

		path = completionPath
	case *v1.TaskRequest_Embedding:
		r := req.GetEmbedding()
		log.V(1).Info(fmt.Sprintf("Request: %+v", r))

		var err error
		reqBody, err = json.Marshal(r)
		if err != nil {
			return nil, err
		}

		reqBody, err = api.ConvertCreateEmbeddingRequestToOpenAI(reqBody)
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

func (p *P) goAway(
	ctx context.Context,
	stream sender,
	t *v1.Task,
	goAwayCh chan struct{},
) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Processing a GoAway request")

	// Return the response to the server so that the server move to the next step for graceful shutdown.
	resp := &v1.HttpResponse{
		StatusCode: int32(http.StatusAccepted),
	}
	if err := p.sendHTTPResponse(stream, t, resp); err != nil {
		return err
	}

	close(goAwayCh)

	return nil
}

func (p *P) heartbeat(
	ctx context.Context,
	stream sender,
	t *v1.Task,
) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Processing a Heartbeat request")

	// Just return the response to the server.
	resp := &v1.HttpResponse{
		StatusCode: int32(http.StatusOK),
	}
	if err := p.sendHTTPResponse(stream, t, resp); err != nil {
		return err
	}

	return nil
}

func (p *P) handleUnimplemented(
	ctx context.Context,
	stream sender,
	t *v1.Task,
) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Received an unimplemented request", "request", t.Request)

	resp := &v1.HttpResponse{
		StatusCode: int32(http.StatusNotImplemented),
	}
	if err := p.sendHTTPResponse(stream, t, resp); err != nil {
		return err
	}

	return nil
}

func (p *P) sendEngineStatus(stream sender, ready bool) error {
	var models []*v1.EngineStatus_Model
	var syncedModels []string
	ms := p.modelSyncer.ListSyncedModels()
	for _, m := range ms {
		models = append(models, &v1.EngineStatus_Model{
			Id:           m.ID,
			IsReady:      m.Ready,
			GpuAllocated: m.GPU,
		})
		syncedModels = append(syncedModels, m.ID)
	}
	var inProgressModels []string
	ms = p.modelSyncer.ListInProgressModels()
	for _, m := range ms {
		models = append(models, &v1.EngineStatus_Model{
			Id:           m.ID,
			IsReady:      m.Ready,
			GpuAllocated: m.GPU,
		})
		inProgressModels = append(inProgressModels, m.ID)
	}
	req := &v1.ProcessTasksRequest{
		Message: &v1.ProcessTasksRequest_EngineStatus{
			EngineStatus: &v1.EngineStatus{
				EngineId: p.engineID,
				ModelIds: syncedModels,
				SyncStatus: &v1.EngineStatus_SyncStatus{
					InProgressModelIds: inProgressModels,
				},
				Models: models,
				Ready:  ready,
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
		return "n/a"
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
