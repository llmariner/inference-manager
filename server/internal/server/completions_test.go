package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestCreateChatCompletion(t *testing.T) {
	const modelID = "m0"

	logger := testutil.NewTestLogger(t)
	capturingTaskSender := &captureChatRequestTaskSender{}
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
		capturingTaskSender,
		logger,
	)
	srv.enableAuth = true

	tcs := []struct {
		name    string
		req     *v1.CreateChatCompletionRequest
		expCode int
		assert  func(t *testing.T, req *v1.CreateChatCompletionRequest)
	}{
		{
			name:    "success",
			req:     CreateChatCompletionRequestFixture(WithModel(modelID)),
			expCode: http.StatusOK,
		},
		{
			name:    "no model",
			req:     CreateChatCompletionRequestFixture(WithModel("unknown")),
			expCode: http.StatusBadRequest,
		},
		{
			name: "no message",
			req: &v1.CreateChatCompletionRequest{
				Model: "m0",
			},
			expCode: http.StatusBadRequest,
		},
		{
			name: "no content",
			req: &v1.CreateChatCompletionRequest{
				Model: "m0",
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{Role: "user"},
				},
			},
			expCode: http.StatusBadRequest,
		},
		{
			name: "valid tools",
			req: CreateChatCompletionRequestFixture(WithModel(modelID), func(c *v1.CreateChatCompletionRequest) {
				c.ToolChoiceObject = &v1.CreateChatCompletionRequest_ToolChoice{
					Type: functionObjectType,
					Function: &v1.CreateChatCompletionRequest_ToolChoice_Function{
						Name: "test",
					},
				}
			}),
			expCode: http.StatusOK,
		},
		{
			name:    "valid rag",
			req:     CreateChatCompletionRequestFixture(WithModel(modelID), WithRAG),
			expCode: http.StatusOK,
		},
		{
			name: "invalid vector store name",
			req: CreateChatCompletionRequestFixture(WithModel(modelID), WithRAG, func(c *v1.CreateChatCompletionRequest) {
				c.Tools = []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:              ragToolName,
							EncodedParameters: base64.URLEncoding.EncodeToString([]byte(`{"vector_store_name":"invalid_name"}`)),
						},
					},
				}
			}),
			expCode: http.StatusBadRequest,
		},
		{
			name: "skip rag",
			req: CreateChatCompletionRequestFixture(WithModel(modelID), WithRAG, func(c *v1.CreateChatCompletionRequest) {
				c.ToolChoice = string(noneToolChoice)
			}),
			expCode: http.StatusOK,
		},
		{
			name: "invalid rag parameter",
			req: CreateChatCompletionRequestFixture(WithModel(modelID), WithRAG, func(c *v1.CreateChatCompletionRequest) {
				c.Tools = []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:              ragToolName,
							EncodedParameters: base64.URLEncoding.EncodeToString([]byte(`{"vector_store":"test"}`)),
						},
					},
				}
			}),
			expCode: http.StatusBadRequest,
		},
		{
			name: "MaxCompletionTokens not set - use MaxTokens as default",
			req: CreateChatCompletionRequestFixture(WithModel(modelID), func(c *v1.CreateChatCompletionRequest) {
				c.MaxTokens = 100
			}),
			expCode: http.StatusOK,
			assert: func(t *testing.T, req *v1.CreateChatCompletionRequest) {
				assert.Equal(t, int32(100), req.MaxCompletionTokens)
			},
		},
		{
			name: "MaxCompletionTokens and MaxTokens equal value set",
			req: CreateChatCompletionRequestFixture(WithModel(modelID), func(c *v1.CreateChatCompletionRequest) {
				c.MaxTokens = 100
				c.MaxCompletionTokens = 100
			}),
			expCode: http.StatusOK,
			assert: func(t *testing.T, req *v1.CreateChatCompletionRequest) {
				assert.Equal(t, int32(100), req.MaxCompletionTokens)
			},
		},
		{
			name: "MaxCompletionTokens and MaxTokens different value set",
			req: CreateChatCompletionRequestFixture(WithModel(modelID), func(c *v1.CreateChatCompletionRequest) {
				c.MaxTokens = 100
				c.MaxCompletionTokens = 200
			}),
			expCode: http.StatusBadRequest,
		},
		{
			name: "MaxCompletionTokens set, no MaxTokens defined",
			req: CreateChatCompletionRequestFixture(WithModel(modelID), func(c *v1.CreateChatCompletionRequest) {
				c.MaxCompletionTokens = 200
			}),
			expCode: http.StatusOK,
			assert: func(t *testing.T, req *v1.CreateChatCompletionRequest) {
				assert.Equal(t, int32(200), req.MaxCompletionTokens)
				assert.Equal(t, int32(200), req.MaxTokens, "legacy support of MaxTokens for Ollama")
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			capturingTaskSender.reset()
			w := &httptest.ResponseRecorder{}
			reqBody, err := json.Marshal(tc.req)
			assert.NoError(t, err)

			req, err := http.NewRequest(http.MethodGet, "", bytes.NewReader(reqBody))
			assert.NoError(t, err)
			pathParams := map[string]string{}

			srv.CreateChatCompletion(w, req, pathParams)
			assert.Equal(t, tc.expCode, w.Code)

			if tc.assert != nil {
				tc.assert(t, capturingTaskSender.capturedReq)
			}

		})
	}
}

func TestHandleToolsForRAG(t *testing.T) {
	srv := &S{
		vsClient: &fakeVectorStoreClient{
			vs: &vsv1.VectorStore{
				Name: "test",
			},
		},
		rewriter: &fakeRewriter{
			msg: &v1.CreateChatCompletionRequest_Message{
				Role: "user",
				Content: []*v1.CreateChatCompletionRequest_Message_Content{
					{
						Type: "text",
						Text: "RAG info",
					},
				},
			},
		},
	}

	tcs := []struct {
		name    string
		req     *v1.CreateChatCompletionRequest
		want    *v1.CreateChatCompletionRequest
		wantErr bool
	}{
		{
			name: "rag",
			req: &v1.CreateChatCompletionRequest{
				ToolChoiceObject: &v1.CreateChatCompletionRequest_ToolChoice{
					Type: functionObjectType,
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:              ragToolName,
							EncodedParameters: base64.URLEncoding.EncodeToString([]byte(`{"vector_store_name":"test"}`)),
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
				},
			},
			want: &v1.CreateChatCompletionRequest{
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "RAG info",
							},
						},
					},
				},
			},
		},
		{
			name: "rag - invalid vector store name",
			req: &v1.CreateChatCompletionRequest{
				ToolChoiceObject: &v1.CreateChatCompletionRequest_ToolChoice{
					Type: functionObjectType,
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:              ragToolName,
							EncodedParameters: base64.URLEncoding.EncodeToString([]byte(`{"vector_store_name":"invalid"}`)),
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "other tool",
			req: &v1.CreateChatCompletionRequest{
				ToolChoiceObject: &v1.CreateChatCompletionRequest_ToolChoice{
					Type: functionObjectType,
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name: "get_weacher",
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
				},
			},
			want: &v1.CreateChatCompletionRequest{
				ToolChoiceObject: &v1.CreateChatCompletionRequest_ToolChoice{
					Type: functionObjectType,
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name: "get_weacher",
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
				},
			},
		},
		{
			name: "rag and other tools",
			req: &v1.CreateChatCompletionRequest{
				ToolChoiceObject: &v1.CreateChatCompletionRequest_ToolChoice{
					Type: functionObjectType,
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:              ragToolName,
							EncodedParameters: base64.URLEncoding.EncodeToString([]byte(`{"vector_store_name":"test"}`)),
						},
					},
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name: "get_weacher",
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
				},
			},
			want: &v1.CreateChatCompletionRequest{
				ToolChoiceObject: &v1.CreateChatCompletionRequest_ToolChoice{
					Type: functionObjectType,
				},
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name: "get_weacher",
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "RAG info",
							},
						},
					},
				},
			},
		},
		{
			name: "rag with none tool choice",
			req: &v1.CreateChatCompletionRequest{
				ToolChoice: string(noneToolChoice),
				Tools: []*v1.CreateChatCompletionRequest_Tool{
					{
						Type: functionObjectType,
						Function: &v1.CreateChatCompletionRequest_Tool_Function{
							Name:              ragToolName,
							EncodedParameters: base64.URLEncoding.EncodeToString([]byte(`{"vector_store_name":"test"}`)),
						},
					},
				},
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
				},
			},
			want: &v1.CreateChatCompletionRequest{
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
				},
			},
		},
		{
			name: "no tools",
			req: &v1.CreateChatCompletionRequest{
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
				},
			},
			want: &v1.CreateChatCompletionRequest{
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role: "user",
						Content: []*v1.CreateChatCompletionRequest_Message_Content{
							{
								Type: "text",
								Text: "Hello",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.handleToolsForRAG(context.Background(), tc.req)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Truef(t, proto.Equal(tc.want, tc.req), cmp.Diff(tc.want, tc.req, protocmp.Transform()))
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

func (c *fakeModelClient) ActivateModel(ctx context.Context, in *mv1.ActivateModelRequest, opts ...grpc.CallOption) (*mv1.ActivateModelResponse, error) {
	return &mv1.ActivateModelResponse{}, nil
}

type fakeRewriter struct {
	msg *v1.CreateChatCompletionRequest_Message
}

func (c *fakeRewriter) ProcessMessages(
	ctx context.Context,
	vstore *vsv1.VectorStore,
	messages []*v1.CreateChatCompletionRequest_Message,
) ([]*v1.CreateChatCompletionRequest_Message, error) {
	return append(messages, c.msg), nil
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

func (m *fakeMetricsMonitor) ObserveRequestCount(modelID, tenantID string, statusCode int32) {
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

type captureChatRequestTaskSender struct {
	capturedReq *v1.CreateChatCompletionRequest
}

// reset clears internal state
func (s *captureChatRequestTaskSender) reset() {
	s.capturedReq = nil
}
func (s *captureChatRequestTaskSender) SendChatCompletionTask(ctx context.Context, tenantID string, req *v1.CreateChatCompletionRequest, header http.Header) (*http.Response, error) {
	s.capturedReq = req
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte{}))}, nil
}

func (s *captureChatRequestTaskSender) SendEmbeddingTask(ctx context.Context, tenantID string, req *v1.CreateEmbeddingRequest, header http.Header) (*http.Response, error) {
	panic("not implemented")
}
