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
		Status: appsv1.StatefulSetStatus{
			UpdateRevision:  "sts-updated-revision",
			CurrentRevision: "sts-current-revision",
		},
	}

	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-model0-0",
				Namespace: namespace,
				Labels: map[string]string{
					appsv1.StatefulSetRevisionLabel: "sts-updated-revision",
					"app.kubernetes.io/instance":    sts.Name,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-model0-1",
				Namespace: namespace,
				Labels: map[string]string{
					appsv1.StatefulSetRevisionLabel: "sts-current-revision",
					"app.kubernetes.io/instance":    sts.Name,
				},
			},
		},
	}

	var objs []apiruntime.Object
	objs = append(objs, sts)
	for _, p := range pods {
		objs = append(objs, p)
	}
	k8sClient := fake.NewFakeClient(objs...)

	rtClient := &fakeClient{
		deployed:  map[string]bool{},
		k8sClient: k8sClient,
		namespace: namespace,
	}

	u := NewUpdater(
		namespace,
		k8sClient,
		&fakeClientFactory{c: rtClient},
	)

	ctx := testutil.ContextWithLogger(t)
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace},
	}
	_, err := u.Reconcile(ctx, req)
	assert.NoError(t, err)

	rec, ok := u.stsesByName[sts.Name]
	assert.True(t, ok)
	assert.Equal(t, "model0", rec.modelID)
	assert.Len(t, rec.driftedPods, 1)
	assert.Equal(t, "pod-model0-1", rec.driftedPods[0].Name)

	err = k8sClient.Delete(ctx, sts)
	assert.NoError(t, err)
	_, err = u.Reconcile(ctx, req)
	assert.NoError(t, err)

	assert.Empty(t, u.stsesByName)
}
