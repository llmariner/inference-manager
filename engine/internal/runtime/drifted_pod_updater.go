package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
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

const updaterUpdateInterval = 30 * time.Second

// NewDriftedPodUpdater creates a new DriftedPodUpdater.
func NewDriftedPodUpdater(namespace string, k8sClient client.Client) *DriftedPodUpdater {
	return &DriftedPodUpdater{
		namespace:   namespace,
		k8sClient:   k8sClient,
		stsesByName: make(map[string]*statefulSet),
	}
}

// statefulSet is a struct that represents a StatefulSet.
type statefulSet struct {
	name      string
	namespace string

	modelID        string
	replicas       int
	updateRevision string
}

func (s *statefulSet) clone() *statefulSet {
	return &statefulSet{
		name:           s.name,
		namespace:      s.namespace,
		modelID:        s.modelID,
		replicas:       s.replicas,
		updateRevision: s.updateRevision,
	}
}

// DriftedPodUpdater updates runtimes at startup.
type DriftedPodUpdater struct {
	namespace string

	k8sClient client.Client

	logger logr.Logger

	mu          sync.Mutex
	stsesByName map[string]*statefulSet
}

// SetupWithManager sets up the updater with the manager.
func (u *DriftedPodUpdater) SetupWithManager(mgr ctrl.Manager) error {
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
		Named("runtime-updater").
		Watches(&corev1.Pod{},
			handler.TypedEnqueueRequestForOwner[client.Object](mgr.GetScheme(), mgr.GetRESTMapper(), &appsv1.StatefulSet{}, handler.OnlyControllerOwner()),
			builder.WithPredicates(filterByLabel)).
		WithLogConstructor(ctor).
		Complete(u)
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (u *DriftedPodUpdater) NeedLeaderElection() bool {
	return true
}

// Run runs the updater.
func (u *DriftedPodUpdater) Run(ctx context.Context) error {
	u.logger.Info("Starting drifted pod updater")

	ticker := time.NewTicker(updaterUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stses := u.listStatefulSets()
			for _, sts := range stses {
				if err := u.deleteDriftedPods(ctx, sts); err != nil {
					return err
				}
			}
		}
	}
}

func (u *DriftedPodUpdater) deleteDriftedPods(ctx context.Context, sts *statefulSet) error {
	pods, err := listPods(ctx, u.k8sClient, sts.namespace, sts.name)
	if err != nil {
		return err
	}

	var driftedPods []*corev1.Pod
	for _, pod := range pods {
		hash := pod.Labels[appsv1.StatefulSetRevisionLabel]
		if hash == sts.updateRevision {
			continue
		}
		driftedPods = append(driftedPods, &pod)
	}

	if len(driftedPods) == 0 {
		u.logger.Info("No drifted pods found", "statefulset", sts.name)
		return nil
	}

	u.logger.Info("Found drifted pods", "statefulset", sts.name, "driftedPods", len(driftedPods))

	// If there is only replica, simply delete the drifted pod.
	if sts.replicas == 1 {
		u.logger.Info("Only one replica, deleting drifted pods", "statefulset", sts.name)
		for _, pod := range driftedPods {
			if err := u.deleteDriftedPod(ctx, pod); err != nil {
				return err
			}
		}
		return nil
	}

	// We want to make sure that when we delete a drifted pod, other pods are ready and all models
	// (both base models and fine-tuned models) are loaded.
	//
	// - If all pods are ready, we can delete any drifted pod.
	// - If all pods except one are ready, check if the unready pod is the drifted one. If so, we can delete it.

	readyPodsByName := map[string]*corev1.Pod{}
	for _, pod := range pods {
		if isPodReady(&pod) {
			// TODO(kenji): Check model loading status.
			readyPodsByName[pod.Name] = &pod
		}
	}

	u.logger.Info("Ready pods found", "statefulset", sts.name, "readyPods", len(readyPodsByName), "replicas", sts.replicas)

	if len(readyPodsByName) == sts.replicas {
		u.logger.Info("All pods are ready, deleting drifted pods", "statefulset", sts.name)
		var driftedPod *corev1.Pod
		for _, p := range driftedPods {
			driftedPod = p
			break
		}
		if err := u.deleteDriftedPod(ctx, driftedPod); err != nil {
			return err
		}
		return nil
	}

	if len(readyPodsByName) == sts.replicas-1 {
		// Find the drifted pod that is not ready.
		var driftedPod *corev1.Pod
		for _, p := range driftedPods {
			if _, ok := readyPodsByName[p.Name]; !ok {
				driftedPod = p
				break
			}
		}
		if driftedPod == nil {
			u.logger.Info("Do not delete a drifted pod as it will create one more unready pod", "statefulset", sts.name)
			return nil
		}

		u.logger.Info("Found drifted pod that is not ready, deleting it", "statefulset", sts.name, "pod", driftedPod.Name)
		if err := u.deleteDriftedPod(ctx, driftedPod); err != nil {
			return err
		}
	}

	u.logger.Info("Drifted pods found but not deleted", "statefulset", sts.name, "pods", len(driftedPods))

	// TODO(kenji): check if activators & preloader complete the first run

	return nil
}

func (u *DriftedPodUpdater) deleteDriftedPod(ctx context.Context, pod *corev1.Pod) error {
	u.logger.Info("Deleting drifted pod", "pod", pod.Name)
	if err := u.k8sClient.Delete(ctx, pod); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete drifted pod %s: %s", pod.Name, err)
		}
	}
	return nil
}

// Reconcile reconciles the runtime.
func (u *DriftedPodUpdater) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling statefulset...", "name", req.Name)

	var sts appsv1.StatefulSet
	if err := u.k8sClient.Get(ctx, req.NamespacedName, &sts); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		log.Info("Deleting statefulset...", "name", req.NamespacedName.Name)
		u.deleteStatefulSet(req.Name)
		return ctrl.Result{}, nil
	}

	log.Info("Updating statefulset...", "name", sts.Name)

	u.createOrUpdateStatefulSet(&sts)

	return ctrl.Result{}, nil
}

func (u *DriftedPodUpdater) createOrUpdateStatefulSet(sts *appsv1.StatefulSet) {
	u.mu.Lock()
	defer u.mu.Unlock()

	s, ok := u.stsesByName[sts.Name]
	if !ok {
		s = &statefulSet{
			name:      sts.Name,
			namespace: sts.Namespace,
			modelID:   sts.GetAnnotations()[modelAnnotationKey],
		}
		u.stsesByName[sts.Name] = s
	}

	s.replicas = int(*sts.Spec.Replicas)
	s.updateRevision = sts.Status.UpdateRevision
}

func (u *DriftedPodUpdater) deleteStatefulSet(name string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	delete(u.stsesByName, name)
}

func (u *DriftedPodUpdater) listStatefulSets() []*statefulSet {
	u.mu.Lock()
	defer u.mu.Unlock()

	var stses []*statefulSet
	for _, s := range u.stsesByName {
		stses = append(stses, s.clone())
	}
	return stses
}
