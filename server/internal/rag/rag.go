package rag

import (
	"context"
	"fmt"
	"log"

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

// VectorStoreClient is an interface for a vector store client.
type VectorStoreClient interface {
	ListVectorStores(ctx context.Context, req *vsv1.ListVectorStoresRequest, opts ...grpc.CallOption) (*vsv1.ListVectorStoresResponse, error)
}

// R is for rag.
type R struct {
	vsClient         VectorStoreClient
	vsInternalClient VectorStoreInternalClient
	enableAuth       bool
}

// NewR creates a new R instance.
func NewR(enableAuth bool, vsClient VectorStoreClient, vsInernalClient VectorStoreInternalClient) *R {
	return &R{
		enableAuth:       enableAuth,
		vsClient:         vsClient,
		vsInternalClient: vsInernalClient,
	}
}

// ProcessMessages processes chat completion messages.
func (r *R) ProcessMessages(
	ctx context.Context,
	vectorStoreName string,
	messages []*v1.CreateChatCompletionRequest_Message,
) ([]*v1.CreateChatCompletionRequest_Message, error) {
	log.Printf("Processing messages for vector store %s\n", vectorStoreName)

	listResp, err := r.vsClient.ListVectorStores(ctx, &vsv1.ListVectorStoresRequest{})
	if err != nil {
		return nil, err
	}

	var vstore *vsv1.VectorStore
	for _, vs := range listResp.Data {
		if vs.Name == vectorStoreName {
			vstore = vs
			break
		}
	}
	if vstore == nil {
		return nil, fmt.Errorf("vector store %s not found", vectorStoreName)
	}

	var msgs []*v1.CreateChatCompletionRequest_Message
	for _, msg := range messages {
		searchResp, err := r.vsInternalClient.SearchVectorStore(ctx, &vsv1.SearchVectorStoreRequest{
			VectorStoreId: vstore.Id,
			Query:         msg.Content,
		})
		if err != nil {
			return nil, err
		}
		log.Printf("Found %d documents from %s for query %s\n", len(searchResp.Documents), vectorStoreName, msg.Content)
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
