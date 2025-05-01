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

	stream, err := client.ProcessTasks(streamCtx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.CloseSend() }()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error)
	go func() {
		errCh <- p.sendEngineStatusPeriodically(ctx, stream)
	}()
	go func() {
		errCh <- p.processTasks(ctx, stream)
	}()

	return <-errCh
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

	var wg sync.WaitGroup
	goAwayCh := make(chan struct{})
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
			time.Sleep(goAwayDelay)
			log.Info("Stopping and waiting for all tasks to complete for the go-away request")
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
	case *v1.TaskRequest_ModelActivation:
		return p.activateModel(ctx, stream, t)
	case *v1.TaskRequest_ModelDeactivation:
		return p.deactivateModel(ctx, stream, t)
	case *v1.TaskRequest_GoAway:
		close(goAwayCh)
		return nil
	default:
		return fmt.Errorf("unknown request type: %T", req.Request)
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

	req, err := p.buildRequest(taskCtx, t)
	if err != nil {
		err := fmt.Errorf("build request: %s", err)
		if e := p.sendHTTPResponse(stream, t, &v1.HttpResponse{
			StatusCode: http.StatusInternalServerError,
			Status:     http.StatusText(http.StatusInternalServerError),
			Body:       []byte(err.Error()),
		}); e != nil {
			log.Error(e, "Failed to parse the request type")
		}
		return err
	}

	log.Info("Sending request to the LLM server", "url", req.URL)
	switch req := t.Request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion:
		log.V(1).Info(fmt.Sprintf("Request: %+v", req.GetChatCompletion()))
	case *v1.TaskRequest_Embedding:
		log.V(1).Info(fmt.Sprintf("Request: %+v", req.GetEmbedding()))
	default:
		err := fmt.Errorf("unknown request type: %T", req.Request)
		sendErrResponse(http.StatusInternalServerError, err.Error())
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if stream.Context().Err() != nil {
			return stream.Context().Err()
		}
		code := http.StatusServiceUnavailable
		if !errors.Is(err, context.Canceled) {
			log.Error(err, "Failed to send request to the LLM server")
			code = http.StatusInternalServerError
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

func (p *P) activateModel(
	ctx context.Context,
	stream sender,
	t *v1.Task,
) error {
	log := ctrl.LoggerFrom(ctx)
	req := t.Request.GetModelActivation()
	log.Info("Activating model", "modelID", req.Id)

	// First return the response to the server so that the server can respond back to the client without
	// waiting for the model activation to complete.
	resp := &v1.HttpResponse{
		StatusCode: int32(http.StatusAccepted),
	}
	if err := p.sendHTTPResponse(stream, t, resp); err != nil {
		return err
	}

	if err := p.modelSyncer.PullModel(ctx, req.Id); err != nil {
		return fmt.Errorf("pull model: %s", err)
	}
	if err := p.sendEngineStatus(stream, true); err != nil {
		return fmt.Errorf("send engine status: %s", err)
	}
	return nil
}

func (p *P) deactivateModel(
	ctx context.Context,
	stream sender,
	t *v1.Task,
) error {
	log := ctrl.LoggerFrom(ctx)
	req := t.Request.GetModelDeactivation()
	log.Info("Deactivating model", "modelID", req.Id)

	// First return the response to the server so that the server can respond back to the client without
	// waiting for the model deactivation to complete.
	resp := &v1.HttpResponse{
		StatusCode: int32(http.StatusAccepted),
	}
	if err := p.sendHTTPResponse(stream, t, resp); err != nil {
		return err
	}

	if err := p.modelSyncer.DeleteModel(ctx, req.Id); err != nil {
		return fmt.Errorf("delete model: %s", err)
	}

	if err := p.sendEngineStatus(stream, true); err != nil {
		return fmt.Errorf("send engine status: %s", err)
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
