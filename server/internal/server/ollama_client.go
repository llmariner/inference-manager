package server

import (
	"context"
	"log"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/ollama/ollama/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newClient() (*api.Client, error) {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return nil, err
	}
	return client, nil
}

func handleChatRequest(ctx context.Context, req *v1.CreateChatCompletionRequest) (*v1.ChatCompletion, error) {
	client, err := newClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to create a client: %s", err)
	}

	var msgs []api.Message
	for _, msg := range req.Messages {
		msgs = append(msgs, api.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	ollamaReq := &api.ChatRequest{
		Model:    req.Model,
		Messages: msgs,
	}

	var ollamaResp api.ChatResponse
	fn := func(resp api.ChatResponse) error {
		ollamaResp = resp
		return processChatResponse(resp)
	}

	if err := client.Chat(ctx, ollamaReq, fn); err != nil {
		log.Printf("Failed to create a chat completion: %v\n", err)
		return nil, status.Errorf(codes.Internal, "Failed to create a chat completion: %s", err)
	}

	return &v1.ChatCompletion{
		Id: "fake-id",
		Choices: []*v1.ChatCompletion_Choice{
			{
				Message: &v1.ChatCompletion_Choice_Message{
					Content: ollamaResp.Message.Content,
				},
			},
		},
		Model: req.Model,
		Usage: &v1.ChatCompletion_Usage{
			CompletionTokens: int32(ollamaResp.EvalCount),
			PromptTokens:     int32(ollamaResp.PromptEvalCount),
			TotalTokens:      int32(ollamaResp.EvalCount + ollamaResp.PromptEvalCount),
		},
	}, nil
}

func processChatResponse(resp api.ChatResponse) error {
	// TODO(guangrui): process and publish metrics.
	return nil
}
