package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	ctrl "sigs.k8s.io/controller-runtime"
)

const modelListInterval = 10 * time.Second

// ModelManager is an interface for managing models.
type ModelManager interface {
	PullModel(ctx context.Context, modelID string) error
	DeleteModel(ctx context.Context, modelID string) error
}

type modelLister interface {
	ListModels(ctx context.Context, in *mv1.ListModelsRequest, opts ...grpc.CallOption) (*mv1.ListModelsResponse, error)
}

// NewModelActivator creates a new ModelActivator.
func NewModelActivator(preloadedModelIDs []string, mmanager ModelManager, modelLister modelLister) *ModelActivator {
	m := map[string]bool{}
	for _, id := range preloadedModelIDs {
		m[id] = true
	}

	return &ModelActivator{
		preloadedModelIDs: m,
		mmanager:          mmanager,
		modelLister:       modelLister,

		parallelism: 3,
	}
}

// ModelActivator preloads models.
type ModelActivator struct {
	preloadedModelIDs map[string]bool

	mmanager ModelManager

	modelLister modelLister

	parallelism int

	logger logr.Logger
}

// SetupWithManager sets up the multi-autoscaler with the Manager.
func (a *ModelActivator) SetupWithManager(mgr ctrl.Manager) error {
	a.logger = mgr.GetLogger().WithName("activator")
	return mgr.Add(a)
}

// Start starts the multi-autoscaler.
func (a *ModelActivator) Start(ctx context.Context) error {
	ctx = ctrl.LoggerInto(ctx, a.logger)

	a.logger.Info("Starting model activator")

	for {
		if err := a.reconcileModelActivation(ctx); err != nil {
			return fmt.Errorf("reconcile model activation: %s", err)
		}
		select {
		case <-time.After(modelListInterval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (a *ModelActivator) reconcileModelActivation(ctx context.Context) error {
	ctx = auth.AppendWorkerAuthorization(ctx)

	resp, err := a.modelLister.ListModels(ctx, &mv1.ListModelsRequest{})
	if err != nil {
		return fmt.Errorf("list models: %s", err)
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(a.parallelism)
	for _, model := range resp.Data {
		mid := model.Id
		switch model.ActivationStatus {
		case mv1.ActivationStatus_ACTIVATION_STATUS_UNSPECIFIED:
			// Do nothing for backward compatibility.
		case mv1.ActivationStatus_ACTIVATION_STATUS_ACTIVE:
			g.Go(func() error {
				if err := a.mmanager.PullModel(ctx, mid); err != nil {
					// Ignore ErrRequestCanceled as it returns when a pod is unschedulable. Returning
					// an error from here will make the preloading fails, but an unschedulable pod is
					// expected when a cluster is being autoscaled.
					if errors.Is(err, ErrRequestCanceled) {
						a.logger.Error(err, "pull model canceled", "modelID", mid)
					} else {
						return fmt.Errorf("pull model %s: %s", mid, err)
					}
				}
				return nil
			})
		case mv1.ActivationStatus_ACTIVATION_STATUS_INACTIVE:
			if a.preloadedModelIDs[mid] {
				// Do not inactivate the preloaded models.
				continue
			}

			g.Go(func() error {
				if err := a.mmanager.DeleteModel(ctx, mid); err != nil {
					return fmt.Errorf("delete model %s: %s", mid, err)
				}
				return nil
			})
		default:
			return fmt.Errorf("unknown activation state: %s", model.ActivationStatus)
		}
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("preloading: %s", err)
	}

	a.logger.Info("Model activation reconciled", "modelCount", len(resp.Data))
	return nil
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (a *ModelActivator) NeedLeaderElection() bool {
	return true
}
