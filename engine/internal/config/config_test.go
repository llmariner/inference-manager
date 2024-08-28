package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormattedModelContextLengths(t *testing.T) {
	c := Config{
		ModelContextLengths: map[string]int{
			"google/gemma-2b-it-q4": 10,
		},
	}
	got := c.FormattedModelContextLengths()
	want := map[string]int{
		"google-gemma-2b-it-q4": 10,
	}
	assert.True(t, reflect.DeepEqual(got, want), "got: %v, want: %v", got, want)
}

func TestFormattedPreloadedModelIDs(t *testing.T) {
	c := Config{
		PreloadedModelIDs: []string{
			"google/gemma-2b-it-q4",
		},
	}
	got := c.FormattedPreloadedModelIDs()
	want := []string{
		"google-gemma-2b-it-q4",
	}
	assert.ElementsMatch(t, got, want)
}
