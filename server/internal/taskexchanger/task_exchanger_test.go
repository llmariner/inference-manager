package taskexchanger

import (
	"context"
	"testing"
	"time"

	v1 "github.com/llmariner/inference-manager/api/v1"
	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	"github.com/llmariner/inference-manager/server/internal/router"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestE_Reconcile(t *testing.T) {
	iprocessor := infprocessor.NewP(
		router.New(true),
		testutil.NewTestLogger(t),
	)

	pod0 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "server0",
			Namespace: "llmariner",
		},
		Status: corev1.PodStatus{
			PodIP: "ip0",
		},
	}
	// This pod is not ready.
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "server1",
			Namespace: "llmariner",
		},
		Status: corev1.PodStatus{
			PodIP: "",
		},
	}

	exchanger := NewE(
		iprocessor,
		fake.NewFakeClient(pod0, pod1),
		8080,
		"localPodName",
		"podLabelKey",
		"podLabelValue",
		testutil.NewTestLogger(t),
	)

	ctx := testutil.ContextWithLogger(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	_, err := exchanger.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      pod0.Name,
			Namespace: pod0.Namespace,
		},
	})
	assert.NoError(t, err)
	assert.Len(t, exchanger.taskReceivers, 1)

	r, ok := exchanger.taskReceivers["server0"]
	assert.True(t, ok)

	// A new task sender is not created since the pod is not ready.
	_, err = exchanger.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      pod1.Name,
			Namespace: pod1.Namespace,
		},
	})
	assert.NoError(t, err)
	assert.Len(t, exchanger.taskReceivers, 1)

	exchanger.StartGracefulShutdown()

	r.stop()

	assert.Eventually(t, func() bool {
		exchanger.mu.Lock()
		defer exchanger.mu.Unlock()
		return len(exchanger.taskReceivers) == 0
	}, 1*time.Second, 100*time.Millisecond)
}

func TestE_AddUpdateRemoveServer(t *testing.T) {
	iprocessor := infprocessor.NewP(
		router.New(true),
		testutil.NewTestLogger(t),
	)

	exchanger := NewE(
		iprocessor,
		fake.NewFakeClient(),
		8080,
		"localPodName",
		"podLabelKey",
		"podLabelValue",
		testutil.NewTestLogger(t),
	)

	status := &v1.ServerStatus{
		PodName: "server0",
	}
	exchanger.AddOrUpdateServerStatus(&fakeTaskSenderSrv{}, status)
	assert.Len(t, exchanger.taskSenders, 1)

	exchanger.AddOrUpdateServerStatus(&fakeTaskSenderSrv{}, status)
	assert.Len(t, exchanger.taskSenders, 1)

	status = &v1.ServerStatus{
		PodName: "server1",
	}
	exchanger.AddOrUpdateServerStatus(&fakeTaskSenderSrv{}, status)
	assert.Len(t, exchanger.taskSenders, 2)

	exchanger.RemoveServer("server0")
	assert.Len(t, exchanger.taskSenders, 1)
}
