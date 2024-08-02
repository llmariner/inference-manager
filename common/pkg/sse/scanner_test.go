package sse

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplit(t *testing.T) {
	tcs := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single line",
			input: "line0",
			want:  []string{"line0"},
		},
		{
			name: "multiple lines",
			input: `line0

line1

line2

line3`,
			want: []string{"line0", "line1", "line2", "line3"},
		},
		{
			name:  "empty lines",
			input: ``,
			want:  []string{},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {

			scanner := bufio.NewScanner(bytes.NewReader([]byte(tc.input)))
			scanner.Buffer(make([]byte, 4096), 4096)
			scanner.Split(split)
			var got []string
			for scanner.Scan() {
				got = append(got, scanner.Text())
			}
			err := scanner.Err()
			assert.NoError(t, err)

			assert.ElementsMatch(t, tc.want, got)
		})
	}
}
