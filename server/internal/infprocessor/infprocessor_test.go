package infprocessor

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestP(t *testing.T) {
	const (
		modelID = "m0"
	)

	engineSrv, err := newFakeEngineServer()
	assert.NoError(t, err)

	go engineSrv.serve()
	defer engineSrv.shutdown(context.Background())

	assert.Eventuallyf(t, engineSrv.isReady, 10*time.Second, 100*time.Millisecond, "engine server is not ready")

	queue := NewTaskQueue()

	iprocessor := NewP(
		queue,
		&fakeEngineGetter{
			addr: fmt.Sprintf("localhost:%d", engineSrv.port()),
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = iprocessor.Run(ctx)
	}()

	tcs := []struct {
		name string
		req  *v1.CreateChatCompletionRequest
		code int
	}{
		{
			name: "success",
			req: &v1.CreateChatCompletionRequest{
				Model: modelID,
			},
			code: http.StatusOK,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{
				Req:    tc.req,
				RespCh: make(chan *http.Response),
				ErrCh:  make(chan error),
			}
			queue.Enqueue(task)
			resp, err := task.WaitForCompletion(context.Background())
			assert.NoError(t, err)
			assert.Equal(t, tc.code, resp.StatusCode)
		})
	}
}

func newFakeEngineServer() (*fakeEngineServer, error) {
	m := http.NewServeMux()
	m.Handle(completionPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}

	return &fakeEngineServer{
		srv: &http.Server{
			Handler: m,
		},
		listener: listener,
	}, nil
}

type fakeEngineServer struct {
	srv      *http.Server
	listener net.Listener
}

func (s *fakeEngineServer) serve() {
	_ = s.srv.Serve(s.listener)
}

func (s *fakeEngineServer) shutdown(ctx context.Context) {
	_ = s.srv.Shutdown(ctx)
}

func (s *fakeEngineServer) isReady() bool {
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

func (s *fakeEngineServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

type fakeEngineGetter struct {
	addr string
}

func (g *fakeEngineGetter) GetEngineForModel(ctx context.Context, modelID string) (string, error) {
	return g.addr, nil
}
