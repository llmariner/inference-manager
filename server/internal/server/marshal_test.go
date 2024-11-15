package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnmarshal(t *testing.T) {
	body := `
{
  "model": "gpt-4o",
    "messages": [
      {
        "role": "system",
        "content": "You are a helpful assistant."
      },
      {
        "role": "user",
        "content": [
          {
             "type": "input_audio",
             "input_audio": {
               "data": "auditdata",
               "format": "wav"
             }
          }
        ]
      }
    ]
}`
	// Unmarshal the JSON string into a struct.
	req := map[string]interface{}{}
	err := json.Unmarshal([]byte(body), &req)
	assert.NoError(t, err)

	/*


		var req v1.CreateChatCompletionRequest
		err := json.Unmarshal([]byte(body), &req)
		assert.NoError(t, err)
		assert.Equal(t, "You are a helpful assistant.", req.Messages[0].Content)
	*/
}
