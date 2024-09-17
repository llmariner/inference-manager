package rag

import (
	"context"

	"github.com/go-logr/logr"
	v1 "github.com/llm-operator/inference-manager/api/v1"
	vsv1 "github.com/llm-operator/vector-store-manager/api/v1"
	"google.golang.org/grpc"
)

const (
	// Refer to https://community.openai.com/t/prompt-engineering-for-rag/621495/3 for the prompt choice.
	prompt string = "Answer the users QUESTION using the DOCUMENT text below. Keep your answer ground in the facts of the DOCUMENT. If the DOCUMENT doesn’t contain the facts to answer the QUESTION return {NONE}"
)

// VectorStoreInternalClient is an interface for a vector store internal GRPC client.
type VectorStoreInternalClient interface {
	SearchVectorStore(ctx context.Context, req *vsv1.SearchVectorStoreRequest, opts ...grpc.CallOption) (*vsv1.SearchVectorStoreResponse, error)
}

// R is for rag.
type R struct {
	vsInternalClient VectorStoreInternalClient
	enableAuth       bool
	logger           logr.Logger
}

// NewR creates a new R instance.
func NewR(enableAuth bool, vsInernalClient VectorStoreInternalClient, logger logr.Logger) *R {
	return &R{
		enableAuth:       enableAuth,
		vsInternalClient: vsInernalClient,
		logger:           logger.WithName("rag"),
	}
}

// ProcessMessages processes chat completion messages.
func (r *R) ProcessMessages(
	ctx context.Context,
	vstore *vsv1.VectorStore,
	messages []*v1.CreateChatCompletionRequest_Message,
) ([]*v1.CreateChatCompletionRequest_Message, error) {
	r.logger.Info("Processing messages", "store", vstore.Name)

	var msgs []*v1.CreateChatCompletionRequest_Message
	for _, msg := range messages {
		searchResp, err := r.vsInternalClient.SearchVectorStore(ctx, &vsv1.SearchVectorStoreRequest{
			VectorStoreId: vstore.Id,
			Query:         msg.Content,
		})
		if err != nil {
			return nil, err
		}
		r.logger.Info("Found documents", "count", len(searchResp.Documents), "store", vstore.Name, "query", msg.Content)
		for _, doc := range searchResp.Documents {
			msgs = append(msgs, &v1.CreateChatCompletionRequest_Message{
				Content: doc,
				Role:    "assistant",
			})
		}
	}
	if len(msgs) > 0 {
		msgs = append([]*v1.CreateChatCompletionRequest_Message{
			{
				Role:    "system",
				Content: prompt,
			}}, msgs...)
	}
	return append(msgs, messages...), nil
}
