package vllm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecodeListModelsResponse(t *testing.T) {
	body := `
{"object":"list","data":[{"id":"meta-llama-Llama-3.2-1B-Instruct","object":"model","created":1745013322,"owned_by":"vllm","root":"/models/meta-llama-Llama-3.2-1B-Instruct","parent":null,"max_model_len":131072,"permission":[{"id":"modelperm-91177890c6b54610876d0b3cae1b6e67","object":"model_permission","created":1745013322,"allow_create_engine":false,"allow_sampling":true,"allow_logprobs":true,"allow_search_indices":false,"allow_view":true,"allow_fine_tuning":false,"organization":"*","group":null,"is_blocking":false}]},{"id":"meta-llama-Llama-3.2-1B-Instruct:fine-tuning-lOdIbZE88Q","object":"model","created":1745013322,"owned_by":"vllm","root":"/models/ft:meta-llama-Llama-3.2-1B-Instruct:fine-tuning-lOdIbZE88Q","parent":"meta-llama-Llama-3.2-1B-Instruct","max_model_len":null,"permission":[{"id":"modelperm-dfa39c019d7f4328ac1ea3cfdbab746f","object":"model_permission","created":1745013322,"allow_create_engine":false,"allow_sampling":true,"allow_logprobs":true,"allow_search_indices":false,"allow_view":true,"allow_fine_tuning":false,"organization":"*","group":null,"is_blocking":false}]}]}
`

	var response ListModelsResponse
	err := json.Unmarshal([]byte(body), &response)
	assert.NoError(t, err)

	assert.Len(t, response.Data, 2)
	assert.Equal(t, "meta-llama-Llama-3.2-1B-Instruct", response.Data[0].ID)
	assert.Nil(t, response.Data[0].Parent)

	assert.Equal(t, "meta-llama-Llama-3.2-1B-Instruct:fine-tuning-lOdIbZE88Q", response.Data[1].ID)
	assert.Equal(t, "meta-llama-Llama-3.2-1B-Instruct", *response.Data[1].Parent)
}
