package infprocessor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"golang.org/x/sync/errgroup"
)

const (
	maxRetryCount = 3
)

type engineRouter interface {
	AddOrUpdateEngine(engineID, tenantID string, modelIDs []string)
	DeleteEngine(engineID, tenantID string)
	GetEnginesForModel(ctx context.Context, modelID, tenantID string, ignores map[string]bool) ([]string, error)
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

	clusterID          string
	models             []*v1.EngineStatus_Model
	modelIDs           []string
	inProgressModelIDs []string

	// isLocal indicates whether the engine is connected to this local server or not.
	isLocal bool

	lastHeartbeat time.Time
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
	engineIDs, err := p.getEnginesForTask(ctx, t)
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
			Id:             t.id,
			Request:        t.request,
			Header:         header,
			EngineId:       engine.id,
			TimeoutSeconds: t.timeoutSeconds,
		},
	}); err != nil {
		return fmt.Errorf("send the task: %s", err)
	}
	return nil
}

func (p *P) getEnginesForTask(ctx context.Context, t *task) ([]string, error) {
	if eid := t.targetEngineID; eid != "" {
		return []string{eid}, nil
	}

	// TODO (aya): Rethink the routing logic. The engines in the same
	// cluster currently share the same model set. It would be better
	// to choose the cluster first and then randomly select an engine
	//
	// TODO(kenji): Change the routing logic for model deactivation tasks. The deactivation requests
	// should be sent to all clusters where the model exists.
	engineIDs, err := p.engineRouter.GetEnginesForModel(ctx, t.model(), t.tenantID, t.preferredIgnoredEngines)
	if err != nil {
		return nil, fmt.Errorf("find an engine to route the request: %s", err)
	}
	return engineIDs, nil
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

	task := newTaskWithID(
		ctx,
		origTask.Id,
		tenantID,
		origTask.Request,
		header,
		origTask.EngineId,
	)

	return p.enqueueAndProcessTask(ctx, task, processResult, log)
}

// SendChatCompletionTask sends a chat completion task.
func (p *P) SendChatCompletionTask(
	ctx context.Context,
	tenantID string,
	req *v1.CreateChatCompletionRequest,
	header http.Header,
) (*http.Response, error) {
	t, err := newTask(
		ctx,
		tenantID,
		&v1.TaskRequest{
			Request: &v1.TaskRequest_ChatCompletion{
				ChatCompletion: req,
			},
		},
		header,
		"",
	)
	if err != nil {
		return nil, err
	}

	return p.sendTask(ctx, t, p.logger.WithName("chat"))
}

// SendEmbeddingTask sends an embedding task.
func (p *P) SendEmbeddingTask(
	ctx context.Context,
	tenantID string,
	req *v1.CreateEmbeddingRequest,
	header http.Header,
) (*http.Response, error) {
	t, err := newTask(
		ctx,
		tenantID,
		&v1.TaskRequest{
			Request: &v1.TaskRequest_Embedding{
				Embedding: req,
			},
		},
		header,
		"",
	)
	if err != nil {
		return nil, err
	}

	return p.sendTask(ctx, t, p.logger.WithName("embedded"))
}

// SendGoAwayTaskToLocalEngines sends a go away task to local engines.
func (p *P) SendGoAwayTaskToLocalEngines(ctx context.Context) error {
	req := &v1.TaskRequest{
		Request: &v1.TaskRequest_GoAway{
			GoAway: &v1.GoAwayRequest{},
		},
	}

	noopCallback := func(engineID string) {}

	if err := p.sendTaskToEngines(ctx, req, "goAway", noopCallback, true, 0); err != nil {
		return fmt.Errorf("send go away task to local engines: %s", err)
	}

	return nil
}

// SendHeartbeatTaskToEngines sends a heartbeat task to engines.
func (p *P) SendHeartbeatTaskToEngines(ctx context.Context, timeout time.Duration) error {
	req := &v1.TaskRequest{
		Request: &v1.TaskRequest_Heartbeat{
			Heartbeat: &v1.HeartbeatRequest{},
		},
	}

	callback := func(engineID string) {
		if err := p.updateLastEngineHeartbeat(engineID, time.Now()); err != nil {
			p.logger.Error(err, "Failed to update the last heartbeat time")
		}
	}

	if err := p.sendTaskToEngines(ctx, req, "heartbeat", callback, false, timeout); err != nil {
		return fmt.Errorf("send heartbeat task to engines: %s", err)
	}

	return nil
}

func (p *P) sendTaskToEngines(
	ctx context.Context,
	req *v1.TaskRequest,
	taskName string,
	callback func(engineID string),
	localOnly bool,
	timeout time.Duration,
) error {
	p.mu.Lock()
	engineIDsByTenant := map[string][]string{}
	for tenantID, es := range p.engines {
		for _, e := range es {
			if localOnly && !e.isLocal {
				continue
			}
			engineIDsByTenant[tenantID] = append(engineIDsByTenant[tenantID], e.id)
		}
	}
	p.mu.Unlock()

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	errGroup, ctx := errgroup.WithContext(ctx)
	for tenantID, engineIDs := range engineIDsByTenant {
		tid := tenantID
		for _, engineID := range engineIDs {
			eid := engineID
			errGroup.Go(func() error {
				p.logger.Info("Sending task to engine", "taskName", taskName, "engineID", eid, "tenantID", tid)

				t, err := newTask(
					ctx,
					tenantID,
					req,
					http.Header{},
					eid,
				)
				if err != nil {
					return err
				}

				if _, err := p.sendTask(ctx, t, p.logger.WithName(taskName)); err != nil {
					return fmt.Errorf("send task: %s", err)
				}

				callback(eid)

				return nil
			})
		}
	}

	if err := errGroup.Wait(); err != nil {
		return fmt.Errorf("send task %q: %s", taskName, err)
	}

	return nil
}

func (p *P) sendTask(
	ctx context.Context,
	t *task,
	logger logr.Logger,
) (*http.Response, error) {
	log := logger.WithValues("id", t.id)
	log.V(1).Info("Waiting for an initial response to the task")

	respCh := make(chan *http.Response)
	// Keep a buffer an error might happen after the message is respCh is written
	// and this function ends.
	errCh := make(chan error, 1)

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
		if err := p.enqueueAndProcessTask(ctx, t, f, log); err != nil {
			errCh <- err
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
			log.Error(ctx.Err(), "Task completed due to context cancel")
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
					log.Error(err, "Failed to process the task")
					return err
				}
				task.retryCount++

				// add the engine to the preferred ignored engines list to avoid
				// scheduling the task to the same engine again.
				if e := task.engineID; e != "" {
					if task.preferredIgnoredEngines == nil {
						task.preferredIgnoredEngines = map[string]bool{}
					}
					task.preferredIgnoredEngines[e] = true
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

	return t.retryCount < maxRetryCount && p.taskTimeout > 0 && time.Since(t.createdAt.Add(p.retryDelay)) < p.taskTimeout
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
			clusterID:  engineStatus.ClusterId,
		}
		engines[engineStatus.EngineId] = e
		log.Info("Registered new engine", "isLocal", isLocal)
	}
	e.modelIDs = engineStatus.ModelIds
	e.models = engineStatus.Models
	// Check if the sync status is set for backward compatibility.
	if s := engineStatus.SyncStatus; s != nil {
		e.inProgressModelIDs = s.InProgressModelIds
	}
	e.taskSender = taskSender
	e.isLocal = isLocal
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
		p.logger.Info("No task found for the result", "taskID", taskID)
		return
	}

	// Write the result to the task's result channel. if the buffer is full, discard the result.
	// This is to avoid blocking the task processing.
	// TODO(kenji): Revisit. Discarindg results should be ok in most cases as there should be some issue
	// with a task processing side (e.g., client gets disconnected), but there might be some cases that the
	// buffer is just temporarily full.
	select {
	case t.resultCh <- r:
	default:
		p.logger.Error(fmt.Errorf("result buffer full"), "Discarded the result", "taskID", taskID)
	}
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
				Models:   e.models,
				SyncStatus: &v1.EngineStatus_SyncStatus{
					InProgressModelIds: e.inProgressModelIDs,
				},
				Ready:     true,
				ClusterId: e.clusterID,
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

// NumLocalEnginesByTenantID returns the number of localengines by tenant ID.
func (p *P) NumLocalEnginesByTenantID() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := make(map[string]int, len(p.engines))
	for tenantID, engines := range p.engines {
		var num int
		for _, e := range engines {
			if e.isLocal {
				num++
			}
		}
		m[tenantID] = num
	}
	return m
}

// updateLastEngineHeartbeat updates the last heartbeat time of an engine.
func (p *P) updateLastEngineHeartbeat(engineID string, t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var found bool
	for _, engines := range p.engines {
		e, ok := engines[engineID]
		if !ok {
			continue
		}

		e.lastHeartbeat = t
		found = true
		break
	}

	if !found {
		return fmt.Errorf("engine %s not found", engineID)
	}
	return nil
}

// LastEngineHeartbeats returns the last heartbeat time of each engine.
func (p *P) LastEngineHeartbeats() map[string]time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()

	m := make(map[string]time.Time, len(p.engines))
	for _, engines := range p.engines {
		for _, e := range engines {
			m[e.id] = e.lastHeartbeat
		}
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

	Models    []*v1.EngineStatus_Model `json:"models"`
	ClusterID string                   `json:"clusterId"`
}

func newEngineStatus(e *engine) *EngineStatus {
	return &EngineStatus{
		RegisteredModelIDs: e.modelIDs,
		InProgressModelIDs: e.inProgressModelIDs,
		Models:             e.models,
		IsLocal:            e.isLocal,
		ClusterID:          e.clusterID,
	}
}

// TenantStatus is the status of a tenant.
type TenantStatus struct {
	Engines map[string]*EngineStatus `json:"engines"`
}

// Status is the status of the processor.
type Status struct {
	Tenants map[string]*TenantStatus `json:"tenants"`
}

// DumpTenantStatus dumps the status of a tenant.
func (p *P) DumpTenantStatus(tenantID string) *TenantStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	t := &TenantStatus{
		Engines: map[string]*EngineStatus{},
	}
	for id, e := range p.engines[tenantID] {
		t.Engines[id] = newEngineStatus(e)
	}

	tasksByEnginesAndModelIDs := make(map[string]map[string]int32)
	for _, task := range p.inProgressTasksByID {
		if task.tenantID != tenantID {
			continue
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
		em, ok := tasksByEnginesAndModelIDs[task.engineID]
		if !ok {
			em = make(map[string]int32)
			tasksByEnginesAndModelIDs[task.engineID] = em
		}
		em[task.model()]++
	}

	// Sort the modelIDs and task IDs for deterministic output.
	for eid, e := range t.Engines {
		sort.Strings(e.RegisteredModelIDs)
		sort.Strings(e.InProgressModelIDs)
		sort.Slice(e.Models, func(i, j int) bool {
			return e.Models[i].Id < e.Models[j].Id
		})
		sort.Slice(e.Tasks, func(i, j int) bool {
			return e.Tasks[i].ID < e.Tasks[j].ID
		})
		for _, m := range e.Models {
			m.InProgressTaskCount = tasksByEnginesAndModelIDs[eid][m.Id]
		}
	}

	return t
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
			t.Engines[id] = newEngineStatus(e)
		}
	}

	tasksByEnginesAndModelIDs := make(map[string]map[string]int32)
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
		em, ok := tasksByEnginesAndModelIDs[task.engineID]
		if !ok {
			em = make(map[string]int32)
			tasksByEnginesAndModelIDs[task.engineID] = em
		}
		em[task.model()]++
	}

	// Sort the modelIDs and task IDs for deterministic output.
	for _, engines := range status.Tenants {
		for eid, e := range engines.Engines {
			sort.Strings(e.RegisteredModelIDs)
			sort.Strings(e.InProgressModelIDs)
			sort.Slice(e.Models, func(i, j int) bool {
				return e.Models[i].Id < e.Models[j].Id
			})
			sort.Slice(e.Tasks, func(i, j int) bool {
				return e.Tasks[i].ID < e.Tasks[j].ID
			})
			for _, m := range e.Models {
				m.InProgressTaskCount = tasksByEnginesAndModelIDs[eid][m.Id]
			}
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
		if resp.StatusCode == http.StatusInternalServerError {
			return fmt.Errorf("%s: %s", resp.Status, resp.Body)
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
