package api

import (
	"encoding/json"
	"testing"

	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestConvertInput(t *testing.T) {
	tcs := []struct {
		name string
		body string
		want string
	}{
		{
			name: "string input",
			body: `{"input": "The food was delicious."}`,
			want: `{"input":"The food was delicious."}`,
		},
		{
			name: "string input",
			body: `{"input": ["a", "b"]}`,
			want: `{"encoded_input":"WyJhIiwiYiJd"}`,
		},
		{
			name: "string input",
			body: `{"input": [1, 2]}`,
			want: `{"encoded_input":"WzEsMl0="}`,
		},
		{
			name: "string input",
			body: `{"input": [[1], [2]]}`,
			want: `{"encoded_input":"W1sxXSxbMl1d"}`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyConvertFuncs([]byte(tc.body), []convertF{convertInput})
			assert.NoError(t, err)
			assert.Equal(t, tc.want, string(got))

			var req v1.CreateEmbeddingRequest
			err = json.Unmarshal(got, &req)
			assert.NoError(t, err)
			assert.True(t, req.Input != "" || req.EncodedInput != "")
		})
	}
}

func TestConvertEncodedInput(t *testing.T) {
	tcs := []struct {
		name string
		body string
		want string
	}{
		{
			name: "string input",
			body: `{"input": "The food was delicious."}`,
			want: `{"input":"The food was delicious."}`,
		},
		{
			name: "string input",
			body: `{"encoded_input": "WyJhIiwiYiJd"}`,
			want: `{"input":["a","b"]}`,
		},
		{
			name: "string input",
			body: `{"encoded_input": "WzEsMl0="}`,
			want: `{"input":[1,2]}`,
		},
		{
			name: "string input",
			body: `{"encoded_input": "W1sxXSxbMl1d"}`,
			want: `{"input":[[1],[2]]}`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyConvertFuncs([]byte(tc.body), []convertF{convertEncodedInput})
			assert.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}
