package puller

const (
	modelDir = "/models"
)

// pullModelRequest represents a request to pull a model.
type pullModelRequest struct {
	ModelID string `json:"modelID"`
}

// ModelDir returns the directory where models are stored.
func ModelDir() string {
	return modelDir
}
