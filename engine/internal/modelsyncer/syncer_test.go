package modelsyncer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractBaseModel(t *testing.T) {
	tcs := []struct {
		modelID string
		want    string
		wantErr bool
	}{
		{
			modelID: "ft:gemma:2b:custom-model-name:7p4lURel",
			want:    "gemma:2b",
		},
		{
			modelID: "bogus",
			wantErr: true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			got, err := extractBaseModel(tc.modelID)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOllamaModelName(t *testing.T) {
	tcs := []struct {
		modelID string
		want    string
		wantErr bool
	}{
		{
			modelID: "ft:gemma:2b-custom-model-name-7p4lURel",
			want:    "gemma:2b-custom-model-name-7p4lURel",
		},
		{
			modelID: "bogus",
			wantErr: true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			got, err := ollamaModelName(tc.modelID)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
