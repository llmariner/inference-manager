package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const updaterUpdateInterval = 30 * time.Second

// UpdateInProgressPodGetter gets the names of pods that are currently being updated.
type UpdateInProgressPodGetter interface {
	GetUpdateInProgressPodNames() map[string]struct{}
}

// NewDriftedPodUpdater creates a new DriftedPodUpdater.
func NewDriftedPodUpdater(
	namespace string,
	k8sClient client.Client,
	updateInProgressPodGetter UpdateInProgressPodGetter,
) *DriftedPodUpdater {
	return &DriftedPodUpdater{
		namespace:                 namespace,
		k8sClient:                 k8sClient,
		updateInProgressPodGetter: updateInProgressPodGetter,
		stsesByName:               make(map[string]*statefulSet),
	}
}

// statefulSet is a struct that represents a StatefulSet.
type statefulSet struct {
	name      string
	namespace string

	modelID        string
	replicas       int
	updateRevision string

	podSpec *corev1.PodSpec
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

	updateInProgressPodGetter UpdateInProgressPodGetter

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

		if !hasMajorChangeToPodSpec(&pod.Spec, sts.podSpec) {
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

	updateInProgressPods := u.updateInProgressPodGetter.GetUpdateInProgressPodNames()

	readyPodsByName := map[string]*corev1.Pod{}
	for _, pod := range pods {
		if !isPodReady(&pod) {
			continue
		}

		if _, ok := updateInProgressPods[pod.Name]; ok {
			u.logger.Info("Skipping pod that is being updated", "pod", pod.Name)
			continue
		}

		// TODO(kenji): Check model loading status.
		readyPodsByName[pod.Name] = &pod
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
	log.V(1).Info("Reconciling statefulset...", "name", req.Name)

	var sts appsv1.StatefulSet
	if err := u.k8sClient.Get(ctx, req.NamespacedName, &sts); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		log.V(1).Info("Deleting statefulset...", "name", req.Name)
		u.deleteStatefulSet(req.Name)
		return ctrl.Result{}, nil
	}

	log.V(1).Info("Updating statefulset...", "name", sts.Name)

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
	s.podSpec = &sts.Spec.Template.Spec
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

func hasMajorChangeToPodSpec(currPodSpec, specFromTemplate *corev1.PodSpec) bool {
	curr := currPodSpec.DeepCopy()
	expected := specFromTemplate.DeepCopy()

	// Set the default values.
	if expected.EnableServiceLinks == nil {
		expected.EnableServiceLinks = ptr.To(true)
	}
	if expected.PreemptionPolicy == nil {
		p := corev1.PreemptLowerPriority
		expected.PreemptionPolicy = &p
	}
	if expected.Priority == nil {
		expected.Priority = ptr.To[int32](0)
	}

	// Set the dynamically set fields from the current pod spec.
	expected.Hostname = curr.Hostname
	expected.NodeName = curr.NodeName

	// Set the request same as limits for GPU if not set.
	for _, con := range expected.Containers {
		const gpuResource = "nvidia.com/gpu"
		r := con.Resources
		v, ok := r.Limits[gpuResource]
		if !ok {
			continue
		}

		if _, ok := r.Requests[gpuResource]; !ok {
			r.Requests[gpuResource] = v
		}
	}

	// Remove default tolerations.
	var newTolerations []corev1.Toleration
	for _, t := range curr.Tolerations {
		keys := map[string]bool{
			"node.kubernetes.io/not-ready":   true,
			"node.kubernetes.io/unreachable": true,
		}

		if keys[t.Key] && t.Effect == corev1.TaintEffectNoExecute && t.Operator == corev1.TolerationOpExists {
			continue
		}
		newTolerations = append(newTolerations, t)
	}
	curr.Tolerations = newTolerations

	const kubeAPIAccessPrefix = "kube-api-access-"

	// Remove volumes for the serviceaccount.
	removeVolumeMount := func(vms []corev1.VolumeMount) []corev1.VolumeMount {
		var newVolumeMounts []corev1.VolumeMount
		for _, vm := range vms {
			if strings.HasPrefix(vm.Name, kubeAPIAccessPrefix) {
				continue
			}
			newVolumeMounts = append(newVolumeMounts, vm)
		}
		return newVolumeMounts
	}
	for i := range curr.Containers {
		con := &curr.Containers[i]
		con.VolumeMounts = removeVolumeMount(con.VolumeMounts)
	}
	for i := range curr.InitContainers {
		con := &curr.InitContainers[i]
		con.VolumeMounts = removeVolumeMount(con.VolumeMounts)
	}

	// Ignore the version of inference-manager-engine image.
	ignoreInferenceManagerVersion := func(image string) string {
		parts := strings.Split(image, ":")
		if len(parts) != 2 && !strings.Contains(parts[0], "inference-manager-engine") {
			return image
		}
		return parts[0] + ":<IGNORED>"
	}
	for i := range curr.Containers {
		con := &curr.Containers[i]
		con.Image = ignoreInferenceManagerVersion(con.Image)
	}
	for i := range curr.InitContainers {
		con := &curr.InitContainers[i]
		con.Image = ignoreInferenceManagerVersion(con.Image)
	}
	for i := range expected.Containers {
		con := &expected.Containers[i]
		con.Image = ignoreInferenceManagerVersion(con.Image)
	}
	for i := range expected.InitContainers {
		con := &expected.InitContainers[i]
		con.Image = ignoreInferenceManagerVersion(con.Image)
	}

	var newVolumes []corev1.Volume
	for _, v := range curr.Volumes {
		if strings.HasPrefix(v.Name, kubeAPIAccessPrefix) {
			continue
		}
		newVolumes = append(newVolumes, v)
	}
	curr.Volumes = newVolumes

	return !equality.Semantic.DeepEqual(curr, expected)
}
