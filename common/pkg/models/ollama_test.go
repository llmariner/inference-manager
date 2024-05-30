package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOllamaModelName(t *testing.T) {
	tcs := []struct {
		modelID string
		want    string
	}{
		{
			modelID: "ft:gemma:2b-custom-model-name-7p4lURel",
			want:    "gemma:2b-custom-model-name-7p4lURel",
		},
		{
			modelID: "google-gemma-2b",
			want:    "google-gemma-2b",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			got := OllamaModelName(tc.modelID)
			assert.Equal(t, tc.want, got)
		})
	}
}
