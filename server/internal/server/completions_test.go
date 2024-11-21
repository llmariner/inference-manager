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

	"github.com/llmariner/api-usage/pkg/sender"
	v1 "github.com/llmariner/inference-manager/api/v1"
	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/llmariner/inference-manager/server/internal/rate"
	mv1 "github.com/llmariner/model-manager/api/v1"
	vsv1 "github.com/llmariner/vector-store-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateChatCompletion(t *testing.T) {
	const modelID = "m0"

	logger := testutil.NewTestLogger(t)

	srv := New(
		&fakeMetricsMonitor{},
		&sender.NoopUsageSetter{},
		rate.NewLimiter(rate.Config{}, logger),
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
		logger,
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
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "test",
							},
						},
					},
				},
			},
			code: http.StatusOK,
		},
		{
			name: "no model",
			req: &v1.CreateChatCompletionRequest{
				Model: "m1",
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "test",
							},
						},
					},
				},
			},
			code: http.StatusBadRequest,
		},
		{
			name: "no message",
			req: &v1.CreateChatCompletionRequest{
				Model: "m0",
			},
			code: http.StatusBadRequest,
		},
		{
			name: "no content",
			req: &v1.CreateChatCompletionRequest{
				Model: "m0",
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
					},
				},
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
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "test",
							},
						},
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
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "test",
							},
						},
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
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "test",
							},
						},
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
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "test",
							},
						},
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
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "test",
							},
						},
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

func TestConvertContentStringToArray(t *testing.T) {
	tcs := []struct {
		name string
		body string
		want *v1.CreateChatCompletionRequest
	}{

		{
			name: "no conversion",
			body: `
{
	"messages": [
		{
			"role": "user",
			"content": [
				{
					 "type": "text",
					 "text": "Process audio data."
				},
				{
					 "type": "input_audio",
					 "input_audio": {
						 "data": "audiodata",
						 "format": "wav"
					 }
				}
			]
		}
	]
}`,
			want: &v1.CreateChatCompletionRequest{
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Process audio data.",
							},
							{
								Type: "input_audio",
								InputAudio: &v1.CreateChatCompletionRequest_Message_Content_InputAudio{
									Data:   "audiodata",
									Format: "wav",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "convertion",
			body: `
{
	"messages": [
		{
			"role": "system",
			"content": "You are a helpful assistant."
		}
	]
}`,
			want: &v1.CreateChatCompletionRequest{
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "system",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "You are a helpful assistant.",
							},
						},
					},
				},
			},
		},
		{
			name: "mix",
			body: `
			{
				"messages": [
					{
						"role": "system",
						"content": "You are a helpful assistant."
					},
					{
						"role": "user",
						"content": [
				{
					 "type": "text",
					 "text": "Process audio data."
				},
				{
					 "type": "input_audio",
					 "input_audio": {
						 "data": "audiodata",
						 "format": "wav"
					 }
				}
						]
					}
				]
			}`,
			want: &v1.CreateChatCompletionRequest{
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "system",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "You are a helpful assistant.",
							},
						},
					},
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Process audio data.",
							},
							{
								Type: "input_audio",
								InputAudio: &v1.CreateChatCompletionRequest_Message_Content_InputAudio{
									Data:   "audiodata",
									Format: "wav",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			body, _, err := convertContentStringToArray([]byte(tc.body))
			assert.NoError(t, err)

			var req v1.CreateChatCompletionRequest
			err = json.Unmarshal(body, &req)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, &req)
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
