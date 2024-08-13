package manager

import "context"

// ModelSpec is the specification for a new model.
type ModelSpec struct {
	From        string
	AdapterPath string
}

// M manages llm service.
type M interface {
	// Run starts llm service.
	Run() error
	// CreateNewModel creates a new model with the given name and spec.
	CreateNewModel(modelName string, spec *ModelSpec) error
	// WaitForReady waits for the llm service to be ready.
	WaitForReady() error
	// DeleteModel deletes the model.
	DeleteModel(ctx context.Context, modelName string) error

	UpdateModelTemplateToLatest(modelname string) error

	IsReady() (bool, string)
}
