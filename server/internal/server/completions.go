package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CreateChatCompletion creates a chat completion.
func (s *S) CreateChatCompletion(
	ctx context.Context,
	req *v1.CreateChatCompletionRequest,
) (*v1.ChatCompletion, error) {
	log.Printf("Received a CreateChatCompletion request: %+v\n", req)

	if req.Stream {
		return nil, status.Error(codes.Unimplemented, "Streaming chat completions is not supported")
	}

	client := newClient(s.ollamaServerAddr)

	bytes, err := client.sendRequest(ctx, http.MethodPost, "/v1/chat/completions", req)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to create a chat completion: %s", err)
	}

	var resp v1.ChatCompletion
	if err := json.Unmarshal(bytes, &resp); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to unmarshal chat completion response: %s", err)
	}

	if err := processChatCompletionResponse(&resp); err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to process chat completion response: %s", err)
	}
	return &resp, nil
}

func processChatCompletionResponse(resp *v1.ChatCompletion) error {
	// TODO(guangrui): process and publish metrics.
	log.Printf("Received a chat completion: %+v\n", resp)
	return nil
}
