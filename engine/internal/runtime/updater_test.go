package runtime

import (
	"testing"

	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUpdaterReconcile(t *testing.T) {
	const (
		namespace = "test-namespace"
	)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sts-model0",
			Namespace: namespace,
			Annotations: map[string]string{
				modelAnnotationKey: "model0",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To[int32](2),
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision:  "sts-updated-revision",
			CurrentRevision: "sts-current-revision",
		},
	}

	k8sClient := fake.NewFakeClient(sts)

	rtClient := &fakeClient{
		deployed:  map[string]bool{},
		k8sClient: k8sClient,
		namespace: namespace,
	}

	u := NewUpdater(
		namespace,
		true,
		k8sClient,
		&fakeClientFactory{c: rtClient},
	)

	ctx := testutil.ContextWithLogger(t)
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace},
	}
	_, err := u.Reconcile(ctx, req)
	assert.NoError(t, err)

	assert.Len(t, u.stsesByName, 1)
	assert.Len(t, u.listStatefulSets(), 1)

	rec, ok := u.stsesByName[sts.Name]
	assert.True(t, ok)
	assert.Equal(t, "model0", rec.modelID)

	err = k8sClient.Delete(ctx, sts)
	assert.NoError(t, err)
	_, err = u.Reconcile(ctx, req)
	assert.NoError(t, err)

	assert.Empty(t, u.stsesByName)
	assert.Empty(t, u.listStatefulSets())
}

func TestUpdaterDeleteDriftedPods(t *testing.T) {
	const (
		namespace = "test-namespace"
		stsName   = "sts-model0"
	)

	newPod := func(name, revision string, isReady bool) *corev1.Pod {
		condStatus := corev1.ConditionFalse
		if isReady {
			condStatus = corev1.ConditionTrue
		}

		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					appsv1.StatefulSetRevisionLabel: revision,
					"app.kubernetes.io/instance":    stsName,
				},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: condStatus,
					},
				},
			},
		}
	}

	tcs := []struct {
		name            string
		sts             *statefulSet
		pods            []*corev1.Pod
		deletedPodNames []string
	}{
		{
			name: "no drifted pod",
			sts: &statefulSet{
				name:           stsName,
				namespace:      namespace,
				modelID:        "model0",
				replicas:       1,
				updateRevision: "hash0",
			},
			pods: []*corev1.Pod{
				newPod("pod0", "hash0", true),
			},
			deletedPodNames: nil,
		},
		{
			name: "drifted pod with one-replica statefulset",
			sts: &statefulSet{
				name:           stsName,
				namespace:      namespace,
				modelID:        "model0",
				replicas:       1,
				updateRevision: "hash0",
			},
			pods: []*corev1.Pod{
				newPod("pod0", "hash1", true),
			},
			deletedPodNames: []string{"pod0"},
		},
		{
			name: "drifted pods with all ready pods",
			sts: &statefulSet{
				name:           stsName,
				namespace:      namespace,
				modelID:        "model0",
				replicas:       2,
				updateRevision: "hash0",
			},
			pods: []*corev1.Pod{
				newPod("pod0", "hash1", true),
				newPod("pod1", "hash1", true),
			},
			deletedPodNames: []string{"pod0"},
		},
		{
			name: "one drifted pod with all ready pods",
			sts: &statefulSet{
				name:           stsName,
				namespace:      namespace,
				modelID:        "model0",
				replicas:       2,
				updateRevision: "hash0",
			},
			pods: []*corev1.Pod{
				newPod("pod0", "hash0", true),
				newPod("pod1", "hash1", true),
			},
			deletedPodNames: []string{"pod1"},
		},
		{
			name: "drifted pod with all unready pods",
			sts: &statefulSet{
				name:           stsName,
				namespace:      namespace,
				modelID:        "model0",
				replicas:       2,
				updateRevision: "hash0",
			},
			pods: []*corev1.Pod{
				newPod("pod0", "hash1", false),
				newPod("pod1", "hash1", false),
			},
			deletedPodNames: nil,
		},
		{
			name: "drifted unready pod",
			sts: &statefulSet{
				name:           stsName,
				namespace:      namespace,
				modelID:        "model0",
				replicas:       4,
				updateRevision: "hash0",
			},
			pods: []*corev1.Pod{
				newPod("pod0", "hash0", true),
				newPod("pod1", "hash0", true),
				newPod("pod2", "hash1", false),
				newPod("pod3", "hash0", true),
			},
			deletedPodNames: []string{"pod2"},
		},
		{
			name: "drifted pod and unready pod different",
			sts: &statefulSet{
				name:           stsName,
				namespace:      namespace,
				modelID:        "model0",
				replicas:       4,
				updateRevision: "hash0",
			},
			pods: []*corev1.Pod{
				newPod("pod0", "hash0", true),
				newPod("pod1", "hash1", true),
				newPod("pod2", "hash0", false),
				newPod("pod3", "hash0", true),
			},
			deletedPodNames: nil,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			const namespace = "test-namespace"

			var objs []apiruntime.Object
			for _, pod := range tc.pods {
				objs = append(objs, pod)
			}
			k8sClient := fake.NewFakeClient(objs...)
			rtClient := &fakeClient{
				deployed:  map[string]bool{},
				k8sClient: k8sClient,
				namespace: namespace,
			}

			u := NewUpdater(
				namespace,
				true,
				k8sClient,
				&fakeClientFactory{c: rtClient},
			)
			u.logger = testutil.NewTestLogger(t)

			ctx := testutil.ContextWithLogger(t)
			err := u.deleteDriftedPods(ctx, tc.sts)
			assert.NoError(t, err)

			pods, err := listPods(ctx, u.k8sClient, tc.sts.namespace, tc.sts.name)
			assert.NoError(t, err)

			livePodNames := map[string]bool{}
			for _, pod := range pods {
				livePodNames[pod.Name] = true
			}

			var deletedPodNames []string
			for _, pod := range tc.pods {
				if livePodNames[pod.Name] {
					continue
				}
				deletedPodNames = append(deletedPodNames, pod.Name)
			}
			assert.ElementsMatch(t, tc.deletedPodNames, deletedPodNames)
		})
	}
}
