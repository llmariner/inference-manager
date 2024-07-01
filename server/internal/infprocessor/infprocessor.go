package infprocessor

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	v1 "github.com/llm-operator/inference-manager/api/v1"
)

const (
	taskQueueSize = 100
)

// Task is an inference task.
// TODO(kenji): Consider preserving the request context as well.
type Task struct {
	ID     string
	Req    *v1.CreateChatCompletionRequest
	Header http.Header
	RespCh chan *http.Response
	ErrCh  chan error

	bodyWriter pipeReadWriteCloser
}

// WaitForCompletion waits for the completion of the task.
func (t *Task) WaitForCompletion(ctx context.Context) (*http.Response, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-t.RespCh:
		return resp, nil
	case err := <-t.ErrCh:
		return nil, err
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
	tasks chan *Task
}

// Enqueue inserts a task into the queue.
func (q *TaskQueue) Enqueue(t *Task) {
	q.tasks <- t
}

// Dequeue removes a task from the queue.
func (q *TaskQueue) Dequeue(ctx context.Context) (*Task, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case t := <-q.tasks:
		return t, nil
	}
}

type engineGetter interface {
	GetEngineForModel(ctx context.Context, modelID string) (string, error)
}

// NewP creates a new processor.
func NewP(queue *TaskQueue, engineGetter engineGetter) *P {
	return &P{
		queue:               queue,
		engineGetter:        engineGetter,
		enginesByID:         map[string]*engine{},
		inProgressTasksByID: map[string]*Task{},
	}
}

type engineCommunicator interface {
	Send(*v1.ProcessTasksResponse) error
	Recv() (*v1.ProcessTasksRequest, error)
}

type engine struct {
	srv engineCommunicator
}

// P processes inference tasks.
type P struct {
	queue *TaskQueue

	engineGetter engineGetter

	enginesByID         map[string]*engine
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
	if len(p.enginesByID) == 0 {
		t.ErrCh <- fmt.Errorf("no engine found")
		return
	}

	dest, err := p.engineGetter.GetEngineForModel(ctx, t.Req.Model)
	if err != nil {
		t.ErrCh <- fmt.Errorf("find pod to route the request: %s", err)
		return
	}

	log.Printf("Forwarding completion request to Inference Manager Engine (IP: %s)\n", dest)

	// TODO(kenji): Use the above routing algorithm properly.
	var engine *engine
	for _, e := range p.enginesByID {
		engine = e
		break
	}

	p.mu.Lock()
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

// AddOrUpdateEngineStatus adds or updates the engine status.
func (p *P) AddOrUpdateEngineStatus(
	srv engineCommunicator,
	engineStatus *v1.EngineStatus,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.enginesByID[engineStatus.EngineId]; !ok {
		log.Printf("Registering new engine: %s\n", engineStatus.EngineId)
		e := &engine{
			srv: srv,
		}
		p.enginesByID[engineStatus.EngineId] = e
	}

	// TODO(kenji): Update registered models.
}

// ProcessTaskResult processes the task result.
func (p *P) ProcessTaskResult(
	taskResult *v1.TaskResult,
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
				return false, fmt.Errorf("write the body writer: %s", err)
			}
		}

		return !t.Req.Stream, nil
	case *v1.TaskResult_ServerSentEvent:
		if !t.Req.Stream {
			return false, fmt.Errorf("unexpected chunked response for non-streaming request")
		}

		if d := msg.ServerSentEvent.Data; len(d) > 0 {
			if _, err := t.bodyWriter.Write(d); err != nil {
				return false, fmt.Errorf("write the body writer: %s", err)
			}
		}

		return msg.ServerSentEvent.IsLastEvent, nil
	default:
		return false, fmt.Errorf("unexpected message type: %T", msg)
	}
}
