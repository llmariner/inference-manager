package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSelectTarget(t *testing.T) {
	const (
		modelID   = "m0"
		stsName   = "sts0"
		namespace = "default"
	)

	labels := map[string]string{
		"app.kubernetes.io/instance": stsName,
	}

	tcs := []struct {
		name string

		pods        []*corev1.Pod
		wantPodName string
		wantErr     bool
	}{
		{
			name:    "no pods",
			wantErr: true,
		},
		{
			name: "no label matching pods",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod0",
						Namespace: namespace,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "no pod with IP",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod0",
						Namespace: namespace,
					},
					Status: corev1.PodStatus{
						PodIP: "ip0",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "ready and non-ready pod",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod0",
						Namespace: namespace,
						Labels:    labels,
					},
					Status: corev1.PodStatus{
						PodIP: "ip0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: namespace,
						Labels:    labels,
					},
					Status: corev1.PodStatus{
						PodIP: "ip1",
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
				},
			},
			wantPodName: "pod1",
		},
		{
			name: "no ready pod",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod0",
						Namespace: namespace,
						Labels:    labels,
					},
					Status: corev1.PodStatus{
						// No IP.
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: namespace,
						Labels:    labels,
					},
					Status: corev1.PodStatus{
						PodIP: "ip1",
					},
				},
			},
			wantPodName: "pod1",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var objs []apiruntime.Object
			for _, pod := range tc.pods {
				objs = append(objs, pod)
			}

			s := &loraAdapterLoadingTargetSelectorImpl{
				rtClientFactory: &fakeClientFactory{
					c: &fakeClient{
						namespace: namespace,
					},
				},
				k8sClient: fake.NewFakeClient(objs...),
			}

			got, err := s.selectTarget(context.Background(), modelID, stsName)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tc.wantPodName, got.Name)
		})
	}
}
