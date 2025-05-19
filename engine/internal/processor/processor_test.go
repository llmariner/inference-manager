package processor

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	v1 "github.com/llmariner/inference-manager/api/v1"
	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/llmariner/inference-manager/engine/internal/metrics"
	"github.com/llmariner/inference-manager/engine/internal/runtime"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestP(t *testing.T) {
	// Start a fake ollama server.
	ollamaSrv, err := newFakeOllamaServer()
	assert.NoError(t, err)

	go ollamaSrv.serve()
	defer ollamaSrv.shutdown(context.Background())

	assert.Eventuallyf(t, ollamaSrv.isReady, 10*time.Second, 100*time.Millisecond, "engine server is not ready")

	ctx := testutil.ContextWithLogger(t)
	logger := ctrl.LoggerFrom(ctx)

	processor := NewP(
		"engine_id0",
		nil,
		newFixedAddressGetter(fmt.Sprintf("localhost:%d", ollamaSrv.port())),
		newFakeModelSyncer(),
		logger,
		&metrics.NoopCollector{},
		time.Second,
	)

	fakeSender := &fakeSender{ctx: ctx}

	task := &v1.Task{
		Request: &v1.TaskRequest{
			Request: &v1.TaskRequest_ChatCompletion{
				ChatCompletion: &v1.CreateChatCompletionRequest{
					Model: "m0",
				},
			},
		},
	}

	err = processor.processTask(ctx, fakeSender, task, nil)
	assert.NoError(t, err)
	resp := fakeSender.gotReq.GetTaskResult().GetHttpResponse()
	assert.Equal(t, http.StatusOK, int(resp.StatusCode))
	assert.Equal(t, "ok", string(resp.Body))
}

func TestEmbedding(t *testing.T) {
	// Start a fake ollama server.
	ollamaSrv, err := newFakeOllamaServer()
	assert.NoError(t, err)

	go ollamaSrv.serve()
	defer ollamaSrv.shutdown(context.Background())

	assert.Eventuallyf(t, ollamaSrv.isReady, 10*time.Second, 100*time.Millisecond, "engine server is not ready")

	ctx := testutil.ContextWithLogger(t)
	logger := ctrl.LoggerFrom(ctx)

	processor := NewP(
		"engine_id0",
		nil,
		newFixedAddressGetter(fmt.Sprintf("localhost:%d", ollamaSrv.port())),
		newFakeModelSyncer(),
		logger,
		&metrics.NoopCollector{},
		time.Second,
	)

	fakeSender := &fakeSender{ctx: ctx}

	task := &v1.Task{
		Request: &v1.TaskRequest{
			Request: &v1.TaskRequest_Embedding{
				Embedding: &v1.CreateEmbeddingRequest{
					Model: "m0",
				},
			},
		},
	}

	err = processor.processTask(ctx, fakeSender, task, nil)
	assert.NoError(t, err)
	resp := fakeSender.gotReq.GetTaskResult().GetHttpResponse()
	assert.Equal(t, http.StatusOK, int(resp.StatusCode))
	assert.Equal(t, "ok", string(resp.Body))
}

func TestGoAwayTask(t *testing.T) {
	// Start a fake ollama server.
	ollamaSrv, err := newFakeOllamaServer()
	assert.NoError(t, err)

	go ollamaSrv.serve()
	defer ollamaSrv.shutdown(context.Background())

	assert.Eventuallyf(t, ollamaSrv.isReady, 10*time.Second, 100*time.Millisecond, "engine server is not ready")

	ctx := testutil.ContextWithLogger(t)
	logger := ctrl.LoggerFrom(ctx)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	fakeProcessTaskClient := &fakeProcessTaskClient{
		stream: &fakeStream{
			ctx: ctx,
			resp: &v1.ProcessTasksResponse{
				NewTask: &v1.Task{
					Request: &v1.TaskRequest{
						Request: &v1.TaskRequest_GoAway{
							GoAway: &v1.GoAwayRequest{},
						},
					},
				},
			},
		},
	}
	processor := NewP(
		"engine_id0",
		&fakeProcessTaskClientFactory{
			client: fakeProcessTaskClient,
		},
		newFixedAddressGetter(fmt.Sprintf("localhost:%d", ollamaSrv.port())),
		newFakeModelSyncer(),
		logger,
		&metrics.NoopCollector{},
		time.Second,
	)

	err = processor.run(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, errGoAway)
}

func newFakeOllamaServer() (*fakeOllamaServer, error) {
	m := http.NewServeMux()

	f := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("ok"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	m.Handle(completionPath, http.HandlerFunc(f))
	m.Handle(embeddingPath, http.HandlerFunc(f))

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}

	return &fakeOllamaServer{
		srv: &http.Server{
			Handler: m,
		},
		listener: listener,
	}, nil
}

type fakeOllamaServer struct {
	srv      *http.Server
	listener net.Listener
}

func (s *fakeOllamaServer) serve() {
	_ = s.srv.Serve(s.listener)
}

func (s *fakeOllamaServer) shutdown(ctx context.Context) {
	_ = s.srv.Shutdown(ctx)
}

func (s *fakeOllamaServer) isReady() bool {
	baseURL := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", s.port()),
	}
	requestURL := baseURL.JoinPath(completionPath).String()
	freq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, requestURL, bytes.NewReader(nil))
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(freq)
	if err != nil {
		return false
	}

	return resp.StatusCode == http.StatusOK
}

func (s *fakeOllamaServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func newFakeModelSyncer() *fakeModelSyncer {
	return &fakeModelSyncer{
		pulledModels:  map[string]bool{},
		deletedModels: map[string]bool{},
	}
}

type fakeModelSyncer struct {
	pulledModels  map[string]bool
	deletedModels map[string]bool
}

func (f *fakeModelSyncer) ListSyncedModels() []runtime.ModelRuntimeInfo {
	return nil
}

func (f *fakeModelSyncer) PullModel(ctx context.Context, modelID string) error {
	f.pulledModels[modelID] = true
	return nil
}

func (f *fakeModelSyncer) ListInProgressModels() []runtime.ModelRuntimeInfo {
	return nil
}

func (f *fakeModelSyncer) DeleteModel(ctx context.Context, modelID string) error {
	f.deletedModels[modelID] = true
	return nil
}

type fakeProcessTaskClientFactory struct {
	client *fakeProcessTaskClient
}

func (f *fakeProcessTaskClientFactory) Create() (ProcessTasksClient, func(), error) {
	return f.client, func() {}, nil
}

type fakeProcessTaskClient struct {
	stream *fakeStream
}

func (c *fakeProcessTaskClient) ProcessTasks(ctx context.Context, opts ...grpc.CallOption) (v1.InferenceWorkerService_ProcessTasksClient, error) {
	return c.stream, nil
}

type fakeStream struct {
	ctx  context.Context
	resp *v1.ProcessTasksResponse
}

func (s *fakeStream) Context() context.Context {
	return s.ctx
}

func (s *fakeStream) Recv() (*v1.ProcessTasksResponse, error) {
	if s.resp == nil {
		// Block forever.
		<-s.ctx.Done()
		return nil, s.ctx.Err()
	}

	resp := s.resp
	s.resp = nil
	return resp, nil
}

func (s *fakeStream) CloseSend() error {
	return nil
}

func (s *fakeStream) Header() (metadata.MD, error) {
	return nil, nil
}

func (s *fakeStream) RecvMsg(any) error {
	return nil
}

func (s *fakeStream) SendMsg(any) error {
	return nil
}

func (s *fakeStream) Trailer() metadata.MD {
	return nil
}

func (s *fakeStream) Send(req *v1.ProcessTasksRequest) error {
	return nil
}

type fakeSender struct {
	ctx    context.Context
	gotReq *v1.ProcessTasksRequest
}

func (c *fakeSender) Context() context.Context {
	return c.ctx
}

func (c *fakeSender) Send(req *v1.ProcessTasksRequest) error {
	c.gotReq = req
	return nil
}

// newFixedAddressGetter returns a new fixedAddressGetter.
func newFixedAddressGetter(addr string) *fixedAddressGetter {
	return &fixedAddressGetter{addr: addr}
}

// fixedAddressGetter is a fixed address getter.
type fixedAddressGetter struct {
	addr string
}

// GetLLMAddress returns a fixed address.
func (g *fixedAddressGetter) GetLLMAddress(modelID string) (string, error) {
	return g.addr, nil
}

func (g *fixedAddressGetter) BlacklistLLMAddress(modelID, address string) error {
	return nil
}
