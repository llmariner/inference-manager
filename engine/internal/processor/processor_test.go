package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	v1 "github.com/llmariner/inference-manager/api/v1"
	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/llmariner/inference-manager/engine/internal/metrics"
	"github.com/stretchr/testify/assert"
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
		NewFixedAddressGetter(fmt.Sprintf("localhost:%d", ollamaSrv.port())),
		&fakeModelSyncer{},
		logger,
		&metrics.NoopCollector{},
		time.Second,
	)

	fakeClient := &fakeProcessTasksClient{ctx: ctx}

	task := &v1.Task{
		Request: &v1.TaskRequest{
			Request: &v1.TaskRequest_ChatCompletion{
				ChatCompletion: &v1.CreateChatCompletionRequest{
					Model: "m0",
				},
			},
		},
	}

	err = processor.processTask(ctx, fakeClient, task)
	assert.NoError(t, err)
	resp := fakeClient.gotReq.GetTaskResult().GetHttpResponse()
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
		NewFixedAddressGetter(fmt.Sprintf("localhost:%d", ollamaSrv.port())),
		&fakeModelSyncer{},
		logger,
		&metrics.NoopCollector{},
		time.Second,
	)

	fakeClient := &fakeProcessTasksClient{ctx: ctx}

	task := &v1.Task{
		Request: &v1.TaskRequest{
			Request: &v1.TaskRequest_Embedding{
				Embedding: &v1.CreateEmbeddingRequest{
					Model: "m0",
				},
			},
		},
	}

	err = processor.processTask(ctx, fakeClient, task)
	assert.NoError(t, err)
	resp := fakeClient.gotReq.GetTaskResult().GetHttpResponse()
	assert.Equal(t, http.StatusOK, int(resp.StatusCode))
	assert.Equal(t, "ok", string(resp.Body))
}

func TestConvertToolChoiceObject(t *testing.T) {
	tcs := []struct {
		name string
		body string
		want string
	}{
		{
			name: "string tool choice",
			body: `{"tool_choice": "auto"}`,
			want: `{"tool_choice": "auto"}`,
		},
		{
			name: "object tool choice",
			body: `{"tool_choice_object":{"function":{"name":"test"},"type":"function"}}`,
			want: `{"tool_choice":{"function":{"name":"test"},"type":"function"}}`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertToolChoiceObject([]byte(tc.body))
			assert.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestConvertEncodedFunctionParameters(t *testing.T) {
	reqBody := `
{
"tools": [{
  "type": "function",
  "function": {
     "name": "get_weather",
     "description": "Get current temperature for a given location.",
     "encoded_parameters": "eyJwcm9wZXJ0aWVzIjp7ImxvY2F0aW9uIjp7ImRlc2NyaXB0aW9uIjoiQ2l0eSBhbmQgY291bnRyeSIsInR5cGUiOiJzdHJpbmcifX0sInJlcXVpcmVkIjpbImxvY2F0aW9uIl0sInR5cGUiOiJvYmplY3QifQ==",
      "strict": true
    }
}]}`
	got, err := convertEncodedFunctionParameters([]byte(reqBody))
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal(got, &r)
	assert.NoError(t, err)
	tools, ok := r["tools"]
	assert.True(t, ok)
	assert.Len(t, tools.([]interface{}), 1)
	tool := tools.([]interface{})[0].(map[string]interface{})
	f, ok := tool["function"]
	assert.True(t, ok)
	fn := f.(map[string]interface{})

	_, ok = fn["encoded_parameters"]
	assert.False(t, ok)

	p, ok := fn["parameters"]
	assert.True(t, ok)

	gotR := p.(map[string]interface{})
	wantR := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "City and country",
			},
		},
		"required": []interface{}{"location"},
	}
	assert.Equal(t, wantR, gotR)
}

func TestConvertEncodedInput(t *testing.T) {
	tcs := []struct {
		name string
		body string
		want string
	}{
		{
			name: "string input",
			body: `{"input": "The food was delicious."}`,
			want: `{"input": "The food was delicious."}`,
		},
		{
			name: "string input",
			body: `{"encoded_input": "WyJhIiwiYiJd"}`,
			want: `{"input":["a","b"]}`,
		},
		{
			name: "string input",
			body: `{"encoded_input": "WzEsMl0="}`,
			want: `{"input":[1,2]}`,
		},
		{
			name: "string input",
			body: `{"encoded_input": "W1sxXSxbMl1d"}`,
			want: `{"input":[[1],[2]]}`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertEncodedInput([]byte(tc.body))
			assert.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
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

type fakeModelSyncer struct {
}

func (f *fakeModelSyncer) ListSyncedModelIDs() []string {
	return nil
}

func (f *fakeModelSyncer) PullModel(ctx context.Context, modelID string) error {
	return nil
}

func (f *fakeModelSyncer) ListInProgressModels() []string {
	return nil
}

type fakeProcessTasksClient struct {
	ctx    context.Context
	gotReq *v1.ProcessTasksRequest
}

func (c *fakeProcessTasksClient) Context() context.Context {
	return c.ctx
}

func (c *fakeProcessTasksClient) Send(req *v1.ProcessTasksRequest) error {
	c.gotReq = req
	return nil
}
