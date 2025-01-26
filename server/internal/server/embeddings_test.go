package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertInputIfNotString(t *testing.T) {
	tcs := []struct {
		name string
		body string
		want string
	}{
		{
			name: "string input",
			body: `{"input": "The food was delicious."}`,
			want: `{"input": "The food was delicious."}`,
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
			got, err := convertInputIfNotString([]byte(tc.body))
			assert.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}
