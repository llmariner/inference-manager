package server

import (
	"testing"

	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestBuildEnsembleGenerateRequest(t *testing.T) {
	req := &v1.CreateChatCompletionRequest{
		Messages: []*v1.CreateChatCompletionRequest_Message{
			{
				Content: []*v1.CreateChatCompletionRequest_Message_Content{
					{
						Type: "text",
						Text: "hello",
					},
				},
				Role: "user",
			},
			{
				Content: []*v1.CreateChatCompletionRequest_Message_Content{
					{
						Type: "text",
						Text: "world",
					},
				},
				Role: "system",
			},
		},
	}
	got := buildEnsembleGenerateRequest(req)
	want := "<|begin_of_text|><|start_header_id|> user <|end_header_id|>\n hello '\n<|eot_id|>\n<|start_header_id|> system <|end_header_id|>\n world '\n<|eot_id|>\n"
	assert.Equal(t, want, got.TextInput)
}
