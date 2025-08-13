package api

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertToolChoiceTool(t *testing.T) {
	reqBody := `
{
  "tool_choice": {
    "type": "allowed_tools",
    "tools": [
      { "type": "function", "name": "get_weather" },
      { "type": "mcp", "server_label": "deepwiki" }
    ]
  }
}`
	got, err := applyConvertFuncs([]byte(reqBody), []convertF{convertToolChoiceTool})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal(got, &r)
	assert.NoError(t, err)
	toolChoice, ok := r["tool_choice"]
	assert.True(t, ok)
	tc := toolChoice.(map[string]interface{})
	et, ok := tc["encoded_tools"]
	assert.True(t, ok)

	b, ok := et.(string)
	assert.True(t, ok)
	db, err := base64.URLEncoding.DecodeString(b)
	assert.NoError(t, err)

	gotT := []interface{}{}
	err = json.Unmarshal(db, &gotT)
	assert.NoError(t, err)
	wantT := []interface{}{
		map[string]interface{}{
			"type": "function",
			"name": "get_weather",
		},
		map[string]interface{}{
			"type":         "mcp",
			"server_label": "deepwiki",
		},
	}
	assert.Equal(t, wantT, gotT)
}

func TestConvertEncodedToolChoiceTool(t *testing.T) {
	reqBody := `
{
  "tool_choice": {
    "encoded_tools": "W3sibmFtZSI6ImdldF93ZWF0aGVyIiwidHlwZSI6ImZ1bmN0aW9uIn0seyJzZXJ2ZXJfbGFiZWwiOiJkZWVwd2lraSIsInR5cGUiOiJtY3AifV0="
  }
}`
	got, err := applyConvertFuncs([]byte(reqBody), []convertF{convertEncodedToolChoiceTool})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal(got, &r)
	assert.NoError(t, err)
	toolChoice, ok := r["tool_choice"]
	assert.True(t, ok)

	tc := toolChoice.(map[string]interface{})
	ts, ok := tc["tools"]
	assert.True(t, ok)
	gotT := ts.([]interface{})

	wantT := []interface{}{
		map[string]interface{}{
			"type": "function",
			"name": "get_weather",
		},
		map[string]interface{}{
			"type":         "mcp",
			"server_label": "deepwiki",
		},
	}
	assert.Equal(t, wantT, gotT)
}

func TestConvertTextFormatSchema(t *testing.T) {
	reqBody := `
{
  "text": {
    "format": {
       "type": "json_schema",
       "schema": {"key": "value"}
    }
  }
}`
	got, err := applyConvertFuncs([]byte(reqBody), []convertF{convertTextFormatSchema})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal(got, &r)
	assert.NoError(t, err)

	text, ok := r["text"]
	assert.True(t, ok)
	tt := text.(map[string]interface{})

	format, ok := tt["format"]
	assert.True(t, ok)
	f := format.(map[string]interface{})

	es, ok := f["encoded_schema"]
	assert.True(t, ok)

	b, ok := es.(string)
	assert.True(t, ok)
	db, err := base64.URLEncoding.DecodeString(b)
	assert.NoError(t, err)

	gotS := map[string]interface{}{}
	err = json.Unmarshal(db, &gotS)
	assert.NoError(t, err)
	wantS := map[string]interface{}{
		"key": "value",
	}
	assert.Equal(t, wantS, gotS)
}

func TestConvertEncodedTextFormatSchema(t *testing.T) {
	reqBody := `
{
  "text": {
    "format": {
       "type": "json_schema",
       "encoded_schema": "eyJrZXkiOiJ2YWx1ZSJ9"
    }
  }
}`
	got, err := applyConvertFuncs([]byte(reqBody), []convertF{convertEncodedTextFormatSchema})
	assert.NoError(t, err)

	r := map[string]interface{}{}
	err = json.Unmarshal(got, &r)
	assert.NoError(t, err)

	text, ok := r["text"]
	assert.True(t, ok)
	tt := text.(map[string]interface{})

	format, ok := tt["format"]
	assert.True(t, ok)
	f := format.(map[string]interface{})

	s, ok := f["schema"]
	assert.True(t, ok)

	gotS := s.(map[string]interface{})
	wantS := map[string]interface{}{
		"key": "value",
	}
	assert.Equal(t, wantS, gotS)
}
