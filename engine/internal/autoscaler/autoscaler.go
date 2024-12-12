package autoscaler

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Autoscaler is an interface for autoscalers.
type Autoscaler interface {
	Registerer

	manager.Runnable
	manager.LeaderElectionRunnable
	SetupWithManager(mgr ctrl.Manager) error
}

// Registerer is an interface for registering and unregistering scalers.
type Registerer interface {
	Register(ctx context.Context, modelID string, target *appsv1.StatefulSet) error
	Unregister(target types.NamespacedName)
}

// NoopRegisterer is a scaler that does nothing.
type NoopRegisterer struct{}

// Register does nothing.
func (n *NoopRegisterer) Register(ctx context.Context, modelID string, target *appsv1.StatefulSet) error {
	return nil
}

// Unregister does nothing.
func (n *NoopRegisterer) Unregister(target types.NamespacedName) {}
