package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/llmariner/inference-manager/engine/internal/config"
	"github.com/llmariner/inference-manager/engine/internal/puller"
	mv1 "github.com/llmariner/model-manager/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
)

// Client is the interface for managing runtimes.
type Client interface {
	GetName(modelID string) string
	GetAddress(name string) string
	DeployRuntime(ctx context.Context, model *mv1.Model, update bool) (*appsv1.StatefulSet, error)
	DeleteRuntime(ctx context.Context, name, modelID string) error

	RuntimeName() string
	Namespace() string
}

// ClientFactory is the interface for creating a new Client given a model ID.
type ClientFactory interface {
	New(modelID string) (Client, error)
}

// NewCommonClientOptions are options for creating a commonClient.
type NewCommonClientOptions struct {
	K8sClient client.Client
	Namespace string
	Owner     *metav1apply.OwnerReferenceApplyConfiguration
	Rconfig   *config.RuntimeConfig
	Mconfig   *config.ProcessedModelConfig

	EnableDriftedPodUpdate        bool
	EnableOverrideWithModelConfig bool
}

func newCommonClient(opts NewCommonClientOptions, servingPort int) *commonClient {
	return &commonClient{
		k8sClient:                     opts.K8sClient,
		namespace:                     opts.Namespace,
		owner:                         opts.Owner,
		rconfig:                       opts.Rconfig,
		mconfig:                       opts.Mconfig,
		enableDriftedPodUpdate:        opts.EnableDriftedPodUpdate,
		enableOverrideWithModelConfig: opts.EnableOverrideWithModelConfig,
		servingPort:                   servingPort,
	}
}

type commonClient struct {
	k8sClient client.Client

	namespace string
	owner     *metav1apply.OwnerReferenceApplyConfiguration

	rconfig *config.RuntimeConfig
	mconfig *config.ProcessedModelConfig

	modelGetter modelGetter

	enableDriftedPodUpdate        bool
	enableOverrideWithModelConfig bool

	servingPort int
}

// Namespace returns the namespace of the runtime.
func (c *commonClient) Namespace() string {
	return c.namespace
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

// GetName returns a resource name of the runtime.
func (c *commonClient) GetName(modelID string) string {
	mci := c.mconfig.ModelConfigItem(modelID)
	return resourceName(mci.RuntimeName, modelID)
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
	model          *mv1.Model
	initEnvs       []*corev1apply.EnvVarApplyConfiguration
	envs           []*corev1apply.EnvVarApplyConfiguration
	volumes        []*corev1apply.VolumeApplyConfiguration
	volumeMounts   []*corev1apply.VolumeMountApplyConfiguration
	readinessProbe *corev1apply.ProbeApplyConfiguration

	command []string
	args    []string

	initContainerSpec *initContainerSpec

	additionalContainers []*corev1apply.ContainerApplyConfiguration

	// runtimePort is set to a non-zero value when the runtime serve requests on a port different from the serving port.
	runtimePort int

	dynamicModelLoading bool
	pullerDaemonMode    bool
	// pullerPort is the port number of the puller daemon.
	pullerPort int
}

// deployRuntime deploys the runtime for the given model.
func (c *commonClient) deployRuntime(
	ctx context.Context,
	params deployRuntimeParams,
	update bool,
) (*appsv1.StatefulSet, error) {
	// modelID is empty for Ollama dynamic model loading.
	var modelID string
	if params.model != nil {
		modelID = params.model.Id
	}

	mci, err := c.modelConfigItem(ctx, params.model)
	if err != nil {
		return nil, err
	}

	var name string
	if params.dynamicModelLoading {
		name = resourceName(mci.RuntimeName, daemonModeSuffix)
	} else {
		name = resourceName(mci.RuntimeName, params.model.Id)
	}

	log := ctrl.LoggerFrom(ctx).WithValues("name", name)
	log.Info("Deploying runtime", "model", modelID, "update", update)

	const (
		tmpDir        = "/tmp"
		subpathModel  = "model"
		subpathTmp    = "tmp"
		volName       = "model-volume"
		configVolName = "config"
	)

	nn := types.NamespacedName{Name: name, Namespace: c.namespace}
	labels := map[string]string{
		"app.kubernetes.io/name":       "runtime",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/created-by": managerName,
	}

	volumes := []*corev1apply.VolumeApplyConfiguration{
		corev1apply.Volume().
			WithName(configVolName).
			WithConfigMap(corev1apply.ConfigMapVolumeSource().
				WithName(c.rconfig.ConfigMapName)),
	}
	volumes = append(volumes, params.volumes...)

	var volClaim *corev1apply.PersistentVolumeClaimApplyConfiguration
	var forcePull bool
	resConf := mci.Resources
	if vol := resConf.Volume; vol != nil {
		pvcName := volName
		if resConf.Volume.ShareWithReplicas {
			pvcName = name
			volumes = append(volumes, corev1apply.Volume().WithName(volName).
				WithPersistentVolumeClaim(
					corev1apply.PersistentVolumeClaimVolumeSource().
						WithClaimName(name)))
		} else {
			// If shareWithReplicas is false, use StatefulSet volumeClaimTemplates
			// instead of directly specifying a volume.
			forcePull = true
		}
		spec := corev1apply.PersistentVolumeClaimSpec().
			WithAccessModes(corev1.PersistentVolumeAccessMode(vol.AccessMode))
		if vol.StorageClassName != "" {
			spec = spec.WithStorageClassName(vol.StorageClassName)
		}
		if vol.Size != "" {
			size, err := resource.ParseQuantity(vol.Size)
			if err != nil {
				return nil, fmt.Errorf("invalid volume size: %s", err)
			}
			spec = spec.WithResources(corev1apply.
				VolumeResourceRequirements().
				WithRequests(corev1.ResourceList{corev1.ResourceStorage: size}))
		}
		volClaim = corev1apply.
			PersistentVolumeClaim(pvcName, c.namespace).
			WithLabels(labels).
			WithSpec(spec)
	} else {
		v := corev1apply.EmptyDirVolumeSource()
		if c.rconfig.UseMemoryMediumForModelVolume {
			v = v.WithMedium(corev1.StorageMediumMemory)
		}
		volumes = append(volumes, corev1apply.Volume().WithName(volName).
			WithEmptyDir(v))
		// Make every pod pull the model on startup as they don't share the volume.
		forcePull = true
	}

	initVolumeMounts := []*corev1apply.VolumeMountApplyConfiguration{
		corev1apply.VolumeMount().WithName(volName).
			WithMountPath(puller.ModelDir()).WithSubPath(subpathModel),
		corev1apply.VolumeMount().WithName(volName).
			WithMountPath(tmpDir).WithSubPath(subpathTmp),
		corev1apply.VolumeMount().WithName(configVolName).
			WithMountPath("/etc/config").WithReadOnly(true),
	}
	volumeMounts := []*corev1apply.VolumeMountApplyConfiguration{
		corev1apply.VolumeMount().WithName(volName).
			WithMountPath(puller.ModelDir()).WithSubPath(subpathModel),
	}
	volumeMounts = append(volumeMounts, params.volumeMounts...)

	initEnvs := append(params.initEnvs,
		// The pod index label is supported in k8s 1.28 or later.
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
		"--model-id=" + modelID,
		"--config=/etc/config/config.yaml",
		"--force-pull=" + fmt.Sprintf("%t", forcePull),
	}
	if params.pullerDaemonMode {
		pullerArgs = append(pullerArgs, "--daemon-mode")
	}

	image, err := getImage(mci, c.rconfig)
	if err != nil {
		return nil, err
	}

	// Merge init environment variables with runtime config env
	allInitEnvs := mergeEnvVars(initEnvs, c.rconfig.Env)

	// Convert runtime config envFrom to apply configurations
	initEnvFroms := convertEnvFromToApplyConfig(c.rconfig.EnvFrom)

	pullerSpec := corev1apply.Container().
		WithName("puller").
		WithImage(c.rconfig.PullerImage).
		WithImagePullPolicy(corev1.PullPolicy(c.rconfig.PullerImagePullPolicy)).
		WithArgs(pullerArgs...).
		WithEnv(allInitEnvs...).
		WithEnvFrom(initEnvFroms...).
		WithVolumeMounts(initVolumeMounts...)
	if params.pullerDaemonMode {
		pullerSpec = pullerSpec.WithPorts(corev1apply.ContainerPort().
			WithName("puller").
			WithContainerPort(int32(params.pullerPort)).
			WithProtocol(corev1.ProtocolTCP)).
			// The init container will start and remain running during the entire life of the pod.
			// See https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/.
			WithRestartPolicy(corev1.ContainerRestartPolicyAlways)
	}
	podSpec := corev1apply.PodSpec().
		WithInitContainers(pullerSpec)
	if ic := params.initContainerSpec; ic != nil {
		podSpec = podSpec.WithInitContainers(corev1apply.Container().
			WithName(ic.name).
			WithImage(ic.image).
			WithImagePullPolicy(ic.imagePullPolicy).
			WithCommand(ic.command...).
			WithArgs(ic.args...).
			WithEnv(allInitEnvs...).
			WithEnvFrom(initEnvFroms...).
			WithVolumeMounts(initVolumeMounts...))
	}

	// Merge runtime environment variables with runtime config env
	allRuntimeEnvs := mergeEnvVars(params.envs, c.rconfig.Env)

	// Convert runtime config envFrom to apply configurations for runtime container
	runtimeEnvFroms := convertEnvFromToApplyConfig(c.rconfig.EnvFrom)

	cport := c.servingPort
	if p := params.runtimePort; p != 0 {
		cport = p
	}
	podSpec = podSpec.
		WithContainers(corev1apply.Container().
			WithName("runtime").
			WithImage(image).
			WithImagePullPolicy(corev1.PullPolicy(c.rconfig.RuntimeImagePullPolicy)).
			WithCommand(params.command...).
			WithArgs(params.args...).
			WithPorts(corev1apply.ContainerPort().
				WithName("http").
				WithContainerPort(int32(cport)).
				WithProtocol(corev1.ProtocolTCP)).
			WithEnv(allRuntimeEnvs...).
			WithEnvFrom(runtimeEnvFroms...).
			WithVolumeMounts(volumeMounts...).
			WithResources(runtimeResources).
			WithReadinessProbe(params.readinessProbe))

	if secrets := c.rconfig.RuntimeImagePullSecrets; len(secrets) > 0 {
		var objs []*corev1apply.LocalObjectReferenceApplyConfiguration
		for _, secret := range secrets {
			objs = append(objs, corev1apply.LocalObjectReference().WithName(secret))
		}
		podSpec = podSpec.WithImagePullSecrets(objs...)
	}

	if len(params.additionalContainers) > 0 {
		podSpec = podSpec.WithContainers(params.additionalContainers...)
	}

	podSpec = podSpec.WithVolumes(volumes...)

	if sa := c.rconfig.ServiceAccountName; sa != "" {
		podSpec = podSpec.WithServiceAccountName(sa)
	}
	if c.rconfig.Affinity != nil {
		podSpec = podSpec.WithAffinity(buildAffinityApplyConfig(c.rconfig.Affinity))
	}

	nodeSelector, err := c.nodeSelectorForModel(ctx, params.model)
	if err != nil {
		return nil, err
	}
	if len(nodeSelector) > 0 {
		podSpec = podSpec.WithNodeSelector(nodeSelector)
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

	if sn := mci.SchedulerName; sn != "" {
		podSpec = podSpec.WithSchedulerName(sn)
	}
	if rc := mci.ContainerRuntimeClassName; rc != "" {
		podSpec = podSpec.WithRuntimeClassName(rc)
	}

	if v := c.rconfig.TerminationGracePeriodSeconds; v != nil {
		podSpec = podSpec.WithTerminationGracePeriodSeconds(int64(*v))
	}

	annos := map[string]string{
		runtimeAnnotationKey: mci.RuntimeName,
	}
	if !params.dynamicModelLoading {
		annos[modelAnnotationKey] = modelID
	}

	stsSpecConf := appsv1apply.StatefulSetSpec().
		WithReplicas(int32(mci.Replicas)).
		WithSelector(metav1apply.LabelSelector().
			WithMatchLabels(labels)).
		WithTemplate(corev1apply.PodTemplateSpec().
			WithAnnotations(c.rconfig.PodAnnotations).
			WithAnnotations(annos).
			WithLabels(labels).
			WithSpec(podSpec))
	if vol := resConf.Volume; vol != nil && !vol.ShareWithReplicas {
		stsSpecConf = stsSpecConf.WithVolumeClaimTemplates(volClaim)
	}

	if c.enableDriftedPodUpdate {
		// Set the strategy to OnDelete as pods will be deleted by the updater.
		stsSpecConf = stsSpecConf.WithUpdateStrategy(
			appsv1apply.StatefulSetUpdateStrategy().
				WithType(appsv1.OnDeleteStatefulSetStrategyType))
	}

	stsConf := appsv1apply.StatefulSet(name, c.namespace).
		WithLabels(labels).
		WithAnnotations(annos).
		WithOwnerReferences(c.owner).
		WithSpec(stsSpecConf)

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

	svcSpec := corev1apply.ServiceSpec().
		WithSelector(labels).
		WithPorts(corev1apply.ServicePort().
			WithName("runtime").
			WithPort(int32(c.servingPort)))
	if params.pullerDaemonMode {
		svcSpec.WithPorts(corev1apply.ServicePort().
			WithName("puller").
			WithPort(int32(params.pullerPort)))
	}
	objs := []any{
		corev1apply.Service(name, c.namespace).
			WithLabels(labels).
			WithOwnerReferences(ownerRef).
			WithSpec(svcSpec),
	}

	if vol := resConf.Volume; vol != nil && vol.ShareWithReplicas {
		volClaim = volClaim.WithOwnerReferences(ownerRef)
		objs = append(objs, volClaim)
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

// DeleteRuntime deletes the runtime for the given model.
func (c *commonClient) DeleteRuntime(ctx context.Context, name, modelID string) error {
	log := ctrl.LoggerFrom(ctx).WithValues("name", name)
	log.Info("Deleting runtime", "model", modelID)

	var sts appsv1.StatefulSet
	nn := types.NamespacedName{Name: name, Namespace: c.namespace}
	if err := c.k8sClient.Get(ctx, nn, &sts); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(2).Info("StatefulSet not found")
			return nil
		}
	}

	if err := c.k8sClient.Delete(ctx, &sts); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}

	log.Info("Deleted runtime", "model", modelID)

	return nil
}

func (c *commonClient) modelConfigItem(
	ctx context.Context,
	model *mv1.Model,
) (config.ModelConfigItem, error) {
	mci := c.mconfig.ModelConfigItem(model.Id)

	if !c.enableOverrideWithModelConfig {
		return mci, nil
	}

	if model == nil {
		// model is empty for the dynamic Ollama model loading. Do not call GetModel.
		return mci, nil
	}

	mc := model.Config
	if mc == nil {
		return mci, nil
	}

	rc := mc.RuntimeConfig
	if rc == nil {
		return mci, nil
	}

	// Update replicas and resources from the model config.
	mci.Replicas = int(rc.Replicas)
	updateResourceConfWithModelConfig(&mci.Resources, rc)
	return mci, nil
}

func (c *commonClient) nodeSelectorForModel(ctx context.Context, model *mv1.Model) (map[string]string, error) {
	nodeSelector := map[string]string{}

	for k, v := range c.rconfig.NodeSelector {
		nodeSelector[k] = v
	}

	if model == nil {
		// model is empty for the dynamic Ollama model loading.
		return nodeSelector, nil
	}

	if p := model.Project; p != nil {
		// TODO(kenji): Only apply node selector effective for this cluster.
		for _, a := range p.Assignments {
			for _, n := range a.NodeSelector {
				nodeSelector[n.Key] = n.Value
			}
		}
	}

	return nodeSelector, nil
}

func resourceName(runtime, modelID string) string {
	// Avoid using illegal characters like "." or capital letters in the model names
	// TODO(kenji): Have a better way.
	m := strings.ToLower(modelID)
	for _, r := range []string{".", "_", ":", "/"} {
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

func buildAffinityApplyConfig(affinity *corev1.Affinity) *corev1apply.AffinityApplyConfiguration {
	nslrAC := func(nslr corev1.NodeSelectorRequirement) *corev1apply.NodeSelectorRequirementApplyConfiguration {
		ac := corev1apply.NodeSelectorRequirement()
		if nslr.Key != "" {
			ac = ac.WithKey(nslr.Key)
		}
		if nslr.Operator != "" {
			ac = ac.WithOperator(corev1.NodeSelectorOperator(nslr.Operator))
		}
		if len(nslr.Values) > 0 {
			ac = ac.WithValues(nslr.Values...)
		}
		return ac
	}
	nsltAC := func(nslt corev1.NodeSelectorTerm) *corev1apply.NodeSelectorTermApplyConfiguration {
		ac := corev1apply.NodeSelectorTerm()
		for _, me := range nslt.MatchExpressions {
			ac = ac.WithMatchExpressions(nslrAC(me))
		}
		for _, mf := range nslt.MatchFields {
			ac = ac.WithMatchFields(nslrAC(mf))
		}
		return ac
	}

	lslAC := func(lsl *metav1.LabelSelector) *metav1apply.LabelSelectorApplyConfiguration {
		ac := metav1apply.LabelSelector()
		if len(lsl.MatchLabels) > 0 {
			ac = ac.WithMatchLabels(lsl.MatchLabels)
		}
		for _, lse := range lsl.MatchExpressions {
			lsrAC := metav1apply.LabelSelectorRequirement()
			if lse.Key != "" {
				lsrAC = lsrAC.WithKey(lse.Key)
			}
			if lse.Operator != "" {
				lsrAC = lsrAC.WithOperator(metav1.LabelSelectorOperator(lse.Operator))
			}
			if len(lse.Values) > 0 {
				lsrAC = lsrAC.WithValues(lse.Values...)
			}
			ac = ac.WithMatchExpressions(lsrAC)
		}
		return ac
	}
	patAC := func(pat corev1.PodAffinityTerm) *corev1apply.PodAffinityTermApplyConfiguration {
		ac := corev1apply.PodAffinityTerm()
		if pat.TopologyKey != "" {
			ac = ac.WithTopologyKey(pat.TopologyKey)
		}
		if len(pat.Namespaces) > 0 {
			ac.WithNamespaces(pat.Namespaces...)
		}
		if len(pat.MatchLabelKeys) > 0 {
			ac.WithMatchLabelKeys(pat.MatchLabelKeys...)
		}
		if len(pat.MismatchLabelKeys) > 0 {
			ac.WithMismatchLabelKeys(pat.MismatchLabelKeys...)
		}
		if pat.LabelSelector != nil {
			ac = ac.WithLabelSelector(lslAC(pat.LabelSelector))
		}
		if pat.NamespaceSelector != nil {
			ac = ac.WithNamespaceSelector(lslAC(pat.NamespaceSelector))
		}
		return ac
	}

	afAC := corev1apply.Affinity()
	if na := affinity.NodeAffinity; na != nil {
		naAC := corev1apply.NodeAffinity()
		if ntr := na.RequiredDuringSchedulingIgnoredDuringExecution; ntr != nil {
			rdseAC := corev1apply.NodeSelector()
			for _, nslt := range ntr.NodeSelectorTerms {
				rdseAC = rdseAC.WithNodeSelectorTerms(nsltAC(nslt))
			}
			naAC = naAC.WithRequiredDuringSchedulingIgnoredDuringExecution(rdseAC)
		}
		for _, pdse := range na.PreferredDuringSchedulingIgnoredDuringExecution {
			naAC = naAC.WithPreferredDuringSchedulingIgnoredDuringExecution(corev1apply.
				PreferredSchedulingTerm().
				WithWeight(pdse.Weight).
				WithPreference(nsltAC(pdse.Preference)))
		}
		afAC = afAC.WithNodeAffinity(naAC)
	}
	if pa := affinity.PodAffinity; pa != nil {
		paAC := corev1apply.PodAffinity()
		for _, r := range pa.RequiredDuringSchedulingIgnoredDuringExecution {
			paAC = paAC.WithRequiredDuringSchedulingIgnoredDuringExecution(patAC(r))
		}
		for _, p := range pa.PreferredDuringSchedulingIgnoredDuringExecution {
			paAC = paAC.WithPreferredDuringSchedulingIgnoredDuringExecution(corev1apply.
				WeightedPodAffinityTerm().
				WithWeight(p.Weight).
				WithPodAffinityTerm(patAC(p.PodAffinityTerm)))
		}
		afAC = afAC.WithPodAffinity(paAC)
	}
	if paa := affinity.PodAntiAffinity; paa != nil {
		paaAC := corev1apply.PodAntiAffinity()
		for _, r := range paa.RequiredDuringSchedulingIgnoredDuringExecution {
			paaAC = paaAC.WithRequiredDuringSchedulingIgnoredDuringExecution(patAC(r))
		}
		for _, p := range paa.PreferredDuringSchedulingIgnoredDuringExecution {
			paaAC = paaAC.WithPreferredDuringSchedulingIgnoredDuringExecution(corev1apply.
				WeightedPodAffinityTerm().
				WithWeight(p.Weight).
				WithPodAffinityTerm(patAC(p.PodAffinityTerm)))
		}
		afAC = afAC.WithPodAntiAffinity(paaAC)
	}
	return afAC
}

// convertEnvToApplyConfig converts a slice of corev1.EnvVar to apply configuration format
func convertEnvToApplyConfig(envVars []corev1.EnvVar) []*corev1apply.EnvVarApplyConfiguration {
	var result []*corev1apply.EnvVarApplyConfiguration
	for _, env := range envVars {
		envVar := corev1apply.EnvVar().WithName(env.Name)
		if env.Value != "" {
			envVar = envVar.WithValue(env.Value)
		}
		if env.ValueFrom != nil {
			// Convert corev1.EnvVarSource to apply configuration
			valueFromApply := corev1apply.EnvVarSource()
			if fr := env.ValueFrom.FieldRef; fr != nil {
				valueFromApply = valueFromApply.WithFieldRef(corev1apply.ObjectFieldSelector().
					WithAPIVersion(fr.APIVersion).
					WithFieldPath(fr.FieldPath))
			}
			if rfr := env.ValueFrom.ResourceFieldRef; rfr != nil {
				valueFromApply = valueFromApply.WithResourceFieldRef(corev1apply.ResourceFieldSelector().
					WithContainerName(rfr.ContainerName).
					WithResource(rfr.Resource).
					WithDivisor(rfr.Divisor))
			}
			if cmkr := env.ValueFrom.ConfigMapKeyRef; cmkr != nil {
				configMapKeyRef := corev1apply.ConfigMapKeySelector().
					WithName(cmkr.Name).
					WithKey(cmkr.Key)
				if cmkr.Optional != nil {
					configMapKeyRef = configMapKeyRef.WithOptional(*cmkr.Optional)
				}
				valueFromApply = valueFromApply.WithConfigMapKeyRef(configMapKeyRef)
			}
			if skr := env.ValueFrom.SecretKeyRef; skr != nil {
				secretKeyRef := corev1apply.SecretKeySelector().
					WithName(skr.Name).
					WithKey(skr.Key)
				if skr.Optional != nil {
					secretKeyRef = secretKeyRef.WithOptional(*skr.Optional)
				}
				valueFromApply = valueFromApply.WithSecretKeyRef(secretKeyRef)
			}
			envVar = envVar.WithValueFrom(valueFromApply)
		}
		result = append(result, envVar)
	}
	return result
}

// convertEnvFromToApplyConfig converts a slice of corev1.EnvFromSource to apply configuration format
func convertEnvFromToApplyConfig(envFromSources []corev1.EnvFromSource) []*corev1apply.EnvFromSourceApplyConfiguration {
	var result []*corev1apply.EnvFromSourceApplyConfiguration
	for _, envFrom := range envFromSources {
		envFromApply := corev1apply.EnvFromSource().
			WithPrefix(envFrom.Prefix)
		if cfr := envFrom.ConfigMapRef; cfr != nil {
			configMapEnvSource := corev1apply.ConfigMapEnvSource().WithName(cfr.Name)
			if cfr.Optional != nil {
				configMapEnvSource = configMapEnvSource.WithOptional(*cfr.Optional)
			}
			envFromApply = envFromApply.WithConfigMapRef(configMapEnvSource)
		}
		if sr := envFrom.SecretRef; sr != nil {
			secretEnvSource := corev1apply.SecretEnvSource().WithName(sr.Name)
			if sr.Optional != nil {
				secretEnvSource = secretEnvSource.WithOptional(*sr.Optional)
			}
			envFromApply = envFromApply.WithSecretRef(secretEnvSource)
		}
		result = append(result, envFromApply)
	}
	return result
}

// mergeEnvVars merges existing environment variables with runtime config environment variables
func mergeEnvVars(existingEnvs []*corev1apply.EnvVarApplyConfiguration, runtimeConfigEnvs []corev1.EnvVar) []*corev1apply.EnvVarApplyConfiguration {
	allEnvs := make([]*corev1apply.EnvVarApplyConfiguration, 0, len(existingEnvs)+len(runtimeConfigEnvs))
	allEnvs = append(allEnvs, existingEnvs...)
	allEnvs = append(allEnvs, convertEnvToApplyConfig(runtimeConfigEnvs)...)
	return allEnvs
}

func getImage(mci config.ModelConfigItem, rconfig *config.RuntimeConfig) (string, error) {
	if mci.Image != "" {
		return mci.Image, nil
	}
	image, ok := rconfig.RuntimeImages[mci.RuntimeName]
	if !ok {
		return "", fmt.Errorf("runtime image not found for %s", mci.RuntimeName)
	}
	return image, nil
}

func updateResourceConfWithModelConfig(resConf *config.Resources, rc *mv1.ModelConfig_RuntimeConfig) {
	res := rc.Resources
	if res == nil {
		return
	}

	v := strconv.Itoa(int(res.Gpu))
	if resConf.Requests == nil {
		resConf.Requests = map[string]string{}
	}
	resConf.Requests[nvidiaGPUResource] = v

	if resConf.Limits == nil {
		resConf.Limits = map[string]string{}
	}
	resConf.Limits[nvidiaGPUResource] = v
}
