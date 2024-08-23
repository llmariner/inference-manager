package runtime

import (
	"context"

	"github.com/llm-operator/inference-manager/engine/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	managerName = "inference-engine"

	runtimeAnnotationKey = "llm-operator/runtime"
	modelAnnotationKey   = "llm-operator/model"

	finalizerKey = "llm-operator/runtime-finalizer"
)

// Client is the interface for managing runtimes.
type Client interface {
	GetAddress(name string) string
	DeployRuntime(ctx context.Context, modelID string) error
}

type commonClient struct {
	k8sClient client.Client

	namespace string
	config.RuntimeConfig
}

func (o *commonClient) getResouces(modelID string) config.Resources {
	if res, ok := o.ModelResources[modelID]; ok {
		return res
	}
	return o.DefaultResources
}

func (o *commonClient) applyObject(ctx context.Context, applyConfig any) (client.Object, error) {
	uobj, err := apiruntime.DefaultUnstructuredConverter.ToUnstructured(applyConfig)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{Object: uobj}
	opts := &client.PatchOptions{FieldManager: managerName, Force: ptr.To(true)}
	if err := o.k8sClient.Patch(ctx, obj, client.Apply, opts); err != nil {
		return nil, err
	}
	return obj, nil
}
