package models

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
			modelID: "ft:google-gemma-2b:fine-tuning-wpsd9kb5nl",
			want:    "google-gemma-2b",
		},
		{
			modelID: "bogus",
			wantErr: true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			got, err := ExtractBaseModel(tc.modelID)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
