package infprocessor

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"testing"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	testutil "github.com/llm-operator/inference-manager/common/pkg/test"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/stretchr/testify/assert"
)

func TestP(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		&fakeEngineRouter{},
		testutil.NewTestLogger(t),
	)
	iprocessor.taskTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := newFakeEngineCommunicator(t)
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

	req := &v1.CreateChatCompletionRequest{Model: modelID}
	resp, err := iprocessor.SendChatCompletionTask(ctx, "tenant0", req, nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, "ok", string(body))

	// Remove the engine. Check if a newly created task will fail.
	iprocessor.RemoveEngine("engine_id0", clusterInfo)
	_, err = iprocessor.SendChatCompletionTask(ctx, "tenant0", req, nil)
	assert.Error(t, err)
}

func TestEmbedding(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		&fakeEngineRouter{},
		testutil.NewTestLogger(t),
	)
	iprocessor.taskTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := newFakeEngineCommunicator(t)
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

	req := &v1.CreateEmbeddingRequest{Model: modelID}
	resp, err := iprocessor.SendEmbeddingTask(ctx, "tenant0", req, nil)
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

	iprocessor := NewP(
		&fakeEngineRouter{},
		testutil.NewTestLogger(t),
	)
	iprocessor.taskTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := newFakeEngineCommunicator(t)
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
		// Wait for the task to be scheduled.
		_, err := comm.Recv()
		assert.NoError(t, err)
		iprocessor.RemoveEngine("engine_id0", clusterInfo)
	}()

	req := &v1.CreateEmbeddingRequest{Model: modelID}
	_, err := iprocessor.SendEmbeddingTask(ctx, "tenant0", req, nil)
	assert.Error(t, err)
}

func TestProcessTaskResultAfterContextCancel(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		&fakeEngineRouter{},
		testutil.NewTestLogger(t),
	)
	iprocessor.taskTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := newFakeEngineCommunicator(t)
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
	iprocessor.taskTimeout = 0

	go func(ctx context.Context) {
		_ = iprocessor.Run(ctx)
	}(ctx)

	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	req := &v1.CreateEmbeddingRequest{Model: modelID}
	_, err := iprocessor.SendEmbeddingTask(ctx, "tenant0", req, nil)
	assert.Error(t, err)

	// Simulate a case where the task result is received after the context is canceled.
	resp, err := comm.Recv()
	assert.NoError(t, err)
	err = iprocessor.ProcessTaskResult(resp.GetTaskResult(), clusterInfo)
	assert.NoError(t, err)
}

func TestFindLeastLoadedEngine(t *testing.T) {
	p := NewP(
		&fakeEngineRouter{},
		testutil.NewTestLogger(t),
	)

	ts := []*task{
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
		inProgressTasksByID: map[string]*task{
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

func newFakeEngineCommunicator(t *testing.T) *fakeEngineCommunicator {
	return &fakeEngineCommunicator{
		t:        t,
		taskCh:   make(chan *v1.Task),
		resultCh: make(chan *v1.TaskResult),
	}
}

type fakeEngineCommunicator struct {
	t        *testing.T
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
			c.t.Logf("Processing task: %s", t.Id)
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
