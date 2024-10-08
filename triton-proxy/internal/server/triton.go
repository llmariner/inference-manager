package server

import (
	"fmt"

	v1 "github.com/llmariner/inference-manager/api/v1"
)

type ensembleGenerateRequest struct {
	TextInput string `json:"text_input"`
	MaxTokens int    `json:"max_tokens"`
	BadWords  string `json:"bad_words"`
	StopWords string `json:"stop_words"`
}

func buildEnsembleGenerateRequest(req *v1.CreateChatCompletionRequest) *ensembleGenerateRequest {
	// TODO(kenji): Revisit. This only works for Llama3.1. We should also fill other fields.
	// this to a template.
	input := "<|begin_of_text|>"
	for _, m := range req.Messages {
		input += fmt.Sprintf("<|start_header_id|> %s <|end_header_id|>\n %s '\n<|eot_id|>\n", m.Role, m.Content)
	}
	return &ensembleGenerateRequest{
		TextInput: input,
		// TODO(kenji): Revisit.
		MaxTokens: 1024,
		BadWords:  "",
		StopWords: "",
	}
}

type ensembleGenerateResponse struct {
	TextOutput string `json:"text_output"`
}

func buildChatCompletionResponse(resp *ensembleGenerateResponse, modelID string) *v1.ChatCompletion {
	return &v1.ChatCompletion{
		Choices: []*v1.ChatCompletion_Choice{
			{
				Message: &v1.ChatCompletion_Choice_Message{
					Content: resp.TextOutput,
				},
			},
		},
		Model:  modelID,
		Object: "chat.completion",
		// TODO(kenji): Fill other fields.
	}
}
