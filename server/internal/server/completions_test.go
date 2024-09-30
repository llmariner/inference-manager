package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	testutil "github.com/llm-operator/inference-manager/common/pkg/test"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llmariner/api-usage/pkg/sender"
	vsv1 "github.com/llmariner/vector-store-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateChatCompletion(t *testing.T) {
	const modelID = "m0"

	srv := New(
		&fakeMetricsMonitor{},
		&sender.NoopUsageSetter{},
		&fakeModelClient{
			models: map[string]*mv1.Model{
				modelID: {},
			},
		},
		&fakeVectorStoreClient{
			vs: &vsv1.VectorStore{
				Name: "test",
			},
		},
		&fakeRewriter{},
		&fakeTaskSender{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte{})),
			}},
		testutil.NewTestLogger(t),
	)
	srv.enableAuth = true

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
		{
			name: "valid tools",
			req: &v1.CreateChatCompletionRequest{
				Model: modelID,
				ToolChoice: &v1.CreateChatCompletionRequest_ToolChoice{
					Choice: string(autoToolChoice),
					Type:   functionObjectType,
					Function: &v1.CreateChatCompletionRequest_ToolChoice_Function{
						Name: "test",
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role:    "user",
						Content: "test",
					},
				},
			},
			code: http.StatusOK,
		},
		{
			name: "valid rag",
			req: &v1.CreateChatCompletionRequest{
				Model: modelID,
				ToolChoice: &v1.CreateChatCompletionRequest_ToolChoice{
					Choice: string(autoToolChoice),
					Type:   functionObjectType,
					Function: &v1.CreateChatCompletionRequest_ToolChoice_Function{
						Name: ragToolName,
					},
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:       ragToolName,
							Parameters: `{"vector_store_name":"test"}`,
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role:    "user",
						Content: "test",
					},
				},
			},
			code: http.StatusOK,
		},
		{
			name: "invalid vector store name",
			req: &v1.CreateChatCompletionRequest{
				Model: modelID,
				ToolChoice: &v1.CreateChatCompletionRequest_ToolChoice{
					Choice: string(autoToolChoice),
					Type:   functionObjectType,
					Function: &v1.CreateChatCompletionRequest_ToolChoice_Function{
						Name: ragToolName,
					},
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:       ragToolName,
							Parameters: `{"vector_store_name":"invalid_name"}`,
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role:    "user",
						Content: "test",
					},
				},
			},
			code: http.StatusBadRequest,
		},
		{
			name: "skip rag",
			req: &v1.CreateChatCompletionRequest{
				Model: modelID,
				ToolChoice: &v1.CreateChatCompletionRequest_ToolChoice{
					Choice: string(noneToolChoice),
					Type:   functionObjectType,
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:       ragToolName,
							Parameters: `{"vector_store_name":"test"}`,
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role:    "user",
						Content: "test",
					},
				},
			},
			code: http.StatusOK,
		},
		{
			name: "invalid rag parameter",
			req: &v1.CreateChatCompletionRequest{
				Model: modelID,
				ToolChoice: &v1.CreateChatCompletionRequest_ToolChoice{
					Choice: string(autoToolChoice),
					Type:   functionObjectType,
					Function: &v1.CreateChatCompletionRequest_ToolChoice_Function{
						Name: ragToolName,
					},
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:       ragToolName,
							Parameters: `{"vector_store":"test"}`,
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role:    "user",
						Content: "test",
					},
				},
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

type fakeRewriter struct {
}

func (c *fakeRewriter) ProcessMessages(
	ctx context.Context,
	vstore *vsv1.VectorStore,
	messages []*v1.CreateChatCompletionRequest_Message,
) ([]*v1.CreateChatCompletionRequest_Message, error) {
	return messages, nil
}

type fakeMetricsMonitor struct {
}

func (m *fakeMetricsMonitor) ObserveCompletionLatency(modelID string, latency time.Duration) {
}

func (m *fakeMetricsMonitor) UpdateCompletionRequest(modelID string, c int) {
}

func (m *fakeMetricsMonitor) ObserveEmbeddingLatency(modelID string, latency time.Duration) {
}

func (m *fakeMetricsMonitor) UpdateEmbeddingRequest(modelID string, c int) {
}

type fakeVectorStoreClient struct {
	vs *vsv1.VectorStore
}

func (c *fakeVectorStoreClient) GetVectorStoreByName(
	ctx context.Context,
	req *vsv1.GetVectorStoreByNameRequest,
	opts ...grpc.CallOption,
) (*vsv1.VectorStore, error) {
	if req.Name != c.vs.Name {
		return nil, status.Errorf(codes.NotFound, "%s not found", req.Name)
	}
	return c.vs, nil
}

type fakeTaskSender struct {
	resp *http.Response
	err  error
}

func (s *fakeTaskSender) SendChatCompletionTask(ctx context.Context, tenantID string, req *v1.CreateChatCompletionRequest, header http.Header) (*http.Response, error) {
	return s.resp, s.err
}

func (s *fakeTaskSender) SendEmbeddingTask(ctx context.Context, tenantID string, req *v1.CreateEmbeddingRequest, header http.Header) (*http.Response, error) {
	return s.resp, s.err
}
