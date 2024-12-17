package autoscaler

import (
	"context"
	"testing"

	"github.com/kedacore/keda/v2/pkg/generated/clientset/versioned/fake"
	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func TestDeployScaledObject(t *testing.T) {
	const (
		model = "model0"
		ns    = "test"
	)

	conf := KedaConfig{
		PromServerAddress: "prom-addr",
		PromTriggers: []KedaPromTrigger{
			{
				Threshold: 10,
				Query:     `avg(vllm:num_requests_running{model="{{.}}"})`,
			},
		},
	}
	err := conf.validate()
	assert.NoError(t, err)

	fakeClient := fake.NewSimpleClientset().KedaV1alpha1()
	s := KedaScaler{
		config:   conf,
		client:   fakeClient,
		recorder: record.NewFakeRecorder(1),
		logger:   testutil.NewTestLogger(t),
	}

	err = s.createScaledObject(context.Background(), model, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      model,
			Namespace: ns,
		},
	})
	assert.NoError(t, err)

	wantMeta := map[string]string{
		"serverAddress": conf.PromServerAddress,
		"query":         `avg(vllm:num_requests_running{model="model0"})`,
		"threshold":     "10",
	}
	so, err := fakeClient.ScaledObjects(ns).Get(context.Background(), model, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, wantMeta, so.Spec.Triggers[0].Metadata)
}
