package models

import (
	"context"
	"fmt"
	"strings"

	mv1 "github.com/llm-operator/model-manager/api/v1"
	"google.golang.org/grpc"
)

type modelClient interface {
	GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error)
}

// systemOwner is model owner representing the system (= base-model).
const systemOwner = "system"

// IsBaseModel checks if the given model is a base model or not.
func IsBaseModel(ctx context.Context, mClient modelClient, modelID string) (bool, error) {
	// TODO(kenji): Currently we call this RPC to check if the model is a base model or not.
	// Consider changing Model Manager gRPC interface to simplify the interaction (e.g.,
	// add an RPC method that returns a model path for both base model and fine-tuning model).
	model, err := mClient.GetModel(ctx, &mv1.GetModelRequest{
		Id: modelID,
	})
	if err != nil {
		return false, err
	}
	return model.OwnedBy == systemOwner, nil
}

// ExtractBaseModel extracts the base model ID from the given model ID.
func ExtractBaseModel(modelID string) (string, error) {
	l := strings.Split(modelID, ":")
	if len(l) <= 2 {
		return "", fmt.Errorf("invalid model ID: %q", modelID)
	}
	return strings.Join(l[1:len(l)-1], ":"), nil
}
