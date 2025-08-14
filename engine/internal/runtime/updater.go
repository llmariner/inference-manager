package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/llmariner/rbac-manager/pkg/auth"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NewUpdater creates a new Updater.
func NewUpdater(
	namespace string,
	k8sClient client.Client,
	rtClientFactory ClientFactory,
) *Updater {
	return &Updater{
		namespace: namespace,

		k8sClient:       k8sClient,
		rtClientFactory: rtClientFactory,

		stsesByName: make(map[string]*statefulSet),
	}
}

// statefulSet is a struct that represents a StatefulSet.
type statefulSet struct {
	modelID     string
	driftedPods []*corev1.Pod
}

// Updater updates runtimes at startup.
type Updater struct {
	namespace string

	k8sClient       client.Client
	rtClientFactory ClientFactory

	logger logr.Logger

	mu          sync.Mutex
	stsesByName map[string]*statefulSet
}

// SetupWithManager sets up the updater with the manager.
func (u *Updater) SetupWithManager(mgr ctrl.Manager) error {
	u.logger = mgr.GetLogger().WithName("updater")

	filterByLabel := (predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels()["app.kubernetes.io/created-by"] == managerName
	}))

	ctor := func(r *reconcile.Request) logr.Logger {
		if r != nil {
			return mgr.GetLogger().WithValues("runtime", r.NamespacedName)
		}
		return mgr.GetLogger()
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}, builder.WithPredicates(filterByLabel)).
		Watches(&corev1.Pod{},
			handler.TypedEnqueueRequestForOwner[client.Object](mgr.GetScheme(), mgr.GetRESTMapper(), &appsv1.StatefulSet{}, handler.OnlyControllerOwner()),
			builder.WithPredicates(filterByLabel)).
		WithLogConstructor(ctor).
		Complete(u)
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

	// TODO(kenji): Trigger the update.

	u.logger.Info("Updater finished")
	return nil
}

// Reconcile reconciles the runtime.
func (u *Updater) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling statefulset...", "name", req.Name)

	var sts appsv1.StatefulSet
	if err := u.k8sClient.Get(ctx, req.NamespacedName, &sts); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		log.Info("Deleting statefulset...", "name", req.NamespacedName.Name)
		u.deleletStatefulset(req.Name)
		return ctrl.Result{}, nil
	}

	log.Info("Updating statefulset...", "name", sts.Name)

	modelID := sts.GetAnnotations()[modelAnnotationKey]

	pods, err := listPods(ctx, u.k8sClient, sts.Namespace, sts.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	var driftedPods []*corev1.Pod
	for _, pod := range pods {
		hash := pod.Labels[appsv1.StatefulSetRevisionLabel]
		if hash == sts.Status.UpdateRevision {
			continue
		}

		driftedPods = append(driftedPods, &pod)
	}

	u.createOrUpdateStatefulSet(sts.Name, modelID, driftedPods)

	return ctrl.Result{}, nil
}

func (u *Updater) createOrUpdateStatefulSet(name, modelID string, driftedPods []*corev1.Pod) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if _, ok := u.stsesByName[name]; ok {
		// Model ID shouldn't change.
		u.stsesByName[name].driftedPods = driftedPods
		return
	}

	u.stsesByName[name] = &statefulSet{
		modelID:     modelID,
		driftedPods: driftedPods,
	}
}

func (u *Updater) deleletStatefulset(name string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	delete(u.stsesByName, name)
}
