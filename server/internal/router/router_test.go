package router

import (
	"context"
	"fmt"
	"testing"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/server/internal/config"
	"github.com/llm-operator/inference-manager/server/internal/k8s"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

type mockEngineClient struct {
	models []*v1.Model
}

func (m *mockEngineClient) ListModels(
	ctx context.Context,
	req *v1.ListModelsRequest,
	opts ...grpc.CallOption,
) (*v1.ListModelsResponse, error) {
	return &v1.ListModelsResponse{
		Models: m.models,
	}, nil
}

func (m *mockEngineClient) PullModel(
	ctx context.Context,
	req *v1.PullModelRequest,
	opts ...grpc.CallOption,
) (*emptypb.Empty, error) {
	for _, m := range m.models {
		if req.Id == m.Id {
			return &emptypb.Empty{}, nil
		}
	}
	return nil, fmt.Errorf("model not found")
}

func (m *mockEngineClient) DeleteModel(
	ctx context.Context,
	req *v1.DeleteModelRequest,
	opts ...grpc.CallOption,
) (*emptypb.Empty, error) {
	for _, m := range m.models {
		if req.Id == m.Id {
			return &emptypb.Empty{}, nil
		}
	}
	return nil, fmt.Errorf("model not found")
}

var _ v1.InferenceEngineInternalServiceClient = &mockEngineClient{}

func TestRefreshRoutes(t *testing.T) {
	const (
		labelKey   = "app.kubernetes.io/name"
		labelValue = "inference-manager-engine"
		namespace  = "llm-operator"
	)
	clientset := fake.NewSimpleClientset(
		&apiv1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "inference-manager-engine-0",
				Namespace: namespace,
				Labels: map[string]string{
					labelKey: labelValue,
				},
			},
			Status: apiv1.PodStatus{
				PodIP: "1.2.3.4",
			},
		},
		&apiv1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "inference-manager-engine-1",
				Namespace: namespace,
				Labels: map[string]string{
					labelKey: labelValue,
				},
			},
			Status: apiv1.PodStatus{
				PodIP: "",
			},
		},
	)

	engineClient := &mockEngineClient{
		models: []*v1.Model{
			{
				Id: "model1",
			},
		},
	}

	fakeClient := &k8s.Client{
		CoreClientset: clientset,
	}
	c := config.InferenceManagerEngineConfig{
		OllamaPort:       8080,
		InternalGRPCPort: 8082,
		LabelKey:         labelKey,
		LabelValue:       labelValue,
		Namespace:        namespace,
	}
	r := New(c, fakeClient)
	r.engineClients = map[string]v1.InferenceEngineInternalServiceClient{
		"1.2.3.4": engineClient,
	}
	assert.Len(t, r.m.m, 0)
	assert.Len(t, r.m.engines, 0)

	err := r.refreshRoutes(context.Background())
	assert.NoError(t, err)

	assert.Len(t, r.m.m, 1)
	got, ok := r.m.m[model{id: "model1"}]
	assert.True(t, ok)
	assert.Equal(t, 1, len(got))
	assert.Equal(t, "1.2.3.4", got[0])

	assert.Len(t, r.m.engines, 1)
	_, ok = r.m.engines["1.2.3.4"]
	assert.True(t, ok)
}

func TestGetEngineForModel(t *testing.T) {
	engineClient := &mockEngineClient{
		models: []*v1.Model{
			{
				Id: "model1",
			},
		},
	}

	c := config.InferenceManagerEngineConfig{
		OllamaPort:       8080,
		InternalGRPCPort: 8082,
		LabelKey:         "app.kubernetes.io/name",
		LabelValue:       "inference-manager-engine",
		Namespace:        "llm-operator",
	}

	clientset := fake.NewSimpleClientset([]runtime.Object{}...)
	fakeClient := k8s.Client{
		CoreClientset: clientset,
	}
	r := New(c, &fakeClient)
	r.engineClients = map[string]v1.InferenceEngineInternalServiceClient{
		"1.2.3.4": engineClient,
	}
	r.m.addServer("1.2.3.4")

	addr, err := r.GetEngineForModel(context.Background(), "model1")
	assert.NoError(t, err)
	assert.Equal(t, "1.2.3.4:8080", addr)

	_, err = r.GetEngineForModel(context.Background(), "model2")
	assert.Error(t, err)
}
