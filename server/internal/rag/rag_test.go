package rag

import (
	"context"
	"testing"

	v1 "github.com/llmariner/inference-manager/api/v1"
	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	vsv1 "github.com/llmariner/vector-store-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func TestProcessMessages(t *testing.T) {
	query := "what sky is red?"
	r := NewR(
		true,
		&fakeVectorStoreInternalClient{
			query: query,
			docs:  []string{"sky is red when the sun is setting", "sky is blue when the sun is shining"},
		},
		testutil.NewTestLogger(t),
	)

	vs := &vsv1.VectorStore{
		Id:   "default-id",
		Name: "default",
	}

	tcs := []struct {
		name   string
		vsName string
		req    *v1.CreateChatCompletionRequest
		exp    []*v1.CreateChatCompletionRequest_Message
		err    bool
	}{
		{
			name:   "success",
			vsName: "default",
			req: &v1.CreateChatCompletionRequest{
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role:    "user",
						Content: query,
					},
				},
			},
			exp: []*v1.CreateChatCompletionRequest_Message{
				{
					Role:    "system",
					Content: prompt,
				},
				{
					Role:    "assistant",
					Content: "sky is red when the sun is setting",
				},
				{
					Role:    "assistant",
					Content: "sky is blue when the sun is shining",
				},
				{
					Role:    "user",
					Content: query,
				},
			},
		},
		{
			name:   "docs not found",
			vsName: "default",
			req: &v1.CreateChatCompletionRequest{
				Messages: []*v1.CreateChatCompletionRequest_Message{
					{
						Role:    "user",
						Content: "unknown",
					},
				},
			},
			exp: []*v1.CreateChatCompletionRequest_Message{
				{
					Role:    "user",
					Content: "unknown",
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.ProcessMessages(context.Background(), vs, tc.req.Messages)
			assert.Equal(t, tc.err, err != nil)
			if err != nil {
				return
			}
			assert.Equal(t, tc.exp, got)
		})
	}
}

type fakeVectorStoreInternalClient struct {
	query string
	docs  []string
}

func (c *fakeVectorStoreInternalClient) SearchVectorStore(
	ctx context.Context,
	req *vsv1.SearchVectorStoreRequest,
	opts ...grpc.CallOption,
) (*vsv1.SearchVectorStoreResponse, error) {
	if c.query != req.Query {
		return &vsv1.SearchVectorStoreResponse{}, nil
	}
	return &vsv1.SearchVectorStoreResponse{
		Documents: c.docs,
	}, nil
}
