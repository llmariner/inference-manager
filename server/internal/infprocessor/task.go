package infprocessor

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/llmariner/common/pkg/id"
	v1 "github.com/llmariner/inference-manager/api/v1"
)

const (
	taskQueueSize = 100

	// taskResultBufferSize is the size of the task result buffer.
	// When the buffer is full, the task result will be dropped.
	taskResultBufferSize = 100
)

func newTaskID() (string, error) {
	taskID, err := id.GenerateID("inf_", 24)
	if err != nil {
		return "", fmt.Errorf("generate task ID: %s", err)
	}
	return taskID, nil
}

func newTask(
	ctx context.Context,
	tenantID string,
	request *v1.TaskRequest,
	header http.Header,
	targetEngineID string,
) (*task, error) {
	id, err := newTaskID()
	if err != nil {
		return nil, err
	}

	return newTaskWithID(
		ctx,
		id,
		tenantID,
		request,
		header,
		targetEngineID,
	), nil
}

func newTaskWithID(
	ctx context.Context,
	id string,
	tenantID string,
	request *v1.TaskRequest,
	header http.Header,
	targetEngineID string,
) *task {
	var timeoutSeconds int32
	deadline, ok := ctx.Deadline()
	if ok {
		// Set timeoutSeconds. Set to 1 if deadline is less than 1 second.
		timeoutSeconds = int32(time.Until(deadline).Seconds())
		if timeoutSeconds <= 0 {
			timeoutSeconds = 1
		}
	}

	return &task{
		id:             id,
		tenantID:       tenantID,
		request:        request,
		header:         header,
		targetEngineID: targetEngineID,
		timeoutSeconds: timeoutSeconds,
		createdAt:      time.Now(),
		// Use the buffered channel. Otherwise when processing a task result
		// gets stuck, the processor won't be able to process another task result
		// for the same task. As this can block ProcessTaskResult, we would
		// like to avoid that.
		resultCh: make(chan *resultOrError, taskResultBufferSize),
	}
}

type resultOrError struct {
	result *v1.TaskResult
	err    error
}

// task is an inference task.
// TODO(kenji): Consider preserving the request context as well.
type task struct {
	id       string
	tenantID string

	request *v1.TaskRequest

	header http.Header

	resultCh chan *resultOrError

	engineID string

	// targetEngineID is set if the task must be sent to this engine.
	targetEngineID string

	// timeoutSeconds is the timeout in seconds for the task (if non-zero).
	timeoutSeconds int32

	createdAt time.Time

	// preferredIgnoredEngines is a list of engine IDs that should be ignored
	// when scheduling the task only if the requested model is not loaded.
	preferredIgnoredEngines map[string]bool

	retryCount int

	nextResultIndex int
}

func (t *task) model() string {
	switch req := t.request; req.Request.(type) {
	case *v1.TaskRequest_ChatCompletion:
		return req.GetChatCompletion().Model
	case *v1.TaskRequest_Embedding:
		return req.GetEmbedding().Model
	case *v1.TaskRequest_AudioTranscription:
		return req.GetAudioTranscription().Model
	case *v1.TaskRequest_ModelResponse:
		return req.GetModelResponse().Model
	case *v1.TaskRequest_TokenizeRequest:
		return req.GetTokenizeRequest().Model
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
