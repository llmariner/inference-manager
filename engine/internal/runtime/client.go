package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/llmariner/inference-manager/engine/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	managerName = "inference-engine"

	runtimeAnnotationKey = "llmariner/runtime"
	modelAnnotationKey   = "llmariner/model"

	finalizerKey = "llmariner/runtime-finalizer"

	modelDir = "/models"
)

// ModelDir returns the directory where models are stored.
func ModelDir() string {
	return modelDir
}

// Client is the interface for managing runtimes.
type Client interface {
	GetAddress(name string) string
	DeployRuntime(ctx context.Context, modelID string, update bool) (*appsv1.StatefulSet, error)
}

// ClientFactory is the interface for creating a new Client given a model ID.
type ClientFactory interface {
	New(modelID string) (Client, error)
}

type commonClient struct {
	k8sClient client.Client

	namespace string

	servingPort int

	rconfig *config.RuntimeConfig
	mconfig *config.ProcessedModelConfig
}

func (c *commonClient) getResouces(modelID string) config.Resources {
	return c.mconfig.ModelConfigItem(modelID).Resources
}

func (c *commonClient) applyObject(ctx context.Context, applyConfig any) (client.Object, error) {
	uobj, err := apiruntime.DefaultUnstructuredConverter.ToUnstructured(applyConfig)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{Object: uobj}
	opts := &client.PatchOptions{FieldManager: managerName, Force: ptr.To(true)}
	if err := c.k8sClient.Patch(ctx, obj, client.Apply, opts); err != nil {
		return nil, fmt.Errorf("failed to apply object: %s", err)
	}
	return obj, nil
}

// GetAddress returns the address of the runtime.
func (c *commonClient) GetAddress(name string) string {
	return fmt.Sprintf("%s:%d", name, c.servingPort)
}

type initContainerSpec struct {
	name    string
	image   string
	command []string
	args    []string

	imagePullPolicy corev1.PullPolicy
}

type deployRuntimeParams struct {
	modelID        string
	initEnvs       []*corev1apply.EnvVarApplyConfiguration
	envs           []*corev1apply.EnvVarApplyConfiguration
	volumes        []*corev1apply.VolumeApplyConfiguration
	volumeMounts   []*corev1apply.VolumeMountApplyConfiguration
	readinessProbe *corev1apply.ProbeApplyConfiguration

	args []string

	initContainerSpec *initContainerSpec

	additionalContainers []*corev1apply.ContainerApplyConfiguration
}

// deployRuntime deploys the runtime for the given model.
func (c *commonClient) deployRuntime(
	ctx context.Context,
	params deployRuntimeParams,
	update bool,
) (*appsv1.StatefulSet, error) {
	mci := c.mconfig.ModelConfigItem(params.modelID)
	name := resourceName(mci.RuntimeName, params.modelID)

	log := ctrl.LoggerFrom(ctx).WithValues("name", name)
	log.Info("Deploying runtime", "model", params.modelID, "update", update)

	const (
		tmpDir        = "/tmp"
		subpathModel  = "model"
		subpathTmp    = "tmp"
		shareVolName  = "share-volume"
		configVolName = "config"
	)

	nn := types.NamespacedName{Name: name, Namespace: c.namespace}
	labels := map[string]string{
		"app.kubernetes.io/name":       "runtime",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/created-by": managerName,
	}

	resConf := c.getResouces(params.modelID)

	sharedVolume := corev1apply.Volume().WithName(shareVolName)
	var forcePull bool
	if resConf.Volume != nil {
		sharedVolume.WithPersistentVolumeClaim(
			corev1apply.PersistentVolumeClaimVolumeSource().
				WithClaimName(name))
	} else {
		sharedVolume.WithEmptyDir(corev1apply.EmptyDirVolumeSource())
		// Make every pod pull the model on startup as they don't share the volume.
		forcePull = true
	}
	volumes := []*corev1apply.VolumeApplyConfiguration{
		sharedVolume,
		corev1apply.Volume().
			WithName(configVolName).
			WithConfigMap(corev1apply.ConfigMapVolumeSource().
				WithName(c.rconfig.ConfigMapName)),
	}
	volumes = append(volumes, params.volumes...)

	initVolumeMounts := []*corev1apply.VolumeMountApplyConfiguration{
		corev1apply.VolumeMount().WithName(shareVolName).
			WithMountPath(modelDir).WithSubPath(subpathModel),
		corev1apply.VolumeMount().WithName(shareVolName).
			WithMountPath(tmpDir).WithSubPath(subpathTmp),
		corev1apply.VolumeMount().WithName(configVolName).
			WithMountPath("/etc/config").WithReadOnly(true),
	}
	volumeMounts := []*corev1apply.VolumeMountApplyConfiguration{
		corev1apply.VolumeMount().WithName(shareVolName).
			WithMountPath(modelDir).WithSubPath(subpathModel),
	}
	volumeMounts = append(volumeMounts, params.volumeMounts...)

	initEnvs := append(params.initEnvs,
		corev1apply.EnvVar().WithName("INDEX").
			WithValueFrom(corev1apply.EnvVarSource().
				WithFieldRef(corev1apply.ObjectFieldSelector().
					WithFieldPath("metadata.labels['apps.kubernetes.io/pod-index']"))),
		corev1apply.EnvVar().WithName("LLMO_CLUSTER_REGISTRATION_KEY").
			WithValueFrom(corev1apply.EnvVarSource().
				WithSecretKeyRef(corev1apply.SecretKeySelector().
					WithName(c.rconfig.LLMOWorkerSecretName).
					WithKey(c.rconfig.LLMOKeyEnvKey))),
	)

	if c.rconfig.AWSSecretName != "" {
		initEnvs = append(initEnvs,
			corev1apply.EnvVar().WithName("AWS_ACCESS_KEY_ID").
				WithValueFrom(corev1apply.EnvVarSource().
					WithSecretKeyRef(corev1apply.SecretKeySelector().
						WithName(c.rconfig.AWSSecretName).
						WithKey(c.rconfig.AWSKeyIDEnvKey))),
			corev1apply.EnvVar().WithName("AWS_SECRET_ACCESS_KEY").
				WithValueFrom(corev1apply.EnvVarSource().
					WithSecretKeyRef(corev1apply.SecretKeySelector().
						WithName(c.rconfig.AWSSecretName).
						WithKey(c.rconfig.AWSAccessKeyEnvKey))),
		)
	}

	runtimeResources := corev1apply.ResourceRequirements()
	if len(resConf.Requests) > 0 {
		reqs := make(corev1.ResourceList, len(resConf.Requests))
		for name, v := range resConf.Requests {
			val, err := resource.ParseQuantity(v)
			if err != nil {
				return nil, fmt.Errorf("invalid resource request %s: %s", name, err)
			}
			reqs[corev1.ResourceName(name)] = val
		}
		runtimeResources.WithRequests(reqs)
	}
	if len(resConf.Limits) > 0 {
		limits := make(corev1.ResourceList, len(resConf.Limits))
		for name, v := range resConf.Limits {
			val, err := resource.ParseQuantity(v)
			if err != nil {
				return nil, fmt.Errorf("invalid resource limit %s: %s", name, err)
			}
			limits[corev1.ResourceName(name)] = val
		}
		runtimeResources.WithLimits(limits)
	}

	pullerArgs := []string{
		"pull",
		"--index=$(INDEX)",
		"--runtime=" + mci.RuntimeName,
		"--model-id=" + params.modelID,
		"--config=/etc/config/config.yaml",
		"--force-pull=" + fmt.Sprintf("%t", forcePull),
	}

	image, ok := c.rconfig.RuntimeImages[mci.RuntimeName]
	if !ok {
		return nil, fmt.Errorf("runtime image not found for %s", mci.RuntimeName)
	}

	podSpec := corev1apply.PodSpec().
		WithInitContainers(corev1apply.Container().
			WithName("puller").
			WithImage(c.rconfig.PullerImage).
			WithImagePullPolicy(corev1.PullPolicy(c.rconfig.PullerImagePullPolicy)).
			WithArgs(pullerArgs...).
			WithEnv(initEnvs...).
			WithVolumeMounts(initVolumeMounts...))
	if ic := params.initContainerSpec; ic != nil {
		podSpec = podSpec.WithInitContainers(corev1apply.Container().
			WithName(ic.name).
			WithImage(ic.image).
			WithImagePullPolicy(ic.imagePullPolicy).
			WithCommand(ic.command...).
			WithArgs(ic.args...).
			WithEnv(initEnvs...).
			WithVolumeMounts(initVolumeMounts...))
	}

	containers := []*corev1apply.ContainerApplyConfiguration{
		corev1apply.Container().
			WithName("runtime").
			WithImage(image).
			WithImagePullPolicy(corev1.PullPolicy(c.rconfig.RuntimeImagePullPolicy)).
			WithArgs(params.args...).
			WithPorts(corev1apply.ContainerPort().
				WithName("http").
				WithContainerPort(int32(c.servingPort)).
				WithProtocol(corev1.ProtocolTCP)).
			WithEnv(params.envs...).
			WithVolumeMounts(volumeMounts...).
			WithResources(runtimeResources).
			WithReadinessProbe(params.readinessProbe),
	}
	containers = append(containers, params.additionalContainers...)
	podSpec = podSpec.WithContainers(containers...).
		WithVolumes(volumes...)

	if sa := c.rconfig.ServiceAccountName; sa != "" {
		podSpec = podSpec.WithServiceAccountName(sa)
	}

	if len(c.rconfig.NodeSelector) > 0 {
		podSpec = podSpec.WithNodeSelector(c.rconfig.NodeSelector)
	}
	for _, tc := range c.rconfig.Tolerations {
		t := corev1apply.Toleration()
		if tc.Key != "" {
			t = t.WithKey(tc.Key)
		}
		if tc.Operator != "" {
			t = t.WithOperator(corev1.TolerationOperator(tc.Operator))
		}
		if tc.Value != "" {
			t = t.WithValue(tc.Value)
		}
		if tc.Effect != "" {
			t = t.WithEffect(corev1.TaintEffect(tc.Effect))
		}
		if tc.TolerationSeconds > 0 {
			t = t.WithTolerationSeconds(tc.TolerationSeconds)
		}
		podSpec = podSpec.WithTolerations(t)
	}

	stsConf := appsv1apply.StatefulSet(name, c.namespace).
		WithLabels(labels).
		WithAnnotations(map[string]string{
			runtimeAnnotationKey: c.rconfig.Name,
			modelAnnotationKey:   params.modelID}).
		WithFinalizers(finalizerKey).
		WithSpec(appsv1apply.StatefulSetSpec().
			WithReplicas(int32(mci.Replicas)).
			WithSelector(metav1apply.LabelSelector().
				WithMatchLabels(labels)).
			WithTemplate(corev1apply.PodTemplateSpec().
				WithLabels(labels).
				WithSpec(podSpec)))

	var curSts appsv1.StatefulSet
	if err := c.k8sClient.Get(ctx, nn, &curSts); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
	} else {
		if !update {
			log.V(2).Info("Already exists", "RV", curSts.ResourceVersion, "update", update)
			return &curSts, nil
		}
	}
	sts, err := c.applyObject(ctx, stsConf)
	if err != nil {
		return nil, err
	}
	log.V(4).Info("StatefulSet applied")

	gvk := sts.GetObjectKind().GroupVersionKind()
	ownerRef := metav1apply.OwnerReference().
		WithAPIVersion(gvk.GroupVersion().String()).
		WithKind(gvk.Kind).
		WithName(sts.GetName()).
		WithUID(sts.GetUID()).
		WithBlockOwnerDeletion(true).
		WithController(true)

	objs := []any{
		corev1apply.Service(name, c.namespace).
			WithLabels(labels).
			WithOwnerReferences(ownerRef).
			WithSpec(corev1apply.ServiceSpec().
				WithSelector(labels).
				WithPorts(corev1apply.ServicePort().
					WithName("runtime").
					WithPort(int32(c.servingPort)))),
	}

	if vol := resConf.Volume; vol != nil {
		size, err := resource.ParseQuantity(vol.Size)
		if err != nil {
			return nil, fmt.Errorf("invalid volume size: %s", err)
		}
		objs = append(objs, corev1apply.PersistentVolumeClaim(name, c.namespace).
			WithLabels(labels).
			WithOwnerReferences(ownerRef).
			WithSpec(corev1apply.PersistentVolumeClaimSpec().
				WithStorageClassName(vol.StorageClassName).
				WithAccessModes(corev1.PersistentVolumeAccessMode(vol.AccessMode)).
				WithResources(corev1apply.
					VolumeResourceRequirements().
					WithRequests(corev1.ResourceList{corev1.ResourceStorage: size}))))
	}

	for _, obj := range objs {
		newObj, err := c.applyObject(ctx, obj)
		if err != nil {
			return nil, err
		}
		kind := newObj.GetObjectKind().GroupVersionKind().Kind
		log.V(4).Info(fmt.Sprintf("%s applied", kind))
	}

	uobj, ok := sts.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("object is not of type Unstructured: %T", sts)
	}
	var stsObj appsv1.StatefulSet
	if err := apiruntime.DefaultUnstructuredConverter.FromUnstructured(uobj.Object, &stsObj); err != nil {
		return nil, err
	}
	log.V(2).Info("Deployed runtime")
	return &stsObj, nil
}

func resourceName(runtime, modelID string) string {
	// Avoid using llegal characters like "." or capital letters in the model names
	// TODO(kenji): Have a better way.
	m := strings.ToLower(modelID)
	for _, r := range []string{".", "_", ":"} {
		m = strings.ReplaceAll(m, r, "-")
	}

	// Remove "fine-tuning" from the model name as it does not bring useful information the modelID uniqueness.
	m = strings.ReplaceAll(m, "fine-tuning", "")

	// Trunate the name. A pod created from a statefulset will have the "controller-revision-hash" label,
	// whose value contains the statefulset name and the hash. The value of the label must be less than 63 characters.
	// See https://github.com/kubernetes/kubernetes/issues/64023 for a relevant discussion.
	if n := len(m) - 45; n > 0 {
		m = m[n:]
	}

	return fmt.Sprintf("%s-%s", runtime, m)
}
