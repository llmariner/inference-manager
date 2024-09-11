package infprocessor

import (
	"context"
	"io"
	"log"
	"net/http"
	"reflect"
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
		ChatCompletionReq: &v1.CreateChatCompletionRequest{
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

	// Remove the engine. Check if a newly created task will fail.
	iprocessor.RemoveEngine("engine_id0", clusterInfo)

	task = &Task{
		ID:       "task1",
		TenantID: "tenant0",
		ChatCompletionReq: &v1.CreateChatCompletionRequest{
			Model: modelID,
		},
		RespCh: make(chan *http.Response),
		ErrCh:  make(chan error),
	}
	queue.Enqueue(task)
	_, err = task.WaitForCompletion(context.Background())
	assert.Error(t, err)
}

func TestEmbedding(t *testing.T) {
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
		EmbeddingReq: &v1.CreateEmbeddingRequest{
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
}

func TestRemoveEngineWithInProgressTask(t *testing.T) {
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

	task := &Task{
		ID:       "task0",
		TenantID: "tenant0",
		ChatCompletionReq: &v1.CreateChatCompletionRequest{
			Model: modelID,
		},
		RespCh: make(chan *http.Response),
		ErrCh:  make(chan error),
	}
	queue.Enqueue(task)

	// Wait for the task to be scheduled.
	_, err := comm.Recv()
	assert.NoError(t, err)

	iprocessor.RemoveEngine("engine_id0", clusterInfo)

	_, err = task.WaitForCompletion(context.Background())
	assert.Error(t, err)
}

func TestProcessTaskResultAfterContextCancel(t *testing.T) {
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

	go func(ctx context.Context) {
		_ = iprocessor.Run(ctx)
	}(ctx)

	task := &Task{
		ID:       "task0",
		TenantID: "tenant0",
		ChatCompletionReq: &v1.CreateChatCompletionRequest{
			Model: modelID,
		},
		RespCh: make(chan *http.Response),
		ErrCh:  make(chan error),
	}
	queue.Enqueue(task)

	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	_, err := task.WaitForCompletion(ctx)
	assert.Error(t, err)

	// Simulate a case where the task result is received after the context is canceled.
	resp, err := comm.Recv()
	assert.NoError(t, err)
	err = iprocessor.ProcessTaskResult(resp.GetTaskResult(), clusterInfo)
	assert.NoError(t, err)
}

func TestFindLeastLoadedEngine(t *testing.T) {
	p := NewP(
		NewTaskQueue(),
		&fakeEngineRouter{},
	)

	ts := []*Task{
		{
			ID:       "t0",
			EngineID: "e0",
		},
		{
			ID:       "t1",
			EngineID: "e0",
		},
		{
			ID:       "t2",
			EngineID: "e1",
		},
	}
	for _, t := range ts {
		p.inProgressTasksByID[t.ID] = t
	}

	tcs := []struct {
		engineIDs []string
		want      string
	}{
		{
			engineIDs: []string{"e0", "e1", "e2"},
			want:      "e2",
		},
		{
			engineIDs: []string{"e0", "e1"},
			want:      "e1",
		},
		{
			engineIDs: []string{"e0"},
			want:      "e0",
		},
	}
	for _, tc := range tcs {
		engineID := p.findLeastLoadedEngine(tc.engineIDs)
		assert.Equal(t, tc.want, engineID)
	}
}

func TestDumpStatus(t *testing.T) {
	iprocessor := &P{
		engines: map[string]map[string]*engine{
			"tenant0": {
				"e0": {
					modelIDs: []string{"m0", "m1"},
				},
				"e1": {
					modelIDs:           []string{"m2"},
					inProgressModelIDs: []string{"m3"},
				},
			},
		},
		inProgressTasksByID: map[string]*Task{
			"task0": {
				ID:       "task0",
				EngineID: "e0",
				TenantID: "tenant0",
				ChatCompletionReq: &v1.CreateChatCompletionRequest{
					Model: "m0",
				},
			},
			"task1": {
				ID:       "task1",
				EngineID: "e0",
				TenantID: "tenant0",
				ChatCompletionReq: &v1.CreateChatCompletionRequest{
					Model: "m1",
				},
			},
			"task2": {
				ID:       "task2",
				EngineID: "e1",
				TenantID: "tenant0",
				ChatCompletionReq: &v1.CreateChatCompletionRequest{
					Model: "m2",
				},
			},
		},
	}

	got := iprocessor.DumpStatus()
	want := &Status{
		Tenants: map[string]*TenantStatus{
			"tenant0": {
				Engines: map[string]*EngineStatus{
					"e0": {
						RegisteredModelIDs: []string{"m0", "m1"},
						Tasks: []*TaskStatus{
							{
								ID:      "task0",
								ModelID: "m0",
							},
							{
								ID:      "task1",
								ModelID: "m1",
							},
						},
					},
					"e1": {
						RegisteredModelIDs: []string{"m2"},
						InProgressModelIDs: []string{"m3"},
						Tasks: []*TaskStatus{
							{
								ID:      "task2",
								ModelID: "m2",
							},
						},
					},
				},
			},
		},
	}
	assert.True(t, reflect.DeepEqual(want, got), "want: %v, got: %v", want, got)
}

type fakeEngineRouter struct {
	engineID string
}

func (r *fakeEngineRouter) GetEnginesForModel(ctx context.Context, modelID, tenantID string) ([]string, error) {
	return []string{r.engineID}, nil
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
