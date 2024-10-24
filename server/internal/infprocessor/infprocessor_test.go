package infprocessor

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"testing"

	v1 "github.com/llmariner/inference-manager/api/v1"
	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/llmariner/inference-manager/server/internal/router"
	"github.com/stretchr/testify/assert"
)

func TestP(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		router.New(),
		true,
		testutil.NewTestLogger(t),
	)
	iprocessor.taskTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := newFakeEngineCommunicator(t)
	go comm.run(ctx)

	iprocessor.AddOrUpdateEngineStatus(
		comm,
		&v1.EngineStatus{
			EngineId: "engine_id0",
			ModelIds: []string{modelID},
			Ready:    true,
		},
		"tenant0",
		true,
	)

	go func() {
		_ = iprocessor.Run(ctx)
	}()

	go func() {
		resp, err := comm.Recv()
		assert.NoError(t, err)
		iprocessor.ProcessTaskResult(resp.GetTaskResult())
	}()

	req := &v1.CreateChatCompletionRequest{Model: modelID}
	resp, err := iprocessor.SendChatCompletionTask(ctx, "tenant0", req, nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, "ok", string(body))

	engines := iprocessor.Engines()
	assert.Len(t, engines, 1)
	assert.Len(t, engines["tenant0"], 1)

	// Remove the engine. Check if a newly created task will fail.
	iprocessor.RemoveEngine("engine_id0", "tenant0")
	_, err = iprocessor.SendChatCompletionTask(ctx, "tenant0", req, nil)
	assert.Error(t, err)
}

func TestEmbedding(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		router.New(),
		true,
		testutil.NewTestLogger(t),
	)
	iprocessor.taskTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := newFakeEngineCommunicator(t)
	go comm.run(ctx)

	iprocessor.AddOrUpdateEngineStatus(
		comm,
		&v1.EngineStatus{
			EngineId: "engine_id0",
			Ready:    true,
		},
		"tenant0",
		true,
	)

	go func() {
		_ = iprocessor.Run(ctx)
	}()

	go func() {
		resp, err := comm.Recv()
		assert.NoError(t, err)
		iprocessor.ProcessTaskResult(resp.GetTaskResult())
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
		router.New(),
		true,
		testutil.NewTestLogger(t),
	)
	iprocessor.taskTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := newFakeEngineCommunicator(t)
	go comm.run(ctx)

	iprocessor.AddOrUpdateEngineStatus(
		comm,
		&v1.EngineStatus{
			EngineId: "engine_id0",
			Ready:    true,
		},
		"tenant0",
		true,
	)

	go func() {
		_ = iprocessor.Run(ctx)
	}()

	go func() {
		// Wait for the task to be scheduled.
		_, err := comm.Recv()
		assert.NoError(t, err)
		iprocessor.RemoveEngine("engine_id0", "tenant0")
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
		router.New(),
		true,
		testutil.NewTestLogger(t),
	)
	iprocessor.taskTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := newFakeEngineCommunicator(t)
	go comm.run(ctx)

	iprocessor.AddOrUpdateEngineStatus(
		comm,
		&v1.EngineStatus{
			EngineId: "engine_id0",
			Ready:    true,
		},
		"tenant0",
		true,
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
	iprocessor.ProcessTaskResult(resp.GetTaskResult())
	assert.NoError(t, err)
}

func TestSendAndProcessTask(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		router.New(),
		true,
		testutil.NewTestLogger(t),
	)
	iprocessor.taskTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comm := newFakeEngineCommunicator(t)
	go comm.run(ctx)

	iprocessor.AddOrUpdateEngineStatus(
		comm,
		&v1.EngineStatus{
			EngineId: "engine_id0",
			ModelIds: []string{modelID},
			Ready:    true,
		},
		"tenant0",
		true,
	)

	go func() {
		_ = iprocessor.Run(ctx)
	}()

	go func() {
		resp, err := comm.Recv()
		assert.NoError(t, err)
		iprocessor.ProcessTaskResult(resp.GetTaskResult())
		assert.NoError(t, err)
	}()

	task := &v1.Task{
		Id: "task0",
		Request: &v1.TaskRequest{
			Request: &v1.TaskRequest_ChatCompletion{
				ChatCompletion: &v1.CreateChatCompletionRequest{Model: modelID},
			},
		},
	}

	resultCh := make(chan *v1.TaskResult)
	go func() {
		f := func(r *v1.TaskResult) error {
			resultCh <- r
			return nil
		}
		_ = iprocessor.SendAndProcessTask(ctx, task, "tenant0", f)
	}()

	resp := <-resultCh
	assert.Equal(t, http.StatusOK, int(resp.GetHttpResponse().StatusCode))
}

func TestFindMostPreferredtEngine(t *testing.T) {
	p := NewP(
		router.New(),
		true,
		testutil.NewTestLogger(t),
	)

	ts := []*task{
		{
			id:       "t0",
			engineID: "e0",
		},
		{
			id:       "t1",
			engineID: "e0",
		},
		{
			id:       "t2",
			engineID: "e1",
		},
	}
	for _, t := range ts {
		p.inProgressTasksByID[t.id] = t
	}

	p.engines = map[string]map[string]*engine{
		"tenant0": {
			"e0": {
				id:      "e0",
				isLocal: true,
			},
			"e1": {
				id:      "e1",
				isLocal: true,
			},
			"e2": {
				id:      "e2",
				isLocal: true,
			},
		},
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
		engine, err := p.findMostPreferredtEngine(tc.engineIDs, "tenant0")
		assert.NoError(t, err)
		assert.Equal(t, tc.want, engine.id)
	}
}

func TestFindMostPreferredtEngine_PreferLocal(t *testing.T) {
	p := NewP(
		router.New(),
		true,
		testutil.NewTestLogger(t),
	)

	ts := []*task{
		{
			id:       "t0",
			engineID: "e0",
		},
	}
	for _, t := range ts {
		p.inProgressTasksByID[t.id] = t
	}

	p.engines = map[string]map[string]*engine{
		"tenant0": {
			"e0": {
				id:      "e0",
				isLocal: true,
			},
			"e1": {
				id:      "e1",
				isLocal: false,
			},
		},
	}

	tcs := []struct {
		engineIDs []string
		want      string
	}{
		// "e0" has an in-progress task, but it should be preferred as it is a local engine.
		{
			engineIDs: []string{"e0", "e1"},
			want:      "e0",
		},
		{
			engineIDs: []string{"e1"},
			want:      "e1",
		},
	}
	for _, tc := range tcs {
		engine, err := p.findMostPreferredtEngine(tc.engineIDs, "tenant0")
		assert.NoError(t, err)
		assert.Equal(t, tc.want, engine.id)
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
				id:       "task0",
				engineID: "e0",
				tenantID: "tenant0",
				chatCompletionReq: &v1.CreateChatCompletionRequest{
					Model: "m0",
				},
			},
			"task1": {
				id:       "task1",
				engineID: "e0",
				tenantID: "tenant0",
				chatCompletionReq: &v1.CreateChatCompletionRequest{
					Model: "m1",
				},
			},
			"task2": {
				id:       "task2",
				engineID: "e1",
				tenantID: "tenant0",
				chatCompletionReq: &v1.CreateChatCompletionRequest{
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
