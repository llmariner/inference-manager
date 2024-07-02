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

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestP(t *testing.T) {
	// Start a fake ollama server.
	ollamaSrv, err := newFakeOllamaServer()
	assert.NoError(t, err)

	go ollamaSrv.serve()
	defer ollamaSrv.shutdown(context.Background())

	assert.Eventuallyf(t, ollamaSrv.isReady, 10*time.Second, 100*time.Millisecond, "engine server is not ready")

	processor := NewP(
		"engine_id0",
		nil,
		fmt.Sprintf("localhost:%d", ollamaSrv.port()),
		&fakeModelLister{},
	)

	fakeClient := &fakeProcessTasksClient{}

	task := &v1.Task{
		Request: &v1.CreateChatCompletionRequest{
			Model: "m0",
		},
	}

	err = processor.processTask(context.Background(), fakeClient, task)
	assert.NoError(t, err)
	resp := fakeClient.gotReq.GetTaskResult().GetHttpResponse()
	assert.Equal(t, http.StatusOK, int(resp.StatusCode))
	assert.Equal(t, "ok", string(resp.Body))
}

func newFakeOllamaServer() (*fakeOllamaServer, error) {
	m := http.NewServeMux()
	m.Handle(completionPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("ok"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))

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

type fakeModelLister struct {
}

func (f *fakeModelLister) ListSyncedModelIDs(ctx context.Context) []string {
	return nil
}

type fakeProcessTasksClient struct {
	gotReq *v1.ProcessTasksRequest
}

func (c *fakeProcessTasksClient) Send(req *v1.ProcessTasksRequest) error {
	c.gotReq = req
	return nil
}
