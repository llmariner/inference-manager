package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAddRuntime(t *testing.T) {
	createSts := func(name string, readyReplicas int32) appsv1.StatefulSet {
		return appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "test",
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: readyReplicas,
			},
		}
	}
	scaler := &fakeScalerRegister{registered: map[types.NamespacedName]bool{}}
	mgr := &Manager{
		rtClientFactory: &fakeClientFactory{},
		autoscaler:      scaler,
		runtimes:        map[string]runtime{},
	}

	added, ready, err := mgr.addRuntime("model-0", createSts("rt-0", 1))
	assert.NoError(t, err)
	assert.True(t, added)
	assert.True(t, ready)
	assert.True(t, mgr.runtimes["model-0"].ready)
	added, ready, err = mgr.addRuntime("model-0", createSts("rt-0", 1))
	assert.NoError(t, err)
	assert.False(t, added)
	assert.False(t, ready)

	added, ready, err = mgr.addRuntime("model-1", createSts("rt-1", 0))
	assert.NoError(t, err)
	assert.True(t, added)
	assert.False(t, ready)
	assert.False(t, mgr.runtimes["model-1"].ready)
	assert.Len(t, mgr.runtimes, 2)
}

func TestDeleteRuntime(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newPendingRuntime("rt-model-0"),
			"model-1": newReadyRuntime("rt-model-1", "test", 1, 1),
		},
	}
	mgr.deleteRuntime("rt-model-0")
	mgr.deleteRuntime("rt-model-1")
	assert.Empty(t, mgr.runtimes)
}

func TestMarkRuntimeReady(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newPendingRuntime("rt-model-0"),
		},
	}
	mgr.markRuntimeReady("rt-model-0", "model-0", "test", 1, 1)
	assert.True(t, mgr.runtimes["model-0"].ready)
	assert.Equal(t, "test", mgr.runtimes["model-0"].address)
	mgr.markRuntimeReady("rt-model-0", "model-0", "test", 1, 1)
	assert.True(t, mgr.runtimes["model-0"].ready)
	assert.Equal(t, "test", mgr.runtimes["model-0"].address)
}

func TestMarkRuntimeIsPending(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newReadyRuntime("rt-model-0", "test", 1, 1),
		},
	}
	mgr.markRuntimeIsPending("rt-model-0", "model-0")
	assert.False(t, mgr.runtimes["model-0"].ready)
	mgr.markRuntimeIsPending("rt-model-0", "model-0")
	assert.False(t, mgr.runtimes["model-0"].ready)
	assert.Zero(t, mgr.runtimes["model-0"].replicas)
}

func TestIsPending(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newPendingRuntime("rt-model-0"),
			"model-1": newReadyRuntime("rt-model-1", "test", 1, 1),
		},
	}
	ready, ok := mgr.isReady("model-0")
	assert.True(t, ok)
	assert.False(t, ready)
	ready, ok = mgr.isReady("model-1")
	assert.True(t, ok)
	assert.True(t, ready)
	assert.Equal(t, int32(1), mgr.runtimes["model-1"].replicas)
	_, ok = mgr.isReady("model-2")
	assert.False(t, ok)
}

func TestGetLLMAddress(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newReadyRuntime("rt-model-0", "test", 1, 1),
			"model-1": newPendingRuntime("rt-model-1"),
		},
	}
	addr, err := mgr.GetLLMAddress("model-0")
	assert.NoError(t, err)
	assert.Equal(t, "test", addr)
	_, err = mgr.GetLLMAddress("model-1")
	assert.Error(t, err)
}

func TestListSyncedModels(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newReadyRuntime("rt-model-0", "test", 1, 1),
			"model-1": newPendingRuntime("rt-model-1"),
			"model-2": newReadyRuntime("rt-model-2", "test2", 1, 2),
		},
	}
	models := mgr.ListSyncedModels()
	assert.Len(t, models, 2)
	for _, m := range models {
		contains := m.ID == "model-0" || m.ID == "model-2"
		assert.True(t, contains)
	}
}

func TestListInProgressModels(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newPendingRuntime("rt-model-0"),
			"model-1": newReadyRuntime("rt-model-1", "test", 1, 1),
			"model-2": newPendingRuntime("rt-model-2"),
		},
	}
	models := mgr.ListInProgressModels()
	assert.Len(t, models, 2)
	for _, m := range models {
		contains := m.ID == "model-0" || m.ID == "model-2"
		assert.True(t, contains)
	}
}

func TestPullModel(t *testing.T) {
	const (
		testModelID = "mid-0"
	)
	var tests = []struct {
		name          string
		rt            *runtime
		deployed      bool
		readyReplicas int32
		canceled      bool
	}{
		{
			name: "already ready",
			rt:   ptr.To(newReadyRuntime("rt-model-0", "test", 1, 1)),
		},
		{
			name: "already pending",
			rt:   ptr.To(newPendingRuntime("rt-model-1")),
		},
		{
			name:     "new model",
			deployed: true,
		},
		{
			name:          "not registered, but runtime already exists",
			deployed:      true,
			readyReplicas: 1,
		},
		{
			name:     "request is canceled",
			deployed: true,
			canceled: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rtClient := &fakeClient{deployed: map[string]bool{}, readyReplicas: test.readyReplicas}
			scaler := &fakeScalerRegister{registered: map[types.NamespacedName]bool{}}
			mgr := &Manager{
				runtimes:        map[string]runtime{},
				rtClientFactory: &fakeClientFactory{c: rtClient},
				autoscaler:      scaler,
			}
			if test.rt != nil {
				mgr.runtimes[testModelID] = *test.rt
			}
			ctx, cancel := context.WithTimeout(testutil.ContextWithLogger(t), 2*time.Second)
			defer cancel()
			go func() {
				// emulate runtime to be ready
				select {
				case <-ctx.Done():
					return
				case <-time.After(300 * time.Millisecond):
					if test.canceled {
						t.Log("request is canceled")
						mgr.cancelWaitingRequests(testModelID, "error")
					} else {
						t.Log("marking runtime ready")
						mgr.markRuntimeReady("rt-model-0", testModelID, "test", 1, 1)
					}
				}
			}()
			err := mgr.PullModel(ctx, testModelID)
			if test.canceled {
				assert.ErrorIs(t, err, ErrRequestCanceled)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, test.deployed, rtClient.deployed[testModelID])
			assert.Equal(t, test.deployed, len(scaler.registered) == 1)
		})
	}
}

func TestReconcile(t *testing.T) {
	const (
		name      = "rt-0"
		namespace = "ns-0"
		modelID   = "mid-0"
	)
	createSts := func(mutFn func(sts *appsv1.StatefulSet)) *appsv1.StatefulSet {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Annotations: map[string]string{modelAnnotationKey: modelID},
				Finalizers:  []string{finalizerKey},
			},
		}
		mutFn(sts)
		return sts
	}
	var tests = []struct {
		name string

		preFn func(ctx context.Context, m *Manager)
		sts   *appsv1.StatefulSet
		pod   *corev1.Pod
		rt    *runtime

		wantError   bool
		wantReady   bool
		wantChClose bool
		wantExtra   func(m *Manager, fs *fakeScalerRegister)
	}{
		{
			name: "still pending",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 1
			}),
			rt: ptr.To(newPendingRuntime(name)),
		},
		{
			name: "unreachable",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.ReadyReplicas = 1
				sts.Status.Replicas = 1
			}),
			rt: ptr.To(newPendingRuntime(name)),
			preFn: func(ctx context.Context, m *Manager) {
				http.DefaultClient = &http.Client{
					Transport: &fakeRoundTripper{resp: func() (*http.Response, error) {
						return nil, errors.New("runtime not reachable")
					}},
				}
			},
			wantError: true,
		},
		{
			name: "to be ready",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.ReadyReplicas = 1
				sts.Status.Replicas = 1
			}),
			rt: ptr.To(newPendingRuntime(name)),
			preFn: func(ctx context.Context, m *Manager) {
				http.DefaultClient = &http.Client{
					Transport: &fakeRoundTripper{resp: func() (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusOK}, nil
					}},
				}
			},
			wantReady:   true,
			wantChClose: true,
		},
		{
			name: "not-registered (ready)",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.ReadyReplicas = 1
				sts.Status.Replicas = 1
			}),
			preFn: func(ctx context.Context, m *Manager) {
				http.DefaultClient = &http.Client{
					Transport: &fakeRoundTripper{resp: func() (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusOK}, nil
					}},
				}
			},
			wantReady: true,
			wantExtra: func(m *Manager, fs *fakeScalerRegister) {
				assert.Len(t, fs.registered, 1, "scaler")
				assert.Len(t, m.runtimes, 1, "runtime")
			},
		},
		{
			name: "to be pending",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 0
			}),
			rt: ptr.To(newReadyRuntime(name, "test", 1, 1)),
		},
		{
			name: "not-registered (pending)",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 1
			}),
			wantReady: false,
			wantExtra: func(m *Manager, fs *fakeScalerRegister) {
				assert.Len(t, fs.registered, 1, "scaler")
				assert.Len(t, m.runtimes, 1, "runtime")
			},
		},
		{
			name: "not-registered (pending,unschedulable)",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 1
			}),
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-0",
					Namespace: namespace,
					Labels:    map[string]string{"app.kubernetes.io/instance": name},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodScheduled,
							Status: corev1.ConditionFalse,
							Reason: corev1.PodReasonUnschedulable,
						},
					},
				},
			},
			wantReady: false,
			wantExtra: func(m *Manager, fs *fakeScalerRegister) {
				assert.Len(t, fs.registered, 1, "scaler")
				assert.Len(t, m.runtimes, 1, "runtime")
				assert.Equal(t, corev1.PodReasonUnschedulable, m.runtimes[modelID].errReason)
			},
		},
		{
			name: "not found (pending)",
			sts:  nil,
			rt:   ptr.To(newPendingRuntime(name)),
			wantExtra: func(m *Manager, fs *fakeScalerRegister) {
				assert.Empty(t, fs.registered, "scaler")
				assert.Empty(t, m.runtimes, "runtime")
			},
		},
		{
			name: "not found (ready)",
			sts:  nil,
			rt:   ptr.To(newReadyRuntime(name, "test", 1, 1)),
			wantExtra: func(m *Manager, fs *fakeScalerRegister) {
				assert.Empty(t, fs.registered, "scaler")
				assert.Empty(t, m.runtimes, "runtime")
			},
		},
		{
			name: "unschedulable",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 1
			}),
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-0",
					Namespace: namespace,
					Labels:    map[string]string{"app.kubernetes.io/instance": name},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodScheduled,
							Status: corev1.ConditionFalse,
							Reason: corev1.PodReasonUnschedulable,
						},
					},
				},
			},
			rt:          ptr.To(newPendingRuntime(name)),
			wantChClose: true,
			wantExtra: func(m *Manager, fs *fakeScalerRegister) {
				assert.Equal(t, corev1.PodReasonUnschedulable, m.runtimes[modelID].errReason)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var objs []apiruntime.Object
			if test.sts != nil {
				objs = append(objs, test.sts)
			}
			if test.pod != nil {
				objs = append(objs, test.pod)
			}
			k8sClient := fake.NewFakeClient(objs...)
			rtClient := &fakeClient{deployed: map[string]bool{}}
			scaler := &fakeScalerRegister{registered: map[types.NamespacedName]bool{}}
			mgr := &Manager{
				k8sClient:       k8sClient,
				runtimes:        map[string]runtime{},
				rtClientFactory: &fakeClientFactory{c: rtClient},
				autoscaler:      scaler,
			}
			if test.rt != nil {
				mgr.runtimes[modelID] = *test.rt
			}

			ctx := testutil.ContextWithLogger(t)
			if test.preFn != nil {
				test.preFn(ctx, mgr)
			}
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			var chClosed atomic.Bool
			if test.wantChClose {
				go func() {
					select {
					case <-ctx.Done():
					case <-test.rt.waitCh:
						chClosed.Store(true)
					}
				}()
			}

			nn := types.NamespacedName{Name: name, Namespace: namespace}
			_, err := mgr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})

			if test.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if test.rt != nil {
				assert.Equal(t, test.wantReady, mgr.runtimes[modelID].ready)
			}
			if test.wantExtra != nil {
				test.wantExtra(mgr, scaler)
			}
			if test.wantChClose {
				assert.Eventually(t, func() bool {
					return chClosed.Load()
				}, time.Second, 100*time.Millisecond)
			}
		})
	}
}

func TestAllChildrenUnschedulable(t *testing.T) {
	ctx := testutil.ContextWithLogger(t)
	createPod := func(name, revision string, unschedulable bool) corev1.Pod {
		cond := corev1.ConditionTrue
		if unschedulable {
			cond = corev1.ConditionFalse
		}
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/instance": "test-sts",
					"controller-revision-hash":   revision,
				},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodScheduled,
						Status: cond,
						Reason: corev1.PodReasonUnschedulable,
					},
				},
			},
		}
	}

	testCases := []struct {
		name string
		pods []corev1.Pod
		want bool
	}{
		{
			name: "all pods unschedulable",
			pods: []corev1.Pod{
				createPod("pod-0", "revision-1", true),
				createPod("pod-1", "revision-1", true),
				createPod("pod-2", "revision-1", true),
			},
			want: true,
		},
		{
			name: "some pods unschedulable",
			pods: []corev1.Pod{
				createPod("pod-0", "revision-1", true),
				createPod("pod-1", "revision-1", false),
				createPod("pod-2", "revision-1", true),
			},
			want: false,
		},
		{
			name: "no pods unschedulable",
			pods: []corev1.Pod{
				createPod("pod-0", "revision-1", false),
				createPod("pod-1", "revision-2", true),
			},
			want: false,
		},
		{
			name: "no matching pods",
			pods: nil,
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []apiruntime.Object
			for _, pod := range tc.pods {
				objs = append(objs, &pod)
			}
			k8sClient := fake.NewFakeClient(objs...)

			sts := appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts",
					Namespace: "default",
				},
				Status: appsv1.StatefulSetStatus{
					CurrentRevision: "revision-1",
				},
			}

			unschedulable, err := allChildrenUnschedulable(ctx, k8sClient, sts)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, unschedulable)
		})
	}
}

func TestGetGPU(t *testing.T) {
	tcs := []struct {
		name string
		sts  appsv1.StatefulSet
		want int32
	}{
		{
			name: "gpu",
			sts: appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											nvidiaGPUResource: resource.MustParse("1"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "no gpu",
			sts: appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"cpu": resource.MustParse("1"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "multiple containers",
			sts: appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											nvidiaGPUResource: resource.MustParse("1"),
										},
									},
								},
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											nvidiaGPUResource: resource.MustParse("2"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: 3,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := getGPU(&tc.sts)
			assert.Equal(t, tc.want, got)
		})
	}
}

type fakeRoundTripper struct {
	resp func() (*http.Response, error)
}

func (s *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return s.resp()
}

type fakeClientFactory struct {
	c *fakeClient
}

func (f *fakeClientFactory) New(modelID string) (Client, error) {
	return f.c, nil
}

type fakeClient struct {
	deployed      map[string]bool
	readyReplicas int32
}

func (m *fakeClient) GetName(modelID string) string {
	return fmt.Sprintf("rt-%s", modelID)
}

func (m *fakeClient) GetAddress(name string) string {
	return fmt.Sprintf("%s:1234", name)
}

func (m *fakeClient) DeployRuntime(ctx context.Context, modelID string, update bool) (*appsv1.StatefulSet, error) {
	m.deployed[modelID] = true
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.GetName(modelID),
			Namespace: "default",
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: m.readyReplicas,
		},
	}, nil
}

func (m *fakeClient) DeleteRuntime(ctx context.Context, modelID string) error {
	m.deployed[modelID] = false
	return nil
}

type fakeScalerRegister struct {
	registered map[types.NamespacedName]bool
}

func (m *fakeScalerRegister) Register(ctx context.Context, modelID string, target *appsv1.StatefulSet) error {
	nn := types.NamespacedName{Name: target.Name, Namespace: target.Namespace}
	m.registered[nn] = true
	return nil
}

func (m *fakeScalerRegister) Unregister(nn types.NamespacedName) {
	delete(m.registered, nn)
}
