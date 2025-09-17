package api

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestConvertToolChoice(t *testing.T) {
	tcs := []struct {
		name string
		body string
		want string
	}{
		{
			name: "string tool choice",
			body: `{"tool_choice": "auto"}`,
			want: `{"tool_choice":"auto"}`,
		},
		{
			name: "object tool choice",
			body: `{"tool_choice": {"type": "function", "function": {"name": "test"}}}`,
			want: `{"tool_choice_object":{"function":{"name":"test"},"type":"function"}}`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyConvertFuncs([]byte(tc.body), []convertF{convertToolChoice})
			assert.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestConvertToolChoiceObject(t *testing.T) {
	tcs := []struct {
		name string
		body string
		want string
	}{
		{
			name: "string tool choice",
			body: `{"tool_choice": "auto"}`,
			want: `{"tool_choice":"auto"}`,
		},
		{
			name: "object tool choice",
			body: `{"tool_choice_object":{"function":{"name":"test"},"type":"function"}}`,
			want: `{"tool_choice":{"function":{"name":"test"},"type":"function"}}`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyConvertFuncs([]byte(tc.body), []convertF{convertToolChoiceObject})
			assert.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestConvertFunctionParameters(t *testing.T) {
	reqBody := `
{
"tools": [{
  "type": "function",
  "function": {
     "name": "get_weather",
     "description": "Get current temperature for a given location.",
     "parameters": {
        "type": "object",
        "properties": {
          "location": {
            "type": "string",
            "description": "City and country"
          }
        }
      },
      "strict": true
    }
}]}`
	got, err := applyConvertFuncs([]byte(reqBody), []convertF{convertFunctionParameters})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal(got, &r)
	assert.NoError(t, err)
	tools, ok := r["tools"]
	assert.True(t, ok)
	assert.Len(t, tools.([]interface{}), 1)
	tool := tools.([]interface{})[0].(map[string]interface{})
	f, ok := tool["function"]
	assert.True(t, ok)
	fn := f.(map[string]interface{})

	_, ok = fn["parameters"]
	assert.False(t, ok)

	p, ok := fn["encoded_parameters"]
	assert.True(t, ok)

	b, ok := p.(string)
	assert.True(t, ok)
	bb, err := base64.URLEncoding.DecodeString(b)
	assert.NoError(t, err)

	gotR := map[string]interface{}{}
	err = json.Unmarshal(bb, &gotR)
	assert.NoError(t, err)
	wantR := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "City and country",
			},
		},
	}
	assert.Equal(t, wantR, gotR)

	var createReq v1.CreateChatCompletionRequest
	err = json.Unmarshal(got, &createReq)
	assert.NoError(t, err)
}

func TestConvertEncodedFunctionParameters(t *testing.T) {
	reqBody := `
{
"tools": [{
  "type": "function",
  "function": {
     "name": "get_weather",
     "description": "Get current temperature for a given location.",
     "encoded_parameters": "eyJwcm9wZXJ0aWVzIjp7ImxvY2F0aW9uIjp7ImRlc2NyaXB0aW9uIjoiQ2l0eSBhbmQgY291bnRyeSIsInR5cGUiOiJzdHJpbmcifX0sInJlcXVpcmVkIjpbImxvY2F0aW9uIl0sInR5cGUiOiJvYmplY3QifQ==",
      "strict": true
    }
}]}`
	got, err := applyConvertFuncs([]byte(reqBody), []convertF{convertEncodedFunctionParameters})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal(got, &r)
	assert.NoError(t, err)
	tools, ok := r["tools"]
	assert.True(t, ok)
	assert.Len(t, tools.([]interface{}), 1)
	tool := tools.([]interface{})[0].(map[string]interface{})
	f, ok := tool["function"]
	assert.True(t, ok)
	fn := f.(map[string]interface{})

	_, ok = fn["encoded_parameters"]
	assert.False(t, ok)

	p, ok := fn["parameters"]
	assert.True(t, ok)

	gotR := p.(map[string]interface{})
	wantR := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "City and country",
			},
		},
		"required": []interface{}{"location"},
	}
	assert.Equal(t, wantR, gotR)
}

func TestConvertChatTemplateKewargs(t *testing.T) {
	body := `{"chat_template_kwargs":{"enable_thinking":true}}`

	got, err := applyConvertFuncs([]byte(body), []convertF{convertChatTemplateKwargs})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok := r[chatTemplateKwargsKey]
	assert.False(t, ok)

	_, ok = r[encodedChatTemplateKwargsKey]
	assert.True(t, ok)

	got, err = applyConvertFuncs(got, []convertF{convertEncodedChatTemplateKwargs})
	assert.NoError(t, err)

	assert.Equal(t, body, string(got))

	r = map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok = r[chatTemplateKwargsKey]
	assert.True(t, ok)

	_, ok = r[encodedChatTemplateKwargsKey]
	assert.False(t, ok)
}

func TestConvertTemperature_Unset(t *testing.T) {
	body := `{"top_p":1}`

	got, err := applyConvertFuncs([]byte(body), []convertF{convertTemperature})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok := r[temperatureKey]
	assert.False(t, ok)

	_, ok = r[isTemperatureSetKey]
	assert.False(t, ok)

	got, err = applyConvertFuncs(got, []convertF{convertEncodedTemperature})
	assert.NoError(t, err)

	assert.Equal(t, body, string(got))

	r = map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok = r[temperatureKey]
	assert.False(t, ok)

	_, ok = r[isTemperatureSetKey]
	assert.False(t, ok)
}

func TestConvertTemperature_NonZero(t *testing.T) {
	body := `{"temperature":0.5}`

	got, err := applyConvertFuncs([]byte(body), []convertF{convertTemperature})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok := r[temperatureKey]
	assert.True(t, ok)

	_, ok = r[isTemperatureSetKey]
	assert.True(t, ok)

	got, err = applyConvertFuncs(got, []convertF{convertEncodedTemperature})
	assert.NoError(t, err)

	assert.Equal(t, body, string(got))

	r = map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok = r[temperatureKey]
	assert.True(t, ok)

	_, ok = r[isTemperatureSetKey]
	assert.False(t, ok)
}

func TestConvertTemperature_Zero(t *testing.T) {
	body := `{"temperature":0}`

	got, err := applyConvertFuncs([]byte(body), []convertF{convertTemperature})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok := r[temperatureKey]
	assert.True(t, ok)

	_, ok = r[isTemperatureSetKey]
	assert.True(t, ok)

	got, err = applyConvertFuncs(got, []convertF{convertEncodedTemperature})
	assert.NoError(t, err)

	assert.Equal(t, body, string(got))

	r = map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok = r[temperatureKey]
	assert.True(t, ok)

	_, ok = r[isTemperatureSetKey]
	assert.False(t, ok)
}

func TestConvertTopP_Unset(t *testing.T) {
	body := `{"temperature":1}`

	got, err := applyConvertFuncs([]byte(body), []convertF{convertTopP})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok := r[topPKey]
	assert.False(t, ok)

	_, ok = r[isTopPSetKey]
	assert.False(t, ok)

	got, err = applyConvertFuncs(got, []convertF{convertEncodedTopP})
	assert.NoError(t, err)

	assert.Equal(t, body, string(got))

	r = map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok = r[topPKey]
	assert.False(t, ok)

	_, ok = r[isTopPSetKey]
	assert.False(t, ok)
}

func TestConvertTopP_NonZero(t *testing.T) {
	body := `{"top_p":0.5}`

	got, err := applyConvertFuncs([]byte(body), []convertF{convertTopP})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok := r[topPKey]
	assert.True(t, ok)

	_, ok = r[isTopPSetKey]
	assert.True(t, ok)

	got, err = applyConvertFuncs(got, []convertF{convertEncodedTopP})
	assert.NoError(t, err)

	assert.Equal(t, body, string(got))

	r = map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok = r[topPKey]
	assert.True(t, ok)

	_, ok = r[isTopPSetKey]
	assert.False(t, ok)
}

func TestConvertTopP_Zero(t *testing.T) {
	body := `{"top_p":0}`

	got, err := applyConvertFuncs([]byte(body), []convertF{convertTopP})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok := r[topPKey]
	assert.True(t, ok)

	_, ok = r[isTopPSetKey]
	assert.True(t, ok)

	got, err = applyConvertFuncs(got, []convertF{convertEncodedTopP})
	assert.NoError(t, err)

	assert.Equal(t, body, string(got))

	r = map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	_, ok = r[topPKey]
	assert.True(t, ok)

	_, ok = r[isTopPSetKey]
	assert.False(t, ok)
}

func TestConvertResponseFormat(t *testing.T) {
	body := `
{
  "response_format": {
    "type": "json_schema",
    "json_schema": {
      "schema": {"key": "value"}
    }
  }
}`
	got, err := applyConvertFuncs([]byte(body), []convertF{convertResponseFormat})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	rf, ok := r["response_format"]
	assert.True(t, ok)
	rfMap, ok := rf.(map[string]interface{})
	assert.True(t, ok)

	_, ok = rfMap["type"]
	assert.True(t, ok)

	js, ok := rfMap["json_schema"]
	assert.True(t, ok)

	jsMap, ok := js.(map[string]interface{})
	assert.True(t, ok)

	_, ok = jsMap[encodedSchemaKey]
	assert.True(t, ok)
	_, ok = jsMap[schemaKey]
	assert.False(t, ok)

	got, err = applyConvertFuncs(got, []convertF{convertEncodedResponseFormat})
	assert.NoError(t, err)

	r = map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	rf, ok = r["response_format"]
	assert.True(t, ok)
	rfMap, ok = rf.(map[string]interface{})
	assert.True(t, ok)

	_, ok = rfMap["type"]
	assert.True(t, ok)

	js, ok = rfMap["json_schema"]
	assert.True(t, ok)

	jsMap, ok = js.(map[string]interface{})
	assert.True(t, ok)

	_, ok = jsMap[encodedSchemaKey]
	assert.False(t, ok)

	_, ok = jsMap[schemaKey]
	assert.True(t, ok)
}

func TestConvertResponseFormat_Text(t *testing.T) {
	body := `{"response_format": {"type": "text"}}`
	got, err := applyConvertFuncs([]byte(body), []convertF{convertResponseFormat})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal([]byte(got), &r)
	assert.NoError(t, err)

	rf, ok := r["response_format"]
	assert.True(t, ok)
	rfMap, ok := rf.(map[string]interface{})
	assert.True(t, ok)

	_, ok = rfMap["type"]
	assert.True(t, ok)
	_, ok = rfMap[encodedSchemaKey]
	assert.False(t, ok)
}

func TestConvertContentStringToArray(t *testing.T) {
	tcs := []struct {
		name string
		body string
		want *v1.CreateChatCompletionRequest
	}{

		{
			name: "no conversion",
			body: `
{
	"messages": [
		{
			"role": "user",
			"content": [
				{
					 "type": "text",
					 "text": "Process audio data."
				},
				{
					 "type": "input_audio",
					 "input_audio": {
						 "data": "audiodata",
						 "format": "wav"
					 }
				}
			]
		}
	]
}`,
			want: &v1.CreateChatCompletionRequest{
				Messages: []*v1.ChatCompletionMessage{
					{
						Role: "user",
						Content: []*v1.ChatCompletionMessage_Content{
							{
								Type: "text",
								Text: "Process audio data.",
							},
							{
								Type: "input_audio",
								InputAudio: &v1.ChatCompletionMessage_Content_InputAudio{
									Data:   "audiodata",
									Format: "wav",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "conversion",
			body: `
{
	"messages": [
		{
			"role": "system",
			"content": "You are a helpful assistant."
		}
	]
}`,
			want: &v1.CreateChatCompletionRequest{
				Messages: []*v1.ChatCompletionMessage{
					{
						Role: "system",
						Content: []*v1.ChatCompletionMessage_Content{
							{
								Type: "text",
								Text: "You are a helpful assistant.",
							},
						},
					},
				},
			},
		},
		{
			name: "mix",
			body: `
			{
				"messages": [
					{
						"role": "system",
						"content": "You are a helpful assistant."
					},
					{
						"role": "user",
						"content": [
				{
					 "type": "text",
					 "text": "Process audio data."
				},
				{
					 "type": "input_audio",
					 "input_audio": {
						 "data": "audiodata",
						 "format": "wav"
					 }
				}
						]
					}
				]
			}`,
			want: &v1.CreateChatCompletionRequest{
				Messages: []*v1.ChatCompletionMessage{
					{
						Role: "system",
						Content: []*v1.ChatCompletionMessage_Content{
							{
								Type: "text",
								Text: "You are a helpful assistant.",
							},
						},
					},
					{
						Role: "user",
						Content: []*v1.ChatCompletionMessage_Content{
							{
								Type: "text",
								Text: "Process audio data.",
							},
							{
								Type: "input_audio",
								InputAudio: &v1.ChatCompletionMessage_Content_InputAudio{
									Data:   "audiodata",
									Format: "wav",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyConvertFuncs([]byte(tc.body), []convertF{convertContentStringToArray})
			assert.NoError(t, err)

			var req v1.CreateChatCompletionRequest
			err = json.Unmarshal(got, &req)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, &req)
		})
	}
}

func TestConvertContentArrayToString(t *testing.T) {
	tcs := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "no messages field",
			body: `{"model": "gpt-4"}`,
			want: `{"model": "gpt-4"}`,
		},
		{
			name: "content already string",
			body: `{"messages": [{"role": "user", "content": "Hello, world!"}]}`,
			want: `{"messages": [{"role": "user", "content": "Hello, world!"}]}`,
		},
		{
			name: "no content field",
			body: `{"messages": [{"role": "user"}]}`,
			want: `{"messages": [{"role": "user"}]}`,
		},
		{
			name: "single text content in array",
			body: `{"messages": [{"role": "user", "content": [{"type": "text", "text": "Hello, world!"}]}]}`,
			want: `{"messages": [{"role": "user", "content": "Hello, world!"}]}`,
		},
		{
			name: "multiple messages mixed formats",
			body: `{"messages": [
				{"role": "system", "content": "You are a helpful assistant."},
				{"role": "user", "content": [{"type": "text", "text": "What's the weather?"}]}
			]}`,
			want: `{"messages": [
				{"role": "system", "content": "You are a helpful assistant."},
				{"role": "user", "content": "What's the weather?"}
			]}`,
		},
		{
			name:    "non-text content type",
			body:    `{"messages": [{"role": "user", "content": [{"type": "image", "image_url": {"url": "https://example.com/image.jpg"}}]}]}`,
			wantErr: true,
		},
		{
			name:    "multiple content items",
			body:    `{"messages": [{"role": "user", "content": [{"type": "text", "text": "Hello"}, {"type": "text", "text": "world"}]}]}`,
			wantErr: true,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyConvertFuncs([]byte(tc.body), []convertF{convertContentArrayToString})

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			// Compare as parsed JSON to ignore formatting differences
			var gotJSON, wantJSON map[string]interface{}
			err = json.Unmarshal(got, &gotJSON)
			assert.NoError(t, err)

			err = json.Unmarshal([]byte(tc.want), &wantJSON)
			assert.NoError(t, err)

			assert.Equal(t, wantJSON, gotJSON)
		})
	}
}
