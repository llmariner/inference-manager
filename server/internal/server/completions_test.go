package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSplit(t *testing.T) {
	input := `line0

line1

line2

line3`
	scanner := bufio.NewScanner(bytes.NewReader([]byte(input)))
	scanner.Buffer(make([]byte, 4096), 4096)
	scanner.Split(split)
	var got []string
	for scanner.Scan() {
		got = append(got, scanner.Text())
	}
	err := scanner.Err()
	assert.NoError(t, err)

	want := []string{"line0", "line1", "line2", "line3"}
	assert.ElementsMatch(t, want, got)
}

func TestCreateChatCompletion(t *testing.T) {
	const (
		modelID = "m0"
	)

	engineSrv, err := newFakeEngineServer()
	assert.NoError(t, err)

	go engineSrv.serve()
	defer engineSrv.shutdown(context.Background())

	assert.Eventuallyf(t, engineSrv.isReady, 10*time.Second, 100*time.Millisecond, "engine server is not ready")

	srv := New(
		&fakeEngineGetter{
			addr: fmt.Sprintf("localhost:%d", engineSrv.port()),
		},
		&fakeModelClient{
			models: map[string]*mv1.Model{
				modelID: {},
			},
		},
	)

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
		{
			name: "no model",
			req: &v1.CreateChatCompletionRequest{
				Model: "m1",
			},
			code: http.StatusBadRequest,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {

			w := &httptest.ResponseRecorder{}
			reqBody, err := json.Marshal(tc.req)
			assert.NoError(t, err)

			req, err := http.NewRequest(http.MethodGet, "", bytes.NewReader(reqBody))
			assert.NoError(t, err)
			pathParams := map[string]string{}

			srv.CreateChatCompletion(w, req, pathParams)

			assert.Equal(t, tc.code, w.Code)
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

type fakeModelClient struct {
	models map[string]*mv1.Model
}

func (c *fakeModelClient) GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error) {
	model, ok := c.models[in.Id]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "model not found")
	}
	return model, nil
}

type fakeEngineGetter struct {
	addr string
}

func (g *fakeEngineGetter) GetEngineForModel(ctx context.Context, modelID string) (string, error) {
	return g.addr, nil
}
