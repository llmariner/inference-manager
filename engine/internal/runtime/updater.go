package runtime

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/llmariner/rbac-manager/pkg/auth"
	appsv1 "k8s.io/api/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewUpdater creates a new Updater.
func NewUpdater(namespace string, rtClientFactory ClientFactory) *Updater {
	return &Updater{
		namespace:       namespace,
		rtClientFactory: rtClientFactory,
	}
}

// Updater updates runtimes at startup.
type Updater struct {
	namespace       string
	rtClientFactory ClientFactory

	k8sClient client.Client
	logger    logr.Logger
}

// SetupWithManager sets up the updater with the manager.
func (u *Updater) SetupWithManager(mgr ctrl.Manager) error {
	u.k8sClient = mgr.GetClient()
	u.logger = mgr.GetLogger().WithName("updater")
	return mgr.Add(u)
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (u *Updater) NeedLeaderElection() bool {
	return true
}

// Start starts the updater.
func (u *Updater) Start(ctx context.Context) error {
	ctx = ctrl.LoggerInto(ctx, u.logger)
	ctx = auth.AppendWorkerAuthorization(ctx)
	u.logger.Info("Starting updater")

	var stsList appsv1.StatefulSetList
	if err := u.k8sClient.List(ctx, &stsList,
		client.InNamespace(u.namespace),
		client.MatchingLabels{
			"app.kubernetes.io/name":       "runtime",
			"app.kubernetes.io/created-by": managerName,
		}); err != nil {
		return fmt.Errorf("failed to list runtimes: %s", err)
	}

	// TODO: support runtime(ollama, vllm) changes
	for _, sts := range stsList.Items {
		modelID := sts.GetAnnotations()[modelAnnotationKey]
		if modelID == "" {
			u.logger.Error(nil, "No model ID found", "sts", sts.Name)
			continue
		}
		client, err := u.rtClientFactory.New(modelID)
		if err != nil {
			return fmt.Errorf("failed to create runtime client: %s", err)
		}
		_, err = client.DeployRuntime(ctx, modelID, true)
		if err != nil {
			return fmt.Errorf("failed to update runtime: %s", err)
		}
		u.logger.V(1).Info("Updated runtime", "model", modelID)
	}

	u.logger.Info("Updater finished")
	return nil
}
