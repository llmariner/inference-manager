package infprocessor

import (
	"context"
	"io"
	"log"
	"net/http"
	"testing"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/stretchr/testify/assert"
)

func TestP(t *testing.T) {
	const (
		modelID = "m0"
	)

	queue := NewTaskQueue()
	iprocessor := NewP(
		queue,
		&fakeEngineRouter{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := &fakeEngineCommunicator{
		taskCh:   make(chan *v1.Task),
		resultCh: make(chan *v1.TaskResult),
	}
	go comm.run(ctx)

	clusterInfo := &auth.ClusterInfo{
		TenantID: "tenant0",
	}

	iprocessor.AddOrUpdateEngineStatus(
		comm,
		&v1.EngineStatus{
			EngineId: "engine_id0",
		},
		clusterInfo,
	)

	go func() {
		_ = iprocessor.Run(ctx)
	}()

	go func() {
		resp, err := comm.Recv()
		assert.NoError(t, err)
		err = iprocessor.ProcessTaskResult(resp.GetTaskResult(), clusterInfo)
		assert.NoError(t, err)
	}()

	task := &Task{
		ID:       "task0",
		TenantID: "tenant0",
		Req: &v1.CreateChatCompletionRequest{
			Model: modelID,
		},
		RespCh: make(chan *http.Response),
		ErrCh:  make(chan error),
	}
	queue.Enqueue(task)

	resp, err := task.WaitForCompletion(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, "ok", string(body))

	// Remove the engien and check if the task fails.
	iprocessor.RemoveEngine("engine_id0", clusterInfo)

	task = &Task{
		ID:       "task1",
		TenantID: "tenant0",
		Req: &v1.CreateChatCompletionRequest{
			Model: modelID,
		},
		RespCh: make(chan *http.Response),
		ErrCh:  make(chan error),
	}
	queue.Enqueue(task)
	_, err = task.WaitForCompletion(context.Background())
	assert.Error(t, err)
}

type fakeEngineRouter struct {
	engineID string
}

func (r *fakeEngineRouter) GetEngineForModel(ctx context.Context, modelID, tenantID string) (string, error) {
	return r.engineID, nil
}

func (r *fakeEngineRouter) AddOrUpdateEngine(engineID, tenantID string, modelIDs []string) {
	r.engineID = engineID
}

func (r *fakeEngineRouter) DeleteEngine(engineID, tenantID string) {
	r.engineID = ""
}

type fakeEngineCommunicator struct {
	taskCh   chan *v1.Task
	resultCh chan *v1.TaskResult
}

func (c *fakeEngineCommunicator) Send(r *v1.ProcessTasksResponse) error {
	c.taskCh <- r.NewTask
	return nil
}

func (c *fakeEngineCommunicator) Recv() (*v1.ProcessTasksRequest, error) {
	r := <-c.resultCh
	return &v1.ProcessTasksRequest{
		Message: &v1.ProcessTasksRequest_TaskResult{
			TaskResult: r,
		},
	}, nil
}

func (c *fakeEngineCommunicator) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-c.taskCh:
			log.Printf("Processing task: %s\n", t.Id)
			c.resultCh <- &v1.TaskResult{
				TaskId: t.Id,
				Message: &v1.TaskResult_HttpResponse{
					HttpResponse: &v1.HttpResponse{
						StatusCode: http.StatusOK,
						Body:       []byte("ok"),
					},
				},
			}
		}
	}
}
