package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	testutil "github.com/llm-operator/inference-manager/engine/internal/test"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
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

	added, err := mgr.addRuntime("model-0", createSts("rt-0", 1))
	assert.NoError(t, err)
	assert.True(t, added)
	assert.True(t, mgr.runtimes["model-0"].ready)
	added, err = mgr.addRuntime("model-0", createSts("rt-0", 1))
	assert.NoError(t, err)
	assert.False(t, added)

	added, err = mgr.addRuntime("model-1", createSts("rt-1", 0))
	assert.NoError(t, err)
	assert.True(t, added)
	assert.False(t, mgr.runtimes["model-1"].ready)
	assert.Len(t, mgr.runtimes, 2)
}

func TestDeleteRuntime(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newPendingRuntime(),
			"model-1": newReadyRuntime("test"),
		},
	}
	mgr.deleteRuntime("model-0")
	mgr.deleteRuntime("model-1")
	assert.Empty(t, mgr.runtimes)
}

func TestMarkRuntimeReady(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newPendingRuntime(),
		},
	}
	mgr.markRuntimeReady("model-0", "test")
	assert.True(t, mgr.runtimes["model-0"].ready)
	assert.Equal(t, "test", mgr.runtimes["model-0"].address)
	mgr.markRuntimeReady("model-0", "test")
	assert.True(t, mgr.runtimes["model-0"].ready)
	assert.Equal(t, "test", mgr.runtimes["model-0"].address)
}

func TestMarkRuntimeIsPending(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newReadyRuntime("test"),
		},
	}
	mgr.markRuntimeIsPending("model-0")
	assert.False(t, mgr.runtimes["model-0"].ready)
	mgr.markRuntimeIsPending("model-0")
	assert.False(t, mgr.runtimes["model-0"].ready)
}

func TestIsPending(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newPendingRuntime(),
			"model-1": newReadyRuntime("test"),
		},
	}
	ready, ok := mgr.isReady("model-0")
	assert.True(t, ok)
	assert.False(t, ready)
	ready, ok = mgr.isReady("model-1")
	assert.True(t, ok)
	assert.True(t, ready)
	_, ok = mgr.isReady("model-2")
	assert.False(t, ok)
}

func TestGetLLMAddress(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newReadyRuntime("test"),
			"model-1": newPendingRuntime(),
		},
	}
	addr, err := mgr.GetLLMAddress("model-0")
	assert.NoError(t, err)
	assert.Equal(t, "test", addr)
	_, err = mgr.GetLLMAddress("model-1")
	assert.Error(t, err)
}

func TestListSyncedModelIDs(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newReadyRuntime("test"),
			"model-1": newPendingRuntime(),
			"model-2": newReadyRuntime("test2"),
		},
	}
	models := mgr.ListSyncedModelIDs(context.Background())
	assert.Len(t, models, 2)
	assert.Contains(t, models, "model-0")
	assert.Contains(t, models, "model-2")
}

func TestListProgressModelIDs(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]runtime{
			"model-0": newPendingRuntime(),
			"model-1": newReadyRuntime("test"),
			"model-2": newPendingRuntime(),
		},
	}
	models := mgr.ListInProgressModels()
	assert.Len(t, models, 2)
}

func TestPullModel(t *testing.T) {
	const (
		testModelID = "mid-0"
	)
	var tests = []struct {
		name     string
		rt       *runtime
		deployed bool
	}{
		{
			name: "already ready",
			rt:   ptr.To(newReadyRuntime("test")),
		},
		{
			name: "already pending",
			rt:   ptr.To(newPendingRuntime()),
		},
		{
			name:     "new model",
			deployed: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rtClient := &fakeClient{deployed: map[string]bool{}}
			scaler := &fakeScalerRegister{registered: map[types.NamespacedName]bool{}}
			mgr := &Manager{
				runtimes:        map[string]runtime{},
				rtClientFactory: &fakeClientFactory{c: rtClient},
				autoscaler:      scaler,
			}
			if test.rt != nil {
				mgr.runtimes[testModelID] = *test.rt
			}
			ctx, cancel := context.WithCancel(testutil.ContextWithLogger(t))
			go func() {
				time.Sleep(2 * time.Second) // timeout
				t.Log("canceling context")
				cancel()
			}()
			go func() {
				// emulate runtime to be ready
				time.Sleep(300 * time.Millisecond)
				t.Log("marking runtime ready")
				mgr.markRuntimeReady(testModelID, "test")
			}()
			err := mgr.PullModel(ctx, testModelID)
			assert.NoError(t, err)
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
		rt    *runtime

		wantError bool
		wantReady bool
		wantExtra func(m *Manager, fs *fakeScalerRegister)
	}{
		{
			name: "still pending",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 1
			}),
			rt: ptr.To(newPendingRuntime()),
		},
		{
			name: "unreachable",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.ReadyReplicas = 1
				sts.Status.Replicas = 1
			}),
			rt: ptr.To(newPendingRuntime()),
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
			rt: ptr.To(newPendingRuntime()),
			preFn: func(ctx context.Context, m *Manager) {
				http.DefaultClient = &http.Client{
					Transport: &fakeRoundTripper{resp: func() (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusOK}, nil
					}},
				}
			},
			wantReady: true,
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
			rt: ptr.To(newReadyRuntime("test")),
		},
		{
			name: "not-registered (pending)",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 1
			}),
			preFn: func(ctx context.Context, m *Manager) {
				http.DefaultClient = &http.Client{
					Transport: &fakeRoundTripper{resp: func() (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusOK}, nil
					}},
				}
			},
			wantReady: false,
			wantExtra: func(m *Manager, fs *fakeScalerRegister) {
				assert.Len(t, fs.registered, 1, "scaler")
				assert.Len(t, m.runtimes, 1, "runtime")
			},
		},
		{
			name: "not found",
			// no error
		},
		{
			name: "deleting (pending)",
			sts:  createSts(func(sts *appsv1.StatefulSet) {}),
			rt:   ptr.To(newPendingRuntime()),
			preFn: func(ctx context.Context, m *Manager) {
				var sts appsv1.StatefulSet
				err := m.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &sts)
				assert.NoError(t, err)
				err = m.k8sClient.Delete(ctx, &sts)
				assert.NoError(t, err)
			},
			wantExtra: func(m *Manager, fs *fakeScalerRegister) {
				assert.Empty(t, fs.registered, "scaler")
				assert.Empty(t, m.runtimes, "runtime")
			},
		},
		{
			name: "deleting (ready)",
			sts:  createSts(func(sts *appsv1.StatefulSet) {}),
			rt:   ptr.To(newReadyRuntime("test")),
			preFn: func(ctx context.Context, m *Manager) {
				var sts appsv1.StatefulSet
				err := m.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &sts)
				assert.NoError(t, err)
				err = m.k8sClient.Delete(ctx, &sts)
				assert.NoError(t, err)
			},
			wantExtra: func(m *Manager, fs *fakeScalerRegister) {
				assert.Empty(t, fs.registered, "scaler")
				assert.Empty(t, m.runtimes, "runtime")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var objs []apiruntime.Object
			if test.sts != nil {
				objs = append(objs, test.sts)
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
	deployed map[string]bool
}

func (m *fakeClient) GetAddress(name string) string {
	return fmt.Sprintf("%s:1234", name)
}

func (m *fakeClient) DeployRuntime(ctx context.Context, modelID string) (types.NamespacedName, error) {
	m.deployed[modelID] = true
	return types.NamespacedName{Name: "test", Namespace: "default"}, nil
}

type fakeScalerRegister struct {
	registered map[types.NamespacedName]bool
}

func (m *fakeScalerRegister) Register(modelID string, nn types.NamespacedName) {
	m.registered[nn] = true
}

func (m *fakeScalerRegister) Unregister(nn types.NamespacedName) {
	delete(m.registered, nn)
}
