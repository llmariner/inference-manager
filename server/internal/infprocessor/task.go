package infprocessor

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

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

	header http.Header

	request *v1.TaskRequest

	resultCh chan *resultOrError

	engineID string

	createdAt time.Time

	// preferedIgnoredEngines is a list of engine IDs that should be ignored
	// when scheduling the task only if the requested model is not loaded.
	preferredIgnoredEngines map[string]bool

	retryCount int
}

func (t *task) model() string {
	switch req := t.request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion:
		return req.GetChatCompletion().Model
	case *v1.TaskRequest_Embedding:
		return req.GetEmbedding().Model
	case *v1.TaskRequest_ModelActivation:
		return req.GetModelActivation().Id
	case *v1.TaskRequest_ModelDeactivation:
		return req.GetModelDeactivation().Id
	default:
		return ""
	}
}

func (t *task) stream() bool {
	switch req := t.request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion:
		return req.GetChatCompletion().Stream
	default:
		return false
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
