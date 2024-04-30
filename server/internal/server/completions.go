package server

import (
	"context"
	"log"

	v1 "github.com/llm-operator/inference-manager/api/v1"
)

// CreateChatCompletion creates a chat completion.
func (s *S) CreateChatCompletion(
	ctx context.Context,
	req *v1.CreateChatCompletionRequest,
) (*v1.ChatCompletion, error) {
	log.Printf("Received a CreateChatCompletion request: %+v\n", req)

	return handleChatRequest(ctx, req)
}
