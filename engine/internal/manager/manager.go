package manager

import (
	mv1 "github.com/llm-operator/model-manager/api/v1"
)

// ModelSpec is the specification for a new model.
type ModelSpec struct {
	From        string
	AdapterPath string
}

// M manages llm service.
type M interface {
	// Run starts llm service.
	Run() error
	// CreateNewModelOfGGUF creates a new model with the given name and spec that uses a GGUF model file.
	CreateNewModelOfGGUF(modelName string, spec *ModelSpec) error
	DownloadAndCreateNewModel(modelName string, resp *mv1.GetBaseModelPathResponse) error
	// WaitForReady waits for the llm service to be ready.
	WaitForReady() error

	UpdateModelTemplateToLatest(modelname string) error

	IsReady() (bool, string)
}
