package config

import (
	"reflect"
	"sync"

	"github.com/llmariner/inference-manager/engine/internal/models"
)

// NewProcessedModelConfig returns a new ProcessedModelConfig.
func NewProcessedModelConfig(c *Config) *ProcessedModelConfig {
	return &ProcessedModelConfig{
		model:               &c.Model,
		runtime:             &c.Runtime,
		preloadedModelIDs:   c.PreloadedModelIDs,
		modelContextLengths: c.ModelContextLengths,
		items:               map[string]ModelConfigItem{},
	}
}

// ProcessedModelConfig is the processed model configuration.
type ProcessedModelConfig struct {
	model               *ModelConfig
	runtime             *RuntimeConfig
	preloadedModelIDs   []string
	modelContextLengths map[string]int

	items map[string]ModelConfigItem
	// mu protects items.
	mu sync.Mutex
}

// ModelConfigItem returns the model configuration item for the given model ID.
func (c *ProcessedModelConfig) ModelConfigItem(modelID string) ModelConfigItem {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If this is a fine-tuned model, use the runtime that the base model uses.
	// TODO(kenji): Have a better way to determine if the model is a base model or not.
	if baseModelID, err := models.ExtractBaseModel(modelID); err == nil {
		modelID = baseModelID
	}

	modelID = formatModelID(modelID)
	if item, ok := c.items[modelID]; ok {
		return item
	}

	item := c.model.Default

	// Override the default values if there is a matching model.
	var override ModelConfigItem
	var found bool
	for id, i := range c.model.Overrides {
		if formatModelID(id) == modelID {
			override = i
			found = true
			break
		}
	}
	if found {
		// Override the default values if set.
		if n := override.RuntimeName; n != "" {
			item.RuntimeName = n
		}
		if r := override.Resources; !reflect.DeepEqual(r, Resources{}) {
			item.Resources = r
		}
		if r := override.Replicas; r > 0 {
			item.Replicas = r
		}
		if override.Preloaded {
			item.Preloaded = true
		}
		if l := override.ContextLength; l > 0 {
			item.ContextLength = l
		}
		if fs := override.VLLMExtraFlags; len(fs) > 0 {
			item.VLLMExtraFlags = fs
		}
	}

	for _, id := range c.preloadedModelIDs {
		if formatModelID(id) == modelID {
			item.Preloaded = true
			break
		}
	}

	for id, l := range c.modelContextLengths {
		if formatModelID(id) == modelID {
			item.ContextLength = l
			break
		}
	}

	c.items[modelID] = item

	return item
}

// PreloadedModelIDs returns the IDs of the models to be preloaded.
func (c *ProcessedModelConfig) PreloadedModelIDs() []string {
	ids := map[string]struct{}{}
	for id := range c.model.Overrides {
		if mci := c.ModelConfigItem(id); mci.Preloaded {
			ids[formatModelID(id)] = struct{}{}
		}
	}
	for _, id := range c.preloadedModelIDs {
		ids[formatModelID(id)] = struct{}{}

	}

	idsSlice := make([]string, 0, len(ids))
	for id := range ids {
		idsSlice = append(idsSlice, id)
	}
	return idsSlice
}
