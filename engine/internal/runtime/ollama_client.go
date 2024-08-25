package runtime

import (
	"context"
	"fmt"
	"strconv"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RuntimeNameOllama is the name of the Ollama runtime.
const RuntimeNameOllama = "ollama"

const ollamaHTTPPort = 11434

// NewOllamaClient creates a new Ollama runtime client.
func NewOllamaClient(
	k8sClient client.Client,
	namespace string,
	rconfig config.RuntimeConfig,
	oconfig config.OllamaConfig,
) Client {
	return &ollamaClient{
		commonClient: &commonClient{
			k8sClient:     k8sClient,
			namespace:     namespace,
			RuntimeConfig: rconfig,
		},
		config: oconfig,
	}
}

type ollamaClient struct {
	*commonClient

	config config.OllamaConfig
}

// GetAddress returns the address of the runtime.
func (o *ollamaClient) GetAddress(name string) string {
	return fmt.Sprintf("%s:%d", name, ollamaHTTPPort)
}

// DeployRuntime deploys the runtime for the given model.
func (o *ollamaClient) DeployRuntime(ctx context.Context, modelID string) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Deploying runtime", "model", modelID)

	const (
		modelDir      = "/models"
		tmpDir        = "/tmp"
		subpathModel  = "model"
		subpathTmp    = "tmp"
		shareVolName  = "share-volume"
		configVolName = "config"
	)

	name := fmt.Sprintf("%s-%s", RuntimeNameOllama, modelID)
	labels := map[string]string{
		"app.kubernetes.io/name":       "runtime",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/created-by": managerName,
	}

	resConf := o.getResouces(modelID)

	sharedVolume := corev1apply.Volume().WithName(shareVolName)
	if resConf.Volume != nil {
		sharedVolume.WithPersistentVolumeClaim(
			corev1apply.PersistentVolumeClaimVolumeSource().
				WithClaimName(name))
	} else {
		sharedVolume.WithEmptyDir(corev1apply.EmptyDirVolumeSource())
	}
	volumes := []*corev1apply.VolumeApplyConfiguration{
		sharedVolume,
		corev1apply.Volume().
			WithName(configVolName).
			WithConfigMap(corev1apply.ConfigMapVolumeSource().
				WithName(o.ConfigMapName)),
	}

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

	initEnvs := []*corev1apply.EnvVarApplyConfiguration{
		corev1apply.EnvVar().WithName("OLLAMA_MODELS").WithValue(modelDir),
		corev1apply.EnvVar().WithName("INDEX").
			WithValueFrom(corev1apply.EnvVarSource().
				WithFieldRef(corev1apply.ObjectFieldSelector().
					WithFieldPath("metadata.labels['apps.kubernetes.io/pod-index']"))),
		corev1apply.EnvVar().WithName("AWS_ACCESS_KEY_ID").
			WithValueFrom(corev1apply.EnvVarSource().
				WithSecretKeyRef(corev1apply.SecretKeySelector().
					WithName(o.AWSSecretName).
					WithKey(o.AWSKeyIDEnvKey))),
		corev1apply.EnvVar().WithName("AWS_SECRET_ACCESS_KEY").
			WithValueFrom(corev1apply.EnvVarSource().
				WithSecretKeyRef(corev1apply.SecretKeySelector().
					WithName(o.AWSSecretName).
					WithKey(o.AWSAccessKeyEnvKey))),
		corev1apply.EnvVar().WithName("LLMO_CLUSTER_REGISTRATION_KEY").
			WithValueFrom(corev1apply.EnvVarSource().
				WithSecretKeyRef(corev1apply.SecretKeySelector().
					WithName(o.LLMOWorkerSecretName).
					WithKey(o.LLMOKeyEnvKey))),
	}
	envs := []*corev1apply.EnvVarApplyConfiguration{
		corev1apply.EnvVar().WithName("OLLAMA_MODELS").WithValue(modelDir),
		corev1apply.EnvVar().WithName("OLLAMA_KEEP_ALIVE").WithValue(o.config.KeepAlive.String()),
	}
	if o.config.NumParallel > 0 {
		envs = append(envs, corev1apply.EnvVar().WithName("OLLAMA_NUM_PARALLEL").WithValue(strconv.Itoa(o.config.NumParallel)))
	}
	if o.config.ForceSpreading {
		envs = append(envs, corev1apply.EnvVar().WithName("OLLAMA_FORCE_SPREADING").WithValue("true"))
	}
	if o.config.Debug {
		envs = append(envs, corev1apply.EnvVar().WithName("OLLAMA_DEBUG").WithValue("true"))
	}

	runtimeResources := corev1apply.ResourceRequirements()
	if len(resConf.Requests) > 0 {
		reqs := make(corev1.ResourceList, len(resConf.Requests))
		for name, v := range resConf.Requests {
			val, err := resource.ParseQuantity(v)
			if err != nil {
				return fmt.Errorf("invalid resource request %s: %s", name, err)
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
				return fmt.Errorf("invalid resource limit %s: %s", name, err)
			}
			limits[corev1.ResourceName(name)] = val
		}
		runtimeResources.WithLimits(limits)
	}

	pullerArgs := []string{
		"pull",
		"--index=$(INDEX)",
		"--runtime=" + RuntimeNameOllama,
		"--model-id=" + modelID,
		"--config=/etc/config/config.yaml",
	}

	stsConf := appsv1apply.StatefulSet(name, o.namespace).
		WithLabels(labels).
		WithAnnotations(map[string]string{
			runtimeAnnotationKey: RuntimeNameOllama,
			modelAnnotationKey:   modelID}).
		WithFinalizers(finalizerKey).
		WithSpec(appsv1apply.StatefulSetSpec().
			WithReplicas(1).
			WithSelector(metav1apply.LabelSelector().
				WithMatchLabels(labels)).
			WithTemplate(corev1apply.PodTemplateSpec().
				WithLabels(labels).
				WithSpec(corev1apply.PodSpec().
					WithInitContainers(corev1apply.Container().
						WithName("puller").
						WithImage(o.PullerImage).
						WithImagePullPolicy(corev1.PullPolicy(o.PullerImagePullPolicy)).
						WithArgs(pullerArgs...).
						WithEnv(initEnvs...).
						WithVolumeMounts(initVolumeMounts...)).
					WithContainers(corev1apply.Container().
						WithName("runtime").
						WithImage(o.RuntimeImage).
						WithImagePullPolicy(corev1.PullPolicy(o.RuntimeImagePullPolicy)).
						WithArgs("serve").
						WithPorts(corev1apply.ContainerPort().
							WithName("http").
							WithContainerPort(ollamaHTTPPort).
							WithProtocol(corev1.ProtocolTCP)).
						WithEnv(envs...).
						WithVolumeMounts(volumeMounts...).
						WithResources(runtimeResources).
						WithReadinessProbe(corev1apply.Probe().
							WithHTTPGet(corev1apply.HTTPGetAction().
								WithPort(intstr.FromInt(ollamaHTTPPort))))).
					WithVolumes(volumes...))))

	sts, err := o.applyObject(ctx, stsConf)
	if err != nil {
		return err
	}
	log.V(2).Info("StatefulSet applied", "name", sts.GetName())

	gvk := sts.GetObjectKind().GroupVersionKind()
	ownerRef := metav1apply.OwnerReference().
		WithAPIVersion(gvk.GroupVersion().String()).
		WithKind(gvk.Kind).
		WithName(sts.GetName()).
		WithUID(sts.GetUID()).
		WithBlockOwnerDeletion(true).
		WithController(true)

	objs := []any{
		corev1apply.Service(name, o.namespace).
			WithLabels(labels).
			WithOwnerReferences(ownerRef).
			WithSpec(corev1apply.ServiceSpec().
				WithSelector(labels).
				WithPorts(corev1apply.ServicePort().
					WithName("runtime").
					WithPort(ollamaHTTPPort))),
	}

	if vol := resConf.Volume; vol != nil {
		size, err := resource.ParseQuantity(vol.Size)
		if err != nil {
			return fmt.Errorf("invalid volume size: %s", err)
		}
		objs = append(objs, corev1apply.PersistentVolumeClaim(name, o.namespace).
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
		newObj, err := o.applyObject(ctx, obj)
		if err != nil {
			return err
		}
		kind := newObj.GetObjectKind().GroupVersionKind().Kind
		log.V(2).Info(fmt.Sprintf("%s applied", kind), "name", newObj.GetName())
	}
	return nil
}
