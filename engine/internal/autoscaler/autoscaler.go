package autoscaler

import (
	"k8s.io/apimachinery/pkg/types"
)

// Registerer is an interface for registering and unregistering scalers.
type Registerer interface {
	Register(modelID string, target types.NamespacedName)
	Unregister(target types.NamespacedName)
}

// NoopRegisterer is a scaler that does nothing.
type NoopRegisterer struct{}

// Register does nothing.
func (n *NoopRegisterer) Register(modelID string, target types.NamespacedName) {}

// Unregister does nothing.
func (n *NoopRegisterer) Unregister(target types.NamespacedName) {}
