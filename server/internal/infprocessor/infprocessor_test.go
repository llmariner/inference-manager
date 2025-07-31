package infprocessor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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
		router.New(true),
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

	req := &v1.CreateChatCompletionRequest{Model: modelID}
	resp, err := iprocessor.SendChatCompletionTask(ctx, "tenant0", req, nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, "ok", string(body))

	assert.Empty(t, iprocessor.inProgressTasksByID)

	engines := iprocessor.LocalEngines()
	assert.Len(t, engines, 1)
	assert.Len(t, engines["tenant0"], 1)

	// Remove the engine. Check if a newly created task will fail.
	iprocessor.RemoveEngine("engine_id0", "tenant0")
	assert.Empty(t, iprocessor.engines["tenant0"])
	_, err = iprocessor.SendChatCompletionTask(ctx, "tenant0", req, nil)
	assert.Error(t, err)
	assert.Truef(t, strings.Contains(err.Error(), "no engine available"), "got %s", err)
}

func TestEmbedding(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		router.New(true),
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

	assert.Empty(t, iprocessor.inProgressTasksByID)
}

func TestRemoveEngineWithInProgressTask(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		router.New(true),
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

	assert.Empty(t, iprocessor.inProgressTasksByID)
}

func TestProcessTaskTimeout(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		router.New(true),
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

	go func(ctx context.Context) {
		_ = iprocessor.Run(ctx)
	}(ctx)

	ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	req := &v1.CreateChatCompletionRequest{Model: modelID}
	_, err := iprocessor.SendChatCompletionTask(ctx, "tenant0", req, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	assert.Empty(t, iprocessor.inProgressTasksByID)
}

func TestProcessTaskResultAfterContextCancel(t *testing.T) {
	const (
		modelID = "m0"
	)

	iprocessor := NewP(
		router.New(true),
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

	assert.Empty(t, iprocessor.inProgressTasksByID)

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
		router.New(true),
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
	done := make(chan struct{})
	go func() {
		f := func(r *v1.TaskResult) error {
			resultCh <- r
			return nil
		}
		_ = iprocessor.SendAndProcessTask(ctx, task, "tenant0", f)
		close(done)
	}()

	resp := <-resultCh
	assert.Equal(t, http.StatusOK, int(resp.GetHttpResponse().StatusCode))

	// Wait for the task completion.
	<-done

	assert.Empty(t, iprocessor.inProgressTasksByID)
}

func TestFindMostPreferredtEngine(t *testing.T) {
	p := NewP(
		router.New(true),
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
		router.New(true),
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

func TestLocalEngines(t *testing.T) {
	p := NewP(
		router.New(true),
		testutil.NewTestLogger(t),
	)

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

	got := p.LocalEngines()
	assert.Len(t, got, 1)
	assert.Equal(t, "e0", got["tenant0"][0].EngineId)
}

func TestDumpTenantStatus(t *testing.T) {
	iprocessor := &P{
		engines: map[string]map[string]*engine{
			"tenant0": {
				"e0": {
					models: []*v1.EngineStatus_Model{
						{
							Id: "m1",
						},
					},
				},
				"e1": {
					models: []*v1.EngineStatus_Model{
						{
							Id: "m2",
						},
						{
							Id: "m3",
						},
					},
				},
			},
		},
		inProgressTasksByID: map[string]*task{
			"task0": {
				id:       "task0",
				engineID: "e0",
				tenantID: "tenant0",
				request: &v1.TaskRequest{
					Request: &v1.TaskRequest_ChatCompletion{
						ChatCompletion: &v1.CreateChatCompletionRequest{
							Model: "m0",
						},
					},
				},
			},
			"task1": {
				id:       "task1",
				engineID: "e0",
				tenantID: "tenant0",
				request: &v1.TaskRequest{
					Request: &v1.TaskRequest_ChatCompletion{
						ChatCompletion: &v1.CreateChatCompletionRequest{
							Model: "m1",
						},
					},
				},
			},
			"task2": {
				id:       "task2",
				engineID: "e1",
				tenantID: "tenant0",
				request: &v1.TaskRequest{
					Request: &v1.TaskRequest_ChatCompletion{
						ChatCompletion: &v1.CreateChatCompletionRequest{
							Model: "m2",
						},
					},
				},
			},
		},
	}

	tcs := []struct {
		tenantID string
		want     *TenantStatus
	}{
		{
			tenantID: "tenant0",
			want: &TenantStatus{
				Engines: map[string]*EngineStatus{
					"e0": {
						Models: []*v1.EngineStatus_Model{
							{
								Id: "m1",
							},
						},
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
						Models: []*v1.EngineStatus_Model{
							{
								Id: "m2",
							},
							{
								Id: "m3",
							},
						},
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
		{
			tenantID: "tenant1",
			want: &TenantStatus{
				Engines: map[string]*EngineStatus{},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.tenantID, func(t *testing.T) {
			got := iprocessor.DumpTenantStatus(tc.tenantID)
			assert.Len(t, got.Engines, len(tc.want.Engines))
			for engineID, ge := range got.Engines {
				we, ok := tc.want.Engines[engineID]
				assert.True(t, ok)

				assert.Len(t, ge.Models, len(we.Models))
				for i, gm := range ge.Models {
					wm := we.Models[i]
					assert.Equal(t, wm.Id, gm.Id)
				}
				assert.Len(t, ge.Tasks, len(we.Tasks))
				for i, gt := range ge.Tasks {
					wt := we.Tasks[i]
					assert.Equal(t, wt.ID, gt.ID)
					assert.Equal(t, wt.ModelID, gt.ModelID)
				}
			}
		})
	}
}

func TestDumpStatus(t *testing.T) {
	iprocessor := &P{
		engines: map[string]map[string]*engine{
			"tenant0": {
				"e0": {
					models: []*v1.EngineStatus_Model{
						{
							Id: "m0",
						},
						{
							Id: "m1",
						},
					},
				},
				"e1": {
					models: []*v1.EngineStatus_Model{
						{
							Id: "m2",
						},
						{
							Id: "m3",
						},
					},
				},
			},
		},
		inProgressTasksByID: map[string]*task{
			"task0": {
				id:       "task0",
				engineID: "e0",
				tenantID: "tenant0",
				request: &v1.TaskRequest{
					Request: &v1.TaskRequest_ChatCompletion{
						ChatCompletion: &v1.CreateChatCompletionRequest{
							Model: "m0",
						},
					},
				},
			},
			"task1": {
				id:       "task1",
				engineID: "e0",
				tenantID: "tenant0",
				request: &v1.TaskRequest{
					Request: &v1.TaskRequest_ChatCompletion{
						ChatCompletion: &v1.CreateChatCompletionRequest{
							Model: "m1",
						},
					},
				},
			},
			"task2": {
				id:       "task2",
				engineID: "e1",
				tenantID: "tenant0",
				request: &v1.TaskRequest{
					Request: &v1.TaskRequest_ChatCompletion{
						ChatCompletion: &v1.CreateChatCompletionRequest{
							Model: "m2",
						},
					},
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
						Models: []*v1.EngineStatus_Model{
							{
								Id: "m0",
							},
							{
								Id: "m1",
							},
						},
						//RegisteredModelIDs: []string{"m0", "m1"},
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
						Models: []*v1.EngineStatus_Model{
							{
								Id: "m2",
							},
							{
								Id: "m3",
							},
						},
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

	for tenant, gt := range got.Tenants {
		wt, ok := want.Tenants[tenant]
		assert.True(t, ok)
		assert.Len(t, gt.Engines, len(wt.Engines))
		for engineID, ge := range gt.Engines {
			we, ok := wt.Engines[engineID]
			assert.True(t, ok)

			assert.Len(t, ge.Models, len(we.Models))
			for i, gm := range ge.Models {
				wm := we.Models[i]
				assert.Equal(t, wm.Id, gm.Id)
			}
			assert.Len(t, ge.Tasks, len(we.Tasks))
			for i, gt := range ge.Tasks {
				wt := we.Tasks[i]
				assert.Equal(t, wt.ID, gt.ID)
				assert.Equal(t, wt.ModelID, gt.ModelID)
			}
		}
	}
}

func TestAddOrUpdateEngineStatus(t *testing.T) {
	iprocessor := NewP(
		router.New(true),
		testutil.NewTestLogger(t),
	)

	iprocessor.AddOrUpdateEngineStatus(
		newFakeEngineCommunicator(t),
		&v1.EngineStatus{
			EngineId: "engine_id0",
			Models: []*v1.EngineStatus_Model{
				{
					Id:      "m0",
					IsReady: true,
				},
				{
					Id:      "m1",
					IsReady: true,
				},
			},
			Ready: true,
		},
		"tenant0",
		true,
	)

	assertModelIDs := func(t *testing.T, wantIDs []string, es *engine) {
		var gotIDs []string
		for _, m := range es.models {
			gotIDs = append(gotIDs, m.Id)
		}
		assert.ElementsMatch(t, wantIDs, gotIDs)
	}

	assert.Equal(t, 1, len(iprocessor.engines))
	ess := iprocessor.engines["tenant0"]
	assert.Equal(t, 1, len(ess))
	es := ess["engine_id0"]
	assertModelIDs(t, []string{"m0", "m1"}, es)
	assert.True(t, es.isLocal)

	// Add new engine.
	iprocessor.AddOrUpdateEngineStatus(
		newFakeEngineCommunicator(t),
		&v1.EngineStatus{
			EngineId: "engine_id1",
			Models: []*v1.EngineStatus_Model{
				{
					Id:      "m2",
					IsReady: true,
				},
			},
			Ready: true,
		},
		"tenant0",
		false,
	)

	assert.Equal(t, 1, len(iprocessor.engines))
	ess = iprocessor.engines["tenant0"]
	assert.Equal(t, 2, len(ess))
	es = ess["engine_id1"]
	assertModelIDs(t, []string{"m2"}, es)
	assert.False(t, es.isLocal)

	// Update the engine.
	iprocessor.AddOrUpdateEngineStatus(
		newFakeEngineCommunicator(t),
		&v1.EngineStatus{
			EngineId: "engine_id1",
			Models: []*v1.EngineStatus_Model{
				{
					Id:      "m3",
					IsReady: true,
				},
			},
			Ready: true,
		},
		"tenant0",
		true,
	)
	assert.Equal(t, 1, len(iprocessor.engines))
	ess = iprocessor.engines["tenant0"]
	assert.Equal(t, 2, len(ess))
	es = ess["engine_id1"]
	assertModelIDs(t, []string{"m3"}, es)
	assert.True(t, es.isLocal)

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
