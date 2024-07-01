package infprocessor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/common/pkg/models"
)

const (
	taskQueueSize  = 100
	completionPath = "/v1/chat/completions"
)

// Task is an inference task.
// TODO(kenji): Consider preserving the request context as well.
type Task struct {
	ID     string
	Req    *v1.CreateChatCompletionRequest
	Header http.Header
	RespCh chan *http.Response
	ErrCh  chan error
}

// WaitForCompletion waits for the completion of the task.
func (t *Task) WaitForCompletion() (*http.Response, error) {
	select {
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
		queue:        queue,
		engineGetter: engineGetter,
		enginesByID:  map[string]*engine{},
	}
}

type engine struct {
	srv v1.InferenceWorkerService_ProcessTasksServer
}

// P processes inference tasks.
type P struct {
	queue *TaskQueue

	engineGetter engineGetter

	enginesByID map[string]*engine
	mu          sync.Mutex
}

// Run runs the processor.
func (p *P) Run(ctx context.Context) error {
	for {
		t, err := p.queue.Dequeue(ctx)
		if err != nil {
			return err
		}
		p.runTask(ctx, t)
	}
}

func (p *P) runTask(ctx context.Context, t *Task) {
	dest, err := p.engineGetter.GetEngineForModel(ctx, t.Req.Model)
	if err != nil {
		t.ErrCh <- fmt.Errorf("find pod to route the request: %s", err)
		return
	}

	log.Printf("Forwarding completion request to Inference Manager Engine (IP: %s)\n", dest)

	// Convert to the Ollama model name and marshal the request.
	t.Req.Model = models.OllamaModelName(t.Req.Model)
	reqBody, err := json.Marshal(t.Req)
	if err != nil {
		t.ErrCh <- err
		return
	}

	baseURL := &url.URL{
		Scheme: "http",
		Host:   dest,
	}
	requestURL := baseURL.JoinPath(completionPath).String()
	freq, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(reqBody))
	if err != nil {
		t.ErrCh <- err
		return
	}
	// Copy headers.
	for k, v := range t.Header {
		for _, vv := range v {
			freq.Header.Add(k, vv)
		}
	}

	resp, err := http.DefaultClient.Do(freq)
	if err != nil {
		t.ErrCh <- err
		return
	}
	t.RespCh <- resp
}

// AddOrUpdateEngineStatus adds or updates the engine status.
func (p *P) AddOrUpdateEngineStatus(
	srv v1.InferenceWorkerService_ProcessTasksServer,
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
