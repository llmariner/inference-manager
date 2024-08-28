package infprocessor

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/llm-operator/common/pkg/id"
	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
)

const (
	taskQueueSize = 100
)

// NewTask creates a new task.
func NewTask(
	tenantID string,
	req *v1.CreateChatCompletionRequest,
	header http.Header,
) (*Task, error) {
	taskID, err := id.GenerateID("inf_", 24)
	if err != nil {
		return nil, fmt.Errorf("generate task ID: %s", err)
	}

	return &Task{
		ID:        taskID,
		TenantID:  tenantID,
		Req:       req,
		Header:    header,
		RespCh:    make(chan *http.Response),
		ErrCh:     make(chan error),
		CreatedAt: time.Now(),
	}, nil
}

// Task is an inference task.
// TODO(kenji): Consider preserving the request context as well.
type Task struct {
	ID       string
	TenantID string

	Req    *v1.CreateChatCompletionRequest
	Header http.Header
	RespCh chan *http.Response
	ErrCh  chan error

	bodyWriter pipeReadWriteCloser

	EngineID string

	CreatedAt time.Time
}

// WaitForCompletion waits for the completion of the task.
func (t *Task) WaitForCompletion(ctx context.Context) (*http.Response, error) {
	log.Printf("Waiting for the completion of the task (ID: %q)\n", t.ID)
	select {
	case <-ctx.Done():
		// When a task result comes, the processor still attempts to
		// write a response/error to a channel. We need to read
		// from the channel to avoid a goroutine leak.
		go t.discardResp()
		return nil, ctx.Err()
	case resp := <-t.RespCh:
		log.Printf("Received an initial response: taskID=%s, statusCode=%d, status=%q\n", t.ID, resp.StatusCode, resp.Status)
		return resp, nil
	case err := <-t.ErrCh:
		log.Printf("Task (ID: %q) failed: %s\n", t.ID, err)
		return nil, err
	}
}

func (t *Task) discardResp() {
	select {
	case resp := <-t.RespCh:
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	case err := <-t.ErrCh:
		log.Printf("Task (ID: %q) failed: %s\n", t.ID, err)
	}
}

// NewTaskQueue creates a new task queue.
func NewTaskQueue() *TaskQueue {
	return &TaskQueue{
		tasks: make(chan *Task, taskQueueSize),
	}
}

// TaskQueue is a queue for inference tasks.
type TaskQueue struct {
	tasks    chan *Task
	numTasks atomic.Int32
}

// Enqueue inserts a task into the queue.
func (q *TaskQueue) Enqueue(t *Task) {
	q.numTasks.Add(1)
	q.tasks <- t
}

// Dequeue removes a task from the queue.
func (q *TaskQueue) Dequeue(ctx context.Context) (*Task, error) {
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
func NewP(queue *TaskQueue, engineRouter engineRouter) *P {
	return &P{
		queue:               queue,
		engineRouter:        engineRouter,
		engines:             map[string]map[string]*engine{},
		inProgressTasksByID: map[string]*Task{},
	}
}

type engineCommunicator interface {
	Send(*v1.ProcessTasksResponse) error
	Recv() (*v1.ProcessTasksRequest, error)
}

type engine struct {
	srv engineCommunicator

	modelIDs           []string
	inProgressModelIDs []string
}

// P processes inference tasks.
type P struct {
	queue *TaskQueue

	engineRouter engineRouter

	// engines is a map from tenant ID and engine ID to engine.
	engines             map[string]map[string]*engine
	inProgressTasksByID map[string]*Task
	mu                  sync.Mutex
}

// Run runs the processor.
func (p *P) Run(ctx context.Context) error {
	for {
		t, err := p.queue.Dequeue(ctx)
		if err != nil {
			return err
		}
		p.scheduleTask(ctx, t)
	}
}

func (p *P) scheduleTask(ctx context.Context, t *Task) {
	engineIDs, err := p.engineRouter.GetEnginesForModel(ctx, t.Req.Model, t.TenantID)
	if err != nil {
		t.ErrCh <- fmt.Errorf("find pod to route the request: %s", err)
		return
	}

	engineID := p.findLeastLoadedEngine(engineIDs)

	log.Printf("Scheduling the task (ID: %q) to Inference Manager Engine (ID: %q)\n", t.ID, engineID)
	engines := p.engines[t.TenantID]
	if len(engines) == 0 {
		t.ErrCh <- fmt.Errorf("no engine found")
		return
	}
	engine, ok := engines[engineID]
	if !ok {
		t.ErrCh <- fmt.Errorf("engine not found: %s", engineID)
		return
	}

	p.mu.Lock()
	t.EngineID = engineID
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
			Id:      t.ID,
			Request: t.Req,
			Header:  header,
		},
	}); err != nil {
		t.ErrCh <- fmt.Errorf("failed to send the task: %s", err)
		return
	}
}

// findLeastLoadedEngine finds the least loaded engine from the given engine IDs.
func (p *P) findLeastLoadedEngine(engineIDs []string) string {
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

	return leastLoaded
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

	e, ok := engines[engineStatus.EngineId]
	if !ok {
		e = &engine{
			srv: srv,
		}
		engines[engineStatus.EngineId] = e
		log.Printf("Registered new engine: %s\n", engineStatus.EngineId)
	}
	e.modelIDs = engineStatus.ModelIds
	// Check if the sync status is set for backward compatibility.
	if s := engineStatus.SyncStatus; s != nil {
		e.inProgressModelIDs = s.InProgressModelIds
	}

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
		// Write to the error channel in a goroutine to avoid channel block while
		// acquiring the lock.
		go func(t *Task) {
			t.ErrCh <- fmt.Errorf("engine %s is removed", engineID)
		}(t)
		delete(p.inProgressTasksByID, t.ID)
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

	log.Printf("Completed task: ID=%s\n", taskID)
	if err := t.bodyWriter.closeWrite(); err != nil {
		return fmt.Errorf("close the body writer: %s", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.inProgressTasksByID, taskID)

	return nil
}

// writeTaskResultToChan writes the task result to the channel.
//
// The return bool value indicates whether the task is completed.
func (p *P) writeTaskResultToChan(
	t *Task,
	taskResult *v1.TaskResult,
) (bool, error) {
	switch msg := taskResult.Message.(type) {
	case *v1.TaskResult_HttpResponse:
		resp := msg.HttpResponse
		header := http.Header{}
		for k, vs := range resp.Header {
			for _, v := range vs.Values {
				header.Add(k, v)
			}
		}

		p := newPipeReadWriteCloser()
		t.bodyWriter = p

		t.RespCh <- &http.Response{
			StatusCode: int(resp.StatusCode),
			Status:     resp.Status,
			Header:     header,
			Body:       p,
		}
		close(t.RespCh)

		if d := resp.Body; len(d) > 0 {
			if _, err := p.Write(d); err != nil {
				// Gracefully handle the error as it can happen when the request is canceled and
				// the body writer is closed by the client.
				log.Printf("Failed to write the body writer: %s\n", err)
			}
		}

		isErr := resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest
		return isErr || (!t.Req.Stream), nil
	case *v1.TaskResult_ServerSentEvent:
		if !t.Req.Stream {
			return false, fmt.Errorf("unexpected chunked response for non-streaming request")
		}

		if d := msg.ServerSentEvent.Data; len(d) > 0 {
			if _, err := t.bodyWriter.Write(d); err != nil {
				// Gracefully handle the error as it can happen when the request is canceled and
				// the body writer is closed by the client.
				log.Printf("Failed to write the body writer: %s\n", err)
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
			ModelID: task.Req.Model,
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
