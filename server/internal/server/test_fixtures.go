package server

import (
	"encoding/base64"

	v1 "github.com/llmariner/inference-manager/api/v1"
)

// CreateChatCompletionRequestFixture creates a test fixture representing a CreateChatCompletionRequest with default parameters.
// The function accepts optional mutators to modify the generated request before returning it.
func CreateChatCompletionRequestFixture(mutators ...func(c *v1.CreateChatCompletionRequest)) *v1.CreateChatCompletionRequest {
	r := &v1.CreateChatCompletionRequest{
		Model: "defaultTestModel",
		Messages: []*v1.CreateChatCompletionRequest_Message{
			{
				Role: "user",
				Content: []*v1.CreateChatCompletionRequest_Message_Content{
					{Type: "text", Text: "test"},
				},
			},
		},
	}
	for _, m := range mutators {
		m(r)
	}
	return r
}

// WithModel is a mutator for the CreateChatCompletionRequestFixture to set the model id
func WithModel(modelID string) func(c *v1.CreateChatCompletionRequest) {
	return func(c *v1.CreateChatCompletionRequest) {
		c.Model = modelID
	}
}

// WithRAG is a mutator for the CreateChatCompletionRequestFixture to set a default RAG config for testing
func WithRAG(c *v1.CreateChatCompletionRequest) {
	c.ToolChoiceObject = &v1.CreateChatCompletionRequest_ToolChoice{
		Type: functionObjectType,
		Function: &v1.CreateChatCompletionRequest_ToolChoice_Function{
			Name: ragToolName,
		},
	}
	c.Tools = []*v1.CreateChatCompletionRequest_Tool{
		{
			Type: functionObjectType,
			Function: &v1.CreateChatCompletionRequest_Tool_Function{
				Name:              ragToolName,
				EncodedParameters: base64.URLEncoding.EncodeToString([]byte(`{"vector_store_name":"test"}`)),
			},
		},
	}
}
