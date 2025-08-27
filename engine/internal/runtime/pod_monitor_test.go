package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPodMonitor(t *testing.T) {
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod0",
				Namespace: "default",
				Annotations: map[string]string{
					modelAnnotationKey: "model0",
				},
			},
			Status: corev1.PodStatus{
				PodIP: "ip0",
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		},
	}

	var objs []apiruntime.Object
	for _, pod := range pods {
		objs = append(objs, pod)
	}

	k8sClient := fake.NewFakeClient(objs...)
	pm := NewPodMonitor(k8sClient)

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "pod0",
			Namespace: "default",
		},
	}
	ctx := context.Background()
	_, err := pm.Reconcile(ctx, req)
	assert.NoError(t, err)

	got := pm.modelStatus("model0")
	want := &modelStatus{
		numReadyPods: 1,
		numTotalPods: 1,
	}
	assert.Equal(t, want, got)
}
