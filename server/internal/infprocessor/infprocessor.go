package infprocessor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/llmariner/common/pkg/id"
	v1 "github.com/llmariner/inference-manager/api/v1"
)

const (
	taskQueueSize = 100
)

type resultOrError struct {
	result *v1.TaskResult
	err    error
}

// task is an inference task.
// TODO(kenji): Consider preserving the request context as well.
type task struct {
	id       string
	tenantID string

	chatCompletionReq *v1.CreateChatCompletionRequest
	embeddingReq      *v1.CreateEmbeddingRequest

	header http.Header

	resultCh chan *resultOrError

	engineID string

	createdAt time.Time
}

func (t *task) model() string {
	if r := t.chatCompletionReq; r != nil {
		return r.Model
	}
	return t.embeddingReq.Model
}

func (t *task) stream() bool {
	if r := t.chatCompletionReq; r != nil {
		return r.Stream
	}
	return false
}

func (t *task) request() *v1.TaskRequest {
	if r := t.chatCompletionReq; r != nil {
		return &v1.TaskRequest{
			Request: &v1.TaskRequest_ChatCompletion{
				ChatCompletion: t.chatCompletionReq,
			},
		}
	}

	return &v1.TaskRequest{
		Request: &v1.TaskRequest_Embedding{
			Embedding: t.embeddingReq,
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
	AddOrUpdateEngine(engineID, tenantID string, modelIDs []string)
	DeleteEngine(engineID, tenantID string)
	GetEnginesForModel(ctx context.Context, modelID, tenantID string) ([]string, error)
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

// TaskSender sends a new task to the engine.
type TaskSender interface {
	Send(*v1.ProcessTasksResponse) error
}

type engine struct {
	id string

	taskSender TaskSender

	modelIDs           []string
	inProgressModelIDs []string

	// isLocal indicates whether the engine is connected to this local server or not.
	isLocal bool
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
			p.writeResultToTask(t.id, &resultOrError{err: err})
		}
	}
}

func (p *P) scheduleTask(ctx context.Context, t *task) error {
	engineIDs, err := p.engineRouter.GetEnginesForModel(ctx, t.model(), t.tenantID)
	if err != nil {
		return fmt.Errorf("find an engine to route the request: %s", err)
	}

	engine, err := p.findMostPreferredtEngine(engineIDs, t.tenantID)
	if err != nil {
		return fmt.Errorf("find the least loaded engine: %s", err)
	}
	p.logger.Info("Scheduling the task", "task", t.id, "engine", engine.id)

	if err := p.assignTaskToEngine(t, engine.id); err != nil {
		return fmt.Errorf("assign the task to the engine: %s", err)
	}

	header := map[string]*v1.HeaderValue{}
	for k, vs := range t.header {
		header[k] = &v1.HeaderValue{Values: vs}
	}
	if err := engine.taskSender.Send(&v1.ProcessTasksResponse{
		NewTask: &v1.Task{
			Id:      t.id,
			Request: t.request(),
			Header:  header,
		},
	}); err != nil {
		return fmt.Errorf("send the task: %s", err)
	}
	return nil
}

func (p *P) assignTaskToEngine(t *task, engineID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Check again if the engine still exists as it might have been just removed.
	if _, ok := p.engines[t.tenantID][engineID]; !ok {
		p.mu.Unlock()
		return fmt.Errorf("engine is already removed")
	}
	t.engineID = engineID
	return nil
}

func (p *P) unassignTaskFromEngine(t *task) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t.engineID = ""
}

// findMostPreferredtEngine finds the most preferred engine for scheduling a task from a given engines.
func (p *P) findMostPreferredtEngine(engineIDs []string, tenantID string) (*engine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	engines := p.engines[tenantID]
	if len(engines) == 0 {
		return nil, fmt.Errorf("no engine found")
	}

	// Prefer locally connected engines to remote engines.
	var localEngines, remoteEngines []*engine
	for _, engineID := range engineIDs {
		e, ok := engines[engineID]
		if !ok {
			return nil, fmt.Errorf("engine not found: %s", engineID)
		}
		if e.isLocal {
			localEngines = append(localEngines, e)
		} else {
			remoteEngines = append(remoteEngines, e)
		}
	}

	if len(localEngines) > 0 {
		return p.findLeastLoadedEngine(localEngines)
	}

	return p.findLeastLoadedEngine(remoteEngines)
}

func (p *P) findLeastLoadedEngine(engines []*engine) (*engine, error) {
	numTasksByEngine := map[string]int{}
	for _, t := range p.inProgressTasksByID {
		numTasksByEngine[t.engineID]++
	}

	var minTasks int
	var leastLoaded *engine
	for _, e := range engines {
		n := numTasksByEngine[e.id]
		if leastLoaded == nil || n < minTasks {
			minTasks = n
			leastLoaded = e
		}
	}

	if leastLoaded == nil {
		return nil, fmt.Errorf("no engine found")
	}

	return leastLoaded, nil
}

// SendAndProcessTask sends a task and processes the results.
func (p *P) SendAndProcessTask(
	ctx context.Context,
	origTask *v1.Task,
	tenantID string,
	processResult func(*v1.TaskResult) error,
) error {
	log := p.logger.WithValues("id", origTask.Id)

	header := http.Header{}
	for k, vs := range origTask.Header {
		for _, v := range vs.Values {
			header.Add(k, v)
		}
	}
	resultCh := make(chan *resultOrError)
	task := &task{
		id:                origTask.Id,
		tenantID:          tenantID,
		header:            header,
		chatCompletionReq: origTask.Request.GetChatCompletion(),
		embeddingReq:      origTask.Request.GetEmbedding(),
		createdAt:         time.Now(),
		resultCh:          resultCh,
	}

	return p.enqueueAndProcessTask(ctx, task, processResult, log)
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

	resultCh := make(chan *resultOrError)
	task := &task{
		id:                taskID,
		tenantID:          tenantID,
		header:            header,
		chatCompletionReq: chatCompletionReq,
		embeddingReq:      embeddingReq,
		createdAt:         time.Now(),
		resultCh:          resultCh,
	}

	log.V(1).Info("Waiting to receive an initial response to the task")

	respCh := make(chan *http.Response)
	errCh := make(chan error)

	go func() {
		bodyWriter := newPipeReadWriteCloser()
		defer func() {
			if err := bodyWriter.closeWrite(); err != nil {
				log.Error(err, "Failed to close the body writer")
			}
		}()

		f := func(r *v1.TaskResult) error {
			return writeTaskResultToHTTPRespCh(r, bodyWriter, respCh, log)
		}
		if err := p.enqueueAndProcessTask(ctx, task, f, log); err != nil {
			// Use a non-blocking write as an error might happen after the message is respCh is written
			// and this function ends.
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	select {
	case resp := <-respCh:
		return resp, nil
	case err := <-errCh:
		return nil, err
	}
}

type retriableError struct {
	error
}

func (p *P) enqueueAndProcessTask(
	ctx context.Context,
	task *task,
	processResult func(*v1.TaskResult) error,
	log logr.Logger,
) error {
	p.mu.Lock()
	p.inProgressTasksByID[task.id] = task
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.inProgressTasksByID, task.id)
		p.mu.Unlock()

		// Drain the result channel as a result might have been written just before
		// the task is deleted from the in-progress tasks map.
		for {
			select {
			case <-task.resultCh:
			default:
				return
			}
		}
	}()

	p.queue.Enqueue(task)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r := <-task.resultCh:
			var err error
			if r.err != nil {
				err = retriableError{error: r.err}
			} else {
				err = processResult(r.result)
			}

			if err != nil {
				// Retry if possible.
				if !p.canRetry(task, err) {
					return err
				}

				p.unassignTaskFromEngine(task)

				_ = time.AfterFunc(p.retryDelay, func() { p.queue.Enqueue(task) })
				log.V(2).Info("Requeued the task", "reason", err, "delay", p.retryDelay)
				continue
			}

			// Check if the task is completed.
			completed, err := isTaskCompleted(task, r.result)
			if err != nil {
				return fmt.Errorf("check if the task is completed: %s", err)
			}
			if completed {
				log.Info("Completed task")
				return nil
			}
		}
	}
}

func (p *P) canRetry(t *task, err error) bool {
	if !errors.As(err, &retriableError{}) {
		return false
	}

	return p.taskTimeout > 0 && time.Since(t.createdAt.Add(p.retryDelay)) < p.taskTimeout
}

// AddOrUpdateEngineStatus adds or updates the engine status.
func (p *P) AddOrUpdateEngineStatus(
	taskSender TaskSender,
	engineStatus *v1.EngineStatus,
	tenantID string,
	isLocal bool,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	engines, ok := p.engines[tenantID]
	if !ok {
		engines = map[string]*engine{}
		p.engines[tenantID] = engines
	}
	log := p.logger.WithValues("engineID", engineStatus.EngineId)

	e, ok := engines[engineStatus.EngineId]
	if !ok {
		e = &engine{
			id:         engineStatus.EngineId,
			taskSender: taskSender,
			isLocal:    isLocal,
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

	if engineStatus.Ready {
		p.engineRouter.AddOrUpdateEngine(e.id, tenantID, e.modelIDs)
	} else {
		p.engineRouter.DeleteEngine(engineStatus.EngineId, tenantID)
		log.Info("Removed engine from the router", "reason", "engine not ready")
	}
}

// RemoveEngine removes the engine.
func (p *P) RemoveEngine(engineID string, tenantID string) {
	p.mu.Lock()

	// Cancel in-progress tasks allocated to this engine.
	var taskIDs []string
	for _, t := range p.inProgressTasksByID {
		if t.engineID != engineID {
			continue
		}
		taskIDs = append(taskIDs, t.id)
	}

	engines, ok := p.engines[tenantID]
	if ok {
		p.engineRouter.DeleteEngine(engineID, tenantID)
		delete(engines, engineID)
	}
	p.mu.Unlock()

	// Write the result outside of the lock.
	for _, taskID := range taskIDs {
		p.logger.Info("Canceled task", "reason", "engine removed", "engine", engineID, "task", taskID)
		p.writeResultToTask(taskID, &resultOrError{err: fmt.Errorf("engine %s is removed", engineID)})
	}
}

// ProcessTaskResult processes the task result.
func (p *P) ProcessTaskResult(taskResult *v1.TaskResult) {
	p.writeResultToTask(taskResult.TaskId, &resultOrError{result: taskResult})
}

func (p *P) writeResultToTask(taskID string, r *resultOrError) {
	p.mu.Lock()
	t, ok := p.inProgressTasksByID[taskID]
	p.mu.Unlock()

	if !ok {
		// The task has already been removed from the in-progress tasks map due to an error or context cancel.
		return
	}

	t.resultCh <- r
}

// LocalEngines returns the local engine statuses grouped by tenant ID.
func (p *P) LocalEngines() map[string][]*v1.EngineStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := map[string][]*v1.EngineStatus{}
	for tenantID, es := range p.engines {
		var engines []*v1.EngineStatus
		for _, e := range es {
			if !e.isLocal {
				continue
			}

			engines = append(engines, &v1.EngineStatus{
				EngineId: e.id,
				ModelIds: e.modelIDs,
				SyncStatus: &v1.EngineStatus_SyncStatus{
					InProgressModelIds: e.inProgressModelIDs,
				},
				Ready: true,
			})
		}
		result[tenantID] = engines
	}
	return result
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
		d := time.Since(t.createdAt)
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
	IsLocal            bool          `json:"isLocal"`
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
				IsLocal:            e.isLocal,
			}
		}
	}

	for _, task := range p.inProgressTasksByID {
		t, ok := status.Tenants[task.tenantID]
		if !ok {
			t = &TenantStatus{
				Engines: map[string]*EngineStatus{},
			}
			status.Tenants[task.tenantID] = t
		}
		e, ok := t.Engines[task.engineID]
		if !ok {
			e = &EngineStatus{}
			t.Engines[task.engineID] = e
		}

		e.Tasks = append(e.Tasks, &TaskStatus{
			ID:      task.id,
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

// writeTaskResultToHTTPRespch processes a task result and write an http.Response to a given channel.
// The body of the response is also written to the bodyWriter.
func writeTaskResultToHTTPRespCh(
	result *v1.TaskResult,
	bodyWriter *pipeReadWriteCloser,
	respCh chan<- *http.Response,
	log logr.Logger,
) error {
	switch msg := result.Message.(type) {
	case *v1.TaskResult_HttpResponse:
		resp := msg.HttpResponse

		if resp.StatusCode == http.StatusServiceUnavailable {
			return retriableError{error: fmt.Errorf("engine is unavailable")}
		}

		header := http.Header{}
		for k, vs := range resp.Header {
			for _, v := range vs.Values {
				header.Add(k, v)
			}
		}

		log.Info("Received an initial response", "code", resp.StatusCode, "status", resp.Status)

		respCh <- &http.Response{
			StatusCode: int(resp.StatusCode),
			Status:     resp.Status,
			Header:     header,
			Body:       bodyWriter,
		}

		if d := resp.Body; len(d) > 0 {
			if _, err := bodyWriter.Write(d); err != nil {
				// Gracefully handle the error as it can happen when the request is canceled and
				// the body writer is closed by the client.
				log.Error(err, "Failed to write the body writer")
			}
		}
	case *v1.TaskResult_ServerSentEvent:
		if d := msg.ServerSentEvent.Data; len(d) > 0 {
			if _, err := bodyWriter.Write(d); err != nil {
				// Gracefully handle the error as it can happen when the request is canceled and
				// the body writer is closed by the client.
				log.Error(err, "Failed to write the body writer")
			}
		}
	default:
		return fmt.Errorf("unexpected message type: %T", msg)
	}
	return nil
}

// isTaskCompleted returns whether the task is completed.
func isTaskCompleted(t *task, taskResult *v1.TaskResult) (bool, error) {
	switch msg := taskResult.Message.(type) {
	case *v1.TaskResult_HttpResponse:
		// A non-streaming request is considered completed when the initial response is received.
		if !t.stream() {
			return true, nil
		}

		code := msg.HttpResponse.StatusCode
		switch {
		case code == http.StatusServiceUnavailable:
			// Consider that it is not completed as the task might be retried.
			return false, nil
		case code < http.StatusOK || code >= http.StatusBadRequest:
			// The task completed when it receives an error response.
			return true, nil
		default:
			return false, nil
		}
	case *v1.TaskResult_ServerSentEvent:
		return msg.ServerSentEvent.IsLastEvent, nil
	default:
		return false, fmt.Errorf("unexpected message type: %T", msg)
	}
}
