package runtime

import (
	"context"
	"fmt"

	"github.com/llmariner/inference-manager/engine/internal/config"
	mv1 "github.com/llmariner/model-manager/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	nimHTTPPortName = "http-openai"
	// nimHTTPPort is the HTTP port for the NIM runtime.
	nimHTTPPort        = 8000
	ngcAPISecret       = "ngc-api"
	ngcAPIKey          = "NGC_API_KEY"
	ngcImagePullSecret = "ngc-secret"
	modelVolName       = "model-store"
)

// NewNIMClient creates a new NIM runtime client.
func NewNIMClient(
	opts NewCommonClientOptions,
	nconfig *config.NIMConfig,
	nmconfig *config.NIMModelConfig,
) Client {
	return &nimClient{
		commonClient: newCommonClient(opts, nimHTTPPort),
		config:       nconfig,
		modelConfig:  nmconfig,
	}
}

type nimClient struct {
	*commonClient
	config      *config.NIMConfig
	modelConfig *config.NIMModelConfig
}

// DeployRuntime deploys the runtime for the given model.
// TODO(guangrui): refactor to reuse the common code from client.go
func (c *nimClient) DeployRuntime(ctx context.Context, model *mv1.Model, update bool) (*appsv1.StatefulSet, error) {
	name := resourceName(c.RuntimeName(), c.modelConfig.ModelName)

	log := ctrl.LoggerFrom(ctx).WithValues("name", name)
	log.Info("Deploying nim runtime", "model", name, "update", update)

	nn := types.NamespacedName{Name: name, Namespace: c.namespace}
	labels := map[string]string{
		"app.kubernetes.io/name":       "runtime",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/created-by": managerName,
	}

	resConf := c.modelConfig.Resources
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

	pvcSpec := corev1apply.PersistentVolumeClaimSpec()
	if resConf.Volume != nil {
		if resConf.Volume.AccessMode != "" {
			pvcSpec = pvcSpec.WithAccessModes(corev1.PersistentVolumeAccessMode(resConf.Volume.AccessMode))
		}
		if resConf.Volume.StorageClassName != "" {
			pvcSpec = pvcSpec.WithStorageClassName(resConf.Volume.StorageClassName)
		}
		if resConf.Volume.Size != "" {
			volSize, err := resource.ParseQuantity(resConf.Volume.Size)
			if err != nil {
				return nil, fmt.Errorf("invalid volume size: %s", err)
			}
			pvcSpec = pvcSpec.WithResources(corev1apply.VolumeResourceRequirements().
				WithRequests(corev1.ResourceList{corev1.ResourceStorage: volSize}))
		}
	}

	volClaim := corev1apply.PersistentVolumeClaim(name, c.namespace).
		WithLabels(labels).
		WithSpec(pvcSpec)

	volumes := []*corev1apply.VolumeApplyConfiguration{
		corev1apply.Volume().WithName(modelVolName).
			WithPersistentVolumeClaim(
				corev1apply.PersistentVolumeClaimVolumeSource().WithClaimName(name)),
		corev1apply.Volume().WithName("dshm").
			WithEmptyDir(corev1apply.EmptyDirVolumeSource().
				WithMedium(corev1.StorageMediumMemory)),
	}

	volumeMounts := []*corev1apply.VolumeMountApplyConfiguration{
		corev1apply.VolumeMount().WithName(modelVolName).WithMountPath("/model-store"),
		corev1apply.VolumeMount().WithName("dshm").WithMountPath("/dev/shm"),
	}

	cport := c.servingPort
	if p := c.modelConfig.OpenAIPort; p != 0 {
		cport = p
	}

	envs := []*corev1apply.EnvVarApplyConfiguration{
		corev1apply.EnvVar().WithName("NIM_SERVER_PORT").WithValue(fmt.Sprintf("%d", cport)),
		corev1apply.EnvVar().WithName("NIM_LOG_LEVEL").WithValue(c.modelConfig.LogLevel),
		corev1apply.EnvVar().WithName("NIM_JSONL_LOGGING").WithValue("1"),
		corev1apply.EnvVar().WithName("NGC_API_KEY").
			WithValueFrom(corev1apply.EnvVarSource().
				WithSecretKeyRef(corev1apply.SecretKeySelector().
					WithName(ngcAPISecret).
					WithKey(ngcAPIKey))),
	}

	podSpec := corev1apply.PodSpec().
		WithContainers(corev1apply.Container().
			WithName("runtime").
			WithImage(c.modelConfig.Image).
			WithImagePullPolicy(corev1.PullPolicy(c.modelConfig.ImagePullPolicy)).
			WithPorts(corev1apply.ContainerPort().
				WithName(nimHTTPPortName).
				WithContainerPort(int32(cport)).
				WithProtocol(corev1.ProtocolTCP)).
			WithEnv(envs...).
			WithVolumeMounts(volumeMounts...).
			WithSecurityContext(
				corev1apply.SecurityContext().WithRunAsUser(0)).
			WithResources(runtimeResources).
			WithReadinessProbe(
				corev1apply.Probe().
					WithFailureThreshold(3).
					WithHTTPGet(corev1apply.HTTPGetAction().WithPort(intstr.FromInt(cport)).WithPath("/v1/health/ready"))))

	podSpec = podSpec.WithImagePullSecrets(corev1apply.LocalObjectReference().WithName(ngcImagePullSecret)).
		WithVolumes(volumes...).
		WithRuntimeClassName("nvidia")

	if c.rconfig.Affinity != nil {
		podSpec = podSpec.WithAffinity(buildAffinityApplyConfig(c.rconfig.Affinity))
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

	annos := map[string]string{
		runtimeAnnotationKey: c.RuntimeName(),
		modelAnnotationKey:   c.modelConfig.ModelName,
	}

	stsSpecConf := appsv1apply.StatefulSetSpec().
		WithReplicas(int32(1)).
		WithSelector(metav1apply.LabelSelector().
			WithMatchLabels(labels)).
		WithTemplate(corev1apply.PodTemplateSpec().
			WithAnnotations(c.rconfig.PodAnnotations).
			WithAnnotations(annos).
			WithLabels(labels).
			WithSpec(podSpec))

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

	volClaim = volClaim.WithOwnerReferences(ownerRef)

	svcSpec := corev1apply.ServiceSpec().
		WithSelector(labels).
		WithPorts(corev1apply.ServicePort().
			WithName("runtime").
			WithPort(int32(c.servingPort)))

	objs := []any{
		corev1apply.Service(name, c.namespace).
			WithLabels(labels).
			WithOwnerReferences(ownerRef).
			WithSpec(svcSpec),
		volClaim,
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

// RuntimeName returns the runtime name.
func (c *nimClient) RuntimeName() string {
	return config.RuntimeNameNIM
}

func (c *nimClient) GetName(modelID string) string {
	return resourceName(c.RuntimeName(), c.modelConfig.ModelName)
}
