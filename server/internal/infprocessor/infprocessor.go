package infprocessor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/llmariner/common/pkg/id"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
)

const (
	taskQueueSize = 100
)

type result struct {
	resp *http.Response
	err  error
}

// task is an inference task.
// TODO(kenji): Consider preserving the request context as well.
type task struct {
	ID       string
	TenantID string

	ChatCompletionReq *v1.CreateChatCompletionRequest
	EmbeddingReq      *v1.CreateEmbeddingRequest

	Header http.Header

	setResult  func(*result)
	bodyWriter *pipeReadWriteCloser

	EngineID string

	CreatedAt time.Time
}

func (t *task) model() string {
	if r := t.ChatCompletionReq; r != nil {
		return r.Model
	}
	return t.EmbeddingReq.Model
}

func (t *task) stream() bool {
	if r := t.ChatCompletionReq; r != nil {
		return r.Stream
	}
	return false
}

func (t *task) request() *v1.TaskRequest {
	if r := t.ChatCompletionReq; r != nil {
		return &v1.TaskRequest{
			Request: &v1.TaskRequest_ChatCompletion{
				ChatCompletion: t.ChatCompletionReq,
			},
		}
	}

	return &v1.TaskRequest{
		Request: &v1.TaskRequest_Embedding{
			Embedding: t.EmbeddingReq,
		},
	}
}

// newTaskQueue creates a new task queue.
func newTaskQueue() *taskQueue {
	return &taskQueue{
		tasks: make(chan *task, taskQueueSize),
	}
}

// taskQueue is a queue for inference tasks.
type taskQueue struct {
	tasks    chan *task
	numTasks atomic.Int32
}

// Enqueue inserts a task into the queue.
func (q *taskQueue) Enqueue(t *task) {
	q.numTasks.Add(1)
	q.tasks <- t
}

// Dequeue removes a task from the queue.
func (q *taskQueue) Dequeue(ctx context.Context) (*task, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case t := <-q.tasks:
		q.numTasks.Add(-1)
		return t, nil
	}
}

type engineRouter interface {
	GetEnginesForModel(ctx context.Context, modelID, tenantID string) ([]string, error)
	AddOrUpdateEngine(engineID, tenantID string, modelIDs []string)
	DeleteEngine(engineID, tenantID string)
}

// NewP creates a new processor.
func NewP(engineRouter engineRouter, logger logr.Logger) *P {
	return &P{
		queue:               newTaskQueue(),
		engineRouter:        engineRouter,
		engines:             map[string]map[string]*engine{},
		inProgressTasksByID: map[string]*task{},
		logger:              logger.WithName("processor"),
		taskTimeout:         30 * time.Second,
		retryDelay:          3 * time.Second,
	}
}

type engineCommunicator interface {
	Send(*v1.ProcessTasksResponse) error
	Recv() (*v1.ProcessTasksRequest, error)
}

type engine struct {
	id string

	srv engineCommunicator

	modelIDs           []string
	inProgressModelIDs []string
}

// P processes inference tasks.
type P struct {
	queue *taskQueue

	engineRouter engineRouter

	// engines is a map from tenant ID and engine ID to engine.
	engines             map[string]map[string]*engine
	inProgressTasksByID map[string]*task
	mu                  sync.Mutex

	logger logr.Logger

	taskTimeout time.Duration
	retryDelay  time.Duration
}

// Run runs the processor.
func (p *P) Run(ctx context.Context) error {
	p.logger.Info("Starting the processor...")
	for {
		t, err := p.queue.Dequeue(ctx)
		if err != nil {
			return err
		}
		if err := p.scheduleTask(ctx, t); err != nil {
			t.setResult(&result{err: err})
		}
	}
}

func (p *P) scheduleTask(ctx context.Context, t *task) error {
	engineIDs, err := p.engineRouter.GetEnginesForModel(ctx, t.model(), t.TenantID)
	if err != nil {
		return fmt.Errorf("find an engine to route the request: %s", err)
	}

	engine, err := p.findLeastLoadedEngine(engineIDs, t.TenantID)
	if err != nil {
		return fmt.Errorf("find the least loaded engine: %s", err)
	}
	p.logger.Info("Scheduling the task", "task", t.ID, "engine", engine.id)

	p.mu.Lock()
	t.EngineID = engine.id
	p.inProgressTasksByID[t.ID] = t
	p.mu.Unlock()

	// TODO(kenji): Currently we can directly send from here, but later this needs to be changed
	// when there is more than one instance of inference-manager-server.
	header := map[string]*v1.HeaderValue{}
	for k, vs := range t.Header {
		header[k] = &v1.HeaderValue{Values: vs}
	}
	if err := engine.srv.Send(&v1.ProcessTasksResponse{
		NewTask: &v1.Task{
			Id: t.ID,
			// TODO(kenji): Remove once all the engines are updated to a newer
			// version that don't use the deprecated field.
			DeprecatedChatCompletionRequest: t.ChatCompletionReq,
			Request:                         t.request(),
			Header:                          header,
		},
	}); err != nil {
		p.mu.Lock()
		delete(p.inProgressTasksByID, t.ID)
		p.mu.Unlock()
		return fmt.Errorf("failed to send the task: %s", err)
	}
	return nil
}

// findLeastLoadedEngine finds the least loaded engine from the given engine IDs.
func (p *P) findLeastLoadedEngine(engineIDs []string, tenantID string) (*engine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	numTasksByEngine := map[string]int{}
	for _, t := range p.inProgressTasksByID {
		numTasksByEngine[t.EngineID]++
	}

	var minTasks int
	var leastLoaded string
	for _, engineID := range engineIDs {
		n := numTasksByEngine[engineID]
		if leastLoaded == "" || n < minTasks {
			minTasks = n
			leastLoaded = engineID
		}
	}

	engines := p.engines[tenantID]
	if len(engines) == 0 {
		return nil, fmt.Errorf("no engine found")
	}
	engine, ok := engines[leastLoaded]
	if !ok {
		return nil, fmt.Errorf("engine not found: %s", leastLoaded)
	}

	return engine, nil
}

// SendChatCompletionTask sends a chat completion task.
func (p *P) SendChatCompletionTask(
	ctx context.Context,
	tenantID string,
	req *v1.CreateChatCompletionRequest,
	header http.Header,
) (*http.Response, error) {
	return p.sendTask(ctx, tenantID, req, nil, header, p.logger.WithName("chat"))
}

// SendEmbeddingTask sends an embedding task.
func (p *P) SendEmbeddingTask(
	ctx context.Context,
	tenantID string,
	req *v1.CreateEmbeddingRequest,
	header http.Header,
) (*http.Response, error) {
	return p.sendTask(ctx, tenantID, nil, req, header, p.logger.WithName("embedded"))
}

func (p *P) sendTask(
	ctx context.Context,
	tenantID string,
	chatCompletionReq *v1.CreateChatCompletionRequest,
	embeddingReq *v1.CreateEmbeddingRequest,
	header http.Header,
	logger logr.Logger,
) (*http.Response, error) {
	taskID, err := id.GenerateID("inf_", 24)
	if err != nil {
		return nil, fmt.Errorf("generate task ID: %s", err)
	}
	log := logger.WithValues("id", taskID)
	done := make(chan *result, 1)
	task := &task{
		ID:                taskID,
		TenantID:          tenantID,
		Header:            header,
		ChatCompletionReq: chatCompletionReq,
		EmbeddingReq:      embeddingReq,
		CreatedAt:         time.Now(),
		setResult: func(r *result) {
			// done channel has one buffer, so at least one result
			// will be received. If the channel gets an additional error,
			// for example, the engine is removed after getting
			// the initial error, it will be ignored.
			select {
			case done <- r:
			default:
			}
		},
	}

	p.queue.Enqueue(task)

	log.V(1).Info("Waiting to receive an initial response to the task")
	for {
		select {
		case <-ctx.Done():
			// When a task result comes, the processor still attempts to
			// write a response/error to a channel. We need to read
			// from the channel to avoid a goroutine leak.
			go func() {
				r := <-done
				if r.resp != nil {
					_, _ = io.Copy(io.Discard, r.resp.Body)
					_ = r.resp.Body.Close()
				}
				if r.err != nil {
					log.Error(r.err, "Task failed")
				}
			}()
			return nil, ctx.Err()
		case r := <-done:
			if r.err != nil {
				if p.taskTimeout > 0 && time.Since(task.CreatedAt.Add(p.retryDelay)) < p.taskTimeout {
					_ = time.AfterFunc(p.retryDelay, func() { p.queue.Enqueue(task) })
					log.V(2).Info("Requeued the task", "reason", err, "delay", p.retryDelay)
					continue
				}
				log.Error(r.err, "Failed to process the task")
				return nil, r.err
			}
			if r.resp != nil {
				log.Info("Received an initial response", "code", r.resp.StatusCode, "status", r.resp.Status)
				return r.resp, nil
			}
			err := fmt.Errorf("unexpected empty result")
			log.Error(err, "Failed to process the task")
			return nil, err
		}
	}
}

// AddOrUpdateEngineStatus adds or updates the engine status.
func (p *P) AddOrUpdateEngineStatus(
	srv engineCommunicator,
	engineStatus *v1.EngineStatus,
	clusterInfo *auth.ClusterInfo,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	engines, ok := p.engines[clusterInfo.TenantID]
	if !ok {
		engines = map[string]*engine{}
		p.engines[clusterInfo.TenantID] = engines
	}
	log := p.logger.WithValues("engineID", engineStatus.EngineId)

	e, ok := engines[engineStatus.EngineId]
	if !ok {
		e = &engine{
			id:  engineStatus.EngineId,
			srv: srv,
		}
		engines[engineStatus.EngineId] = e
		log.Info("Registered new engine")
	}
	e.modelIDs = engineStatus.ModelIds
	// Check if the sync status is set for backward compatibility.
	if s := engineStatus.SyncStatus; s != nil {
		e.inProgressModelIDs = s.InProgressModelIds
	}
	log.V(5).Info("Updated engine status", "models", e.modelIDs, "in-progress", e.inProgressModelIDs, "ready", engineStatus.Ready)

	/*
		   // TODO(kenji): Add this back once the engine is updated.
		if engineStatus.Ready {
			p.engineRouter.AddOrUpdateEngine(engineStatus.EngineId, clusterInfo.TenantID, engineStatus.ModelIds)
		} else {
			p.engineRouter.DeleteEngine(engineStatus.EngineId, clusterInfo.TenantID)
			log.Info("Removed engine from the router", "reason", "engine not ready")
		}
	*/
	p.engineRouter.AddOrUpdateEngine(engineStatus.EngineId, clusterInfo.TenantID, engineStatus.ModelIds)
}

// RemoveEngine removes the engine.
func (p *P) RemoveEngine(engineID string, clusterInfo *auth.ClusterInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Cancel in-progress tasks allocated to this engine.
	for _, t := range p.inProgressTasksByID {
		if t.EngineID != engineID {
			continue
		}
		p.logger.Info("Canceled task", "reason", "engine removed", "engine", engineID, "task", t.ID)
		delete(p.inProgressTasksByID, t.ID)
		t.setResult(&result{err: fmt.Errorf("engine %s is removed", engineID)})
		if err := t.bodyWriter.closeWrite(); err != nil {
			p.logger.Error(err, "Failed to close the body writer when engine removed", "engine", t.EngineID, "task", t.ID)
		}
	}

	engines, ok := p.engines[clusterInfo.TenantID]
	if !ok {
		return
	}
	p.engineRouter.DeleteEngine(engineID, clusterInfo.TenantID)
	delete(engines, engineID)
}

// ProcessTaskResult processes the task result.
func (p *P) ProcessTaskResult(
	taskResult *v1.TaskResult,
	clusterInfo *auth.ClusterInfo,
) error {
	taskID := taskResult.TaskId

	p.mu.Lock()
	t, ok := p.inProgressTasksByID[taskID]
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	completed, err := p.writeTaskResultToChan(t, taskResult)
	if err != nil {
		return fmt.Errorf("write task result to chan: %s", err)
	}

	if !completed {
		return nil
	}

	p.logger.Info("Completed task", "task", taskID)
	if err := t.bodyWriter.closeWrite(); err != nil {
		return fmt.Errorf("close the body writer: %s", err)
	}

	p.mu.Lock()
	delete(p.inProgressTasksByID, taskID)
	p.mu.Unlock()
	return nil
}

// writeTaskResultToChan writes the task result to the channel.
//
// The return bool value indicates whether the task is completed.
func (p *P) writeTaskResultToChan(
	t *task,
	taskResult *v1.TaskResult,
) (bool, error) {
	switch msg := taskResult.Message.(type) {
	case *v1.TaskResult_HttpResponse:
		resp := msg.HttpResponse

		if resp.StatusCode == http.StatusServiceUnavailable {
			p.mu.Lock()
			delete(p.inProgressTasksByID, t.ID)
			p.mu.Unlock()
			t.setResult(&result{err: fmt.Errorf("engine %s is unavailable", t.EngineID)})
			return false, nil
		}

		header := http.Header{}
		for k, vs := range resp.Header {
			for _, v := range vs.Values {
				header.Add(k, v)
			}
		}

		prwc := newPipeReadWriteCloser()
		t.bodyWriter = prwc
		t.setResult(&result{
			resp: &http.Response{
				StatusCode: int(resp.StatusCode),
				Status:     resp.Status,
				Header:     header,
				Body:       prwc,
			},
		})

		if d := resp.Body; len(d) > 0 {
			if _, err := prwc.Write(d); err != nil {
				// Gracefully handle the error as it can happen when the request is canceled and
				// the body writer is closed by the client.
				p.logger.Error(err, "Failed to write the body writer")
			}
		}

		isErr := resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest
		return isErr || (!t.stream()), nil
	case *v1.TaskResult_ServerSentEvent:
		if !t.stream() {
			return false, fmt.Errorf("unexpected chunked response for non-streaming request")
		}

		if d := msg.ServerSentEvent.Data; len(d) > 0 {
			if _, err := t.bodyWriter.Write(d); err != nil {
				// Gracefully handle the error as it can happen when the request is canceled and
				// the body writer is closed by the client.
				p.logger.Error(err, "Failed to write the body writer")
			}
		}

		return msg.ServerSentEvent.IsLastEvent, nil
	default:
		return false, fmt.Errorf("unexpected message type: %T", msg)
	}
}

// NumQueuedTasks returns the number of queued tasks.
func (p *P) NumQueuedTasks() int32 {
	return p.queue.numTasks.Load()
}

// NumInProgressTasks returns the number of in-progress tasks.
func (p *P) NumInProgressTasks() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.inProgressTasksByID)
}

// MaxInProgressTaskDuration returns the maximum duration of in-progress tasks.
func (p *P) MaxInProgressTaskDuration() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	var maxD time.Duration
	for _, t := range p.inProgressTasksByID {
		d := time.Since(t.CreatedAt)
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

// NumEnginesByTenantID returns the number of engines by tenant ID.
func (p *P) NumEnginesByTenantID() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := make(map[string]int, len(p.engines))
	for tenantID, engines := range p.engines {
		m[tenantID] = len(engines)
	}
	return m
}

// TaskStatus is the status of a task.
type TaskStatus struct {
	ID      string `json:"id"`
	ModelID string `json:"modelId"`
}

// EngineStatus is the status of an engine.
type EngineStatus struct {
	RegisteredModelIDs []string      `json:"registeredModelIds"`
	InProgressModelIDs []string      `json:"inProgressModelIds"`
	Tasks              []*TaskStatus `json:"tasks"`
}

// TenantStatus is the status of a tenant.
type TenantStatus struct {
	Engines map[string]*EngineStatus `json:"engines"`
}

// Status is the status of the processor.
type Status struct {
	Tenants map[string]*TenantStatus `json:"tenants"`
}

// DumpStatus dumps the status of the processor.
func (p *P) DumpStatus() *Status {
	p.mu.Lock()
	defer p.mu.Unlock()

	status := &Status{
		Tenants: map[string]*TenantStatus{},
	}

	for tenantID, engines := range p.engines {
		t, ok := status.Tenants[tenantID]
		if !ok {
			t = &TenantStatus{
				Engines: map[string]*EngineStatus{},
			}
			status.Tenants[tenantID] = t
		}

		for id, e := range engines {
			t.Engines[id] = &EngineStatus{
				RegisteredModelIDs: e.modelIDs,
				InProgressModelIDs: e.inProgressModelIDs,
			}
		}
	}

	for _, task := range p.inProgressTasksByID {
		t, ok := status.Tenants[task.TenantID]
		if !ok {
			t = &TenantStatus{
				Engines: map[string]*EngineStatus{},
			}
			status.Tenants[task.TenantID] = t
		}
		e, ok := t.Engines[task.EngineID]
		if !ok {
			e = &EngineStatus{}
			t.Engines[task.EngineID] = e
		}

		e.Tasks = append(e.Tasks, &TaskStatus{
			ID:      task.ID,
			ModelID: task.model(),
		})
	}

	// Sort the modelIDs and task IDs for deterministic output.
	for _, engines := range status.Tenants {
		for _, e := range engines.Engines {
			sort.Strings(e.RegisteredModelIDs)
			sort.Strings(e.InProgressModelIDs)
			sort.Slice(e.Tasks, func(i, j int) bool {
				return e.Tasks[i].ID < e.Tasks[j].ID
			})
		}
	}

	return status
}
