package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/llmariner/inference-manager/engine/internal/config"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDeleteRuntime(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]*runtime{
			"model-0": newPendingRuntime("rt-model-0", &mv1.Model{Id: "model-0"}),
			"model-1": newReadyRuntime("rt-model-1", "test", 1),
		},
	}
	mgr.deleteRuntimeByName("rt-model-0")
	mgr.deleteRuntimeByName("rt-model-1")
	assert.Empty(t, mgr.runtimes)
}

func TestGetLLMAddress(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]*runtime{
			"model-0": newReadyRuntime("rt-model-0", "test", 1),
			"model-1": newPendingRuntime("rt-model-1", &mv1.Model{Id: "model-1"}),
		},
	}
	addr, err := mgr.GetLLMAddress("model-0")
	assert.NoError(t, err)
	assert.Equal(t, "test", addr)
	_, err = mgr.GetLLMAddress("model-1")
	assert.Error(t, err)
}

func TestListModels(t *testing.T) {
	mgr := &Manager{
		runtimes: map[string]*runtime{
			"model-0": newReadyRuntime("rt-model-0", "test", 1),
			"model-1": newPendingRuntime("rt-model-1", &mv1.Model{Id: "model-1"}),
			"model-2": newReadyRuntime("rt-model-2", "test2", 2),
		},
		podMonitor: &fakePodMonitor{},
	}
	models := mgr.ListModels()
	assert.Len(t, models, 3)
}

func TestPullModel(t *testing.T) {
	const (
		testModelID = "mid-0"
		namespace   = "rt-0"
	)

	var tests = []struct {
		name          string
		rt            *runtime
		sts           *appsv1.StatefulSet
		deployed      bool
		readyReplicas int32
		canceled      bool
	}{
		{
			name: "already ready",
			rt:   newReadyRuntime("rt-model-0", "test", 1),
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rt-model-0",
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas: 1,
				},
			},
		},
		{
			name: "already pending",
			rt:   newPendingRuntime("rt-model-1", &mv1.Model{Id: "model-1"}),
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
			var objs []apiruntime.Object
			if test.sts != nil {
				objs = append(objs, test.sts)
			}
			k8sClient := fake.NewFakeClient(objs...)

			rtClient := &fakeClient{
				deployed:      map[string]bool{},
				readyReplicas: test.readyReplicas,
				k8sClient:     k8sClient,
				namespace:     namespace,
			}
			scaler := &fakeScalerRegister{registered: map[types.NamespacedName]bool{}}
			mgr := NewManager(
				k8sClient,
				&fakeClientFactory{c: rtClient},
				scaler,
				&fakeModelClient{
					models: map[string]*mv1.Model{
						testModelID: {Id: testModelID},
					},
				},
				nil,
				false,
				-1,
				make(map[string]bool),
			)
			if test.rt != nil {
				mgr.runtimes[testModelID] = test.rt
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
						mgr.mu.Lock()
						rt, ok := mgr.runtimes[testModelID]
						if ok {
							rt.closeWaitChs("error")
						}
						mgr.mu.Unlock()
					} else {
						t.Log("marking runtime ready")
						mgr.mu.Lock()
						rt, ok := mgr.runtimes[testModelID]
						if ok {
							rt.becomeReady("test", 1, 1, testutil.NewTestLogger(t))
							rt.closeWaitChs("")
						}
					}
				}
			}()

			go func() {
				if err := mgr.RunStateMachine(ctx); err != nil {
					assert.ErrorIs(t, err, context.Canceled)
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

func TestPullModel_DynamicLoRALoading(t *testing.T) {
	const (
		baseModelID   = "base-mid-0"
		baseModelName = "rt-base-mid-0"

		fineTunedModelID   = "fine-tuned-mid-0"
		fineTunedModelName = "rt-fine-tuned-mid-0"

		namespace = "rt-0"
	)

	var tests = []struct {
		name             string
		baseRT           *runtime
		baseSTS          *appsv1.StatefulSet
		targetExistsResp bool
		wantErr          bool
	}{
		{
			name:             "no base model",
			targetExistsResp: true,
		},
		{
			name: "base model ready",
			baseRT: &runtime{
				ready: true,
				name:  baseModelName,
			},
			baseSTS: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      baseModelName,
					Namespace: namespace,
					Annotations: map[string]string{
						modelAnnotationKey: baseModelID,
					},
				},
			},
			targetExistsResp: true,
		},
		{
			name: "base model pending",
			baseRT: &runtime{
				ready: false,
				name:  baseModelName,
			},
			baseSTS: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      baseModelName,
					Namespace: namespace,
					Annotations: map[string]string{
						modelAnnotationKey: baseModelID,
					},
				},
			},
			targetExistsResp: true,
		},
		{
			name:             "no target",
			targetExistsResp: false,
			wantErr:          true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var objs []apiruntime.Object
			if test.baseSTS != nil {
				objs = append(objs, test.baseSTS)
			}
			k8sClient := fake.NewFakeClient(objs...)

			rtClient := &fakeClient{
				deployed:      map[string]bool{},
				readyReplicas: 1,
				k8sClient:     k8sClient,
				namespace:     namespace,
				runtimeName:   config.RuntimeNameVLLM,
			}
			scaler := &fakeScalerRegister{registered: map[types.NamespacedName]bool{}}
			mgr := NewManager(
				k8sClient,
				&fakeClientFactory{c: rtClient},
				scaler,
				&fakeModelClient{
					models: map[string]*mv1.Model{
						baseModelID: {
							Id:          baseModelID,
							IsBaseModel: true,
						},
						fineTunedModelID: {
							Id:          fineTunedModelID,
							IsBaseModel: false,
							BaseModelId: baseModelID,
						},
					},
				},
				nil,
				true,
				9090,
				make(map[string]bool),
			)
			loader := &fakeLoraAdapterLoader{
				pulled:   map[string]bool{},
				loaded:   map[string]bool{},
				unloaded: map[string]bool{},
			}
			mgr.loraAdapterLoader = loader
			mgr.runtimeReadinessChecker = &fakeRuntimeReadinessChecker{
				err: nil,
			}
			mgr.loraAdapterLoadingTargetSelector = &fakeLoRAAdapterLoadingTargetSelector{
				pod: &corev1.Pod{
					Status: corev1.PodStatus{
						PodIP: "fake-pod-ip",
					},
				},
				targetExistsResp: test.targetExistsResp,
			}

			if test.baseRT != nil {
				mgr.runtimes[baseModelID] = test.baseRT
			}
			ctx, cancel := context.WithTimeout(testutil.ContextWithLogger(t), 2*time.Second)
			defer cancel()

			go func() {
				if err := mgr.RunStateMachine(ctx); err != nil {
					assert.ErrorIs(t, err, context.Canceled)
				}
			}()

			go func() {
				rt := test.baseRT
				if rt == nil || rt.ready {
					return
				}

				// Wait until the pull request for the fine-tuned model is queued.
				assert.Eventually(t, func() bool {
					mgr.mu.Lock()
					defer mgr.mu.Unlock()
					return len(rt.pendingPullModelRequests) > 0
				}, time.Second, 100*time.Millisecond)

				mgr.mu.Lock()
				defer mgr.mu.Unlock()
				mgr.eventCh <- &readinessCheckEvent{
					modelID:     baseModelID,
					eventWaitCh: make(chan error),
				}
			}()

			err := mgr.PullModel(ctx, fineTunedModelID)
			if test.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			assert.True(t, loader.loaded[fineTunedModelID])
		})
	}
}

func TestDeleteModel(t *testing.T) {
	const (
		name        = "rt-mid-0"
		namespace   = "ns-0"
		testModelID = "mid-0"
	)

	var tests = []struct {
		name         string
		rt           *runtime
		sts          *appsv1.StatefulSet
		runReconcile bool
		wantExtra    func(t *testing.T, c *fakeClient, l *fakeLoraAdapterLoader)
	}{
		{
			name: "no runtime",
		},
		{
			name: "existing runtime",
			rt:   newReadyRuntime(name, "test", 1),
			sts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			},
			runReconcile: true,
			wantExtra: func(t *testing.T, c *fakeClient, l *fakeLoraAdapterLoader) {
				assert.False(t, c.deployed[testModelID])
			},
		},
		{
			name: "existing runtime with dynamic LoRA loading",
			rt: &runtime{
				ready:                   true,
				name:                    name,
				isDynamicallyLoadedLoRA: true,
				addrSet: &runtimeAddressSet{
					addresses: map[string]bool{"test": true},
				},
			},
			wantExtra: func(t *testing.T, c *fakeClient, l *fakeLoraAdapterLoader) {
				assert.True(t, l.unloaded[testModelID])
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

			deployed := map[string]bool{}
			if test.rt != nil {
				deployed[testModelID] = true
			}

			rtClient := &fakeClient{
				deployed:  deployed,
				k8sClient: k8sClient,
				namespace: namespace,
			}
			scaler := &fakeScalerRegister{registered: map[types.NamespacedName]bool{}}
			mgr := NewManager(
				k8sClient,
				&fakeClientFactory{c: rtClient},
				scaler,
				nil,
				nil,
				false,
				-1,
				make(map[string]bool),
			)
			loader := &fakeLoraAdapterLoader{
				pulled:   map[string]bool{},
				loaded:   map[string]bool{},
				unloaded: map[string]bool{},
			}
			mgr.loraAdapterLoader = loader

			if test.rt != nil {
				mgr.runtimes[testModelID] = test.rt
			}
			ctx, cancel := context.WithTimeout(testutil.ContextWithLogger(t), 2*time.Second)
			defer cancel()

			go func() {
				if err := mgr.RunStateMachine(ctx); err != nil {
					assert.ErrorIs(t, err, context.Canceled)
				}
			}()

			err := mgr.DeleteModel(ctx, testModelID)
			assert.NoError(t, err)

			// Run reconciliation to mimic the stateful deletion event.
			if test.runReconcile {
				if test.sts != nil {
					_, err := mgr.Reconcile(ctx, ctrl.Request{
						NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
					})
					assert.NoError(t, err)
				}
			}

			mgr.mu.Lock()
			_, ok := mgr.runtimes[testModelID]
			assert.False(t, ok)
			mgr.mu.Unlock()

			if f := test.wantExtra; f != nil {
				f(t, rtClient, loader)
			}
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

		sts *appsv1.StatefulSet
		pod *corev1.Pod
		rt  *runtime

		readinessCheck error

		readinessCheckMaxRetryCount int

		wantReady     bool
		wantChClose   bool
		wantErrReason string
		wantExtra     func(t *testing.T, m *Manager, fs *fakeScalerRegister)
	}{
		{
			name: "still pending",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 1
			}),
			rt: newPendingRuntime(name, &mv1.Model{Id: name}),
		},
		{
			name: "unreachable",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.ReadyReplicas = 1
				sts.Status.Replicas = 1
			}),
			rt: newPendingRuntime(name, &mv1.Model{Id: name}),

			readinessCheck: errors.New("runtime not reachable"),

			// No retry
			readinessCheckMaxRetryCount: 0,

			wantChClose:   true,
			wantErrReason: errMsgUnreachableRuntime,
		},
		{
			name: "transient unreachable",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.ReadyReplicas = 1
				sts.Status.Replicas = 1
			}),
			rt: newPendingRuntime(name, &mv1.Model{Id: name}),

			readinessCheck: errors.New("runtime not reachable"),

			// Enable retry. Readiness check should pass after an initial failure.
			readinessCheckMaxRetryCount: 1,
			wantChClose:                 true,
			wantErrReason:               errMsgUnreachableRuntime,
		},
		{
			name: "to be ready",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.ReadyReplicas = 1
				sts.Status.Replicas = 1
			}),
			rt: newPendingRuntime(name, &mv1.Model{Id: name}),

			wantReady:   true,
			wantChClose: true,
		},
		{
			name: "not-registered (ready)",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.ReadyReplicas = 1
				sts.Status.Replicas = 1
			}),

			wantReady: true,
			wantExtra: func(t *testing.T, m *Manager, fs *fakeScalerRegister) {
				assert.Len(t, fs.registered, 1, "scaler")
				assert.Len(t, m.runtimes, 1, "runtime")
			},
		},
		{
			name: "to be pending",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 0
			}),
			rt: newReadyRuntime(name, "test", 1),
		},
		{
			name: "not-registered (pending)",
			sts: createSts(func(sts *appsv1.StatefulSet) {
				sts.Status.Replicas = 1
			}),
			wantReady: false,
			wantExtra: func(t *testing.T, m *Manager, fs *fakeScalerRegister) {
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
			wantExtra: func(t *testing.T, m *Manager, fs *fakeScalerRegister) {
				assert.Len(t, fs.registered, 1, "scaler")
				assert.Len(t, m.runtimes, 1, "runtime")
			},
		},
		{
			name: "not found (pending)",
			sts:  nil,
			rt:   newPendingRuntime(name, &mv1.Model{Id: name}),
			wantExtra: func(t *testing.T, m *Manager, fs *fakeScalerRegister) {
				assert.Empty(t, fs.registered, "scaler")
				assert.Empty(t, m.runtimes, "runtime")
			},
		},
		{
			name: "not found (ready)",
			sts:  nil,
			rt:   newReadyRuntime(name, "test", 1),
			wantExtra: func(t *testing.T, m *Manager, fs *fakeScalerRegister) {
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
			rt:            newPendingRuntime(name, &mv1.Model{Id: name}),
			wantChClose:   true,
			wantErrReason: corev1.PodReasonUnschedulable,
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
			rtClient := &fakeClient{
				deployed:  map[string]bool{},
				namespace: namespace,
			}
			scaler := &fakeScalerRegister{registered: map[types.NamespacedName]bool{}}
			mgr := NewManager(
				k8sClient,
				&fakeClientFactory{c: rtClient},
				scaler,
				&fakeModelClient{
					models: map[string]*mv1.Model{
						modelID: {Id: modelID},
					},
				},
				nil,
				false,
				-1,
				make(map[string]bool),
			)
			mgr.readinessCheckMaxRetryCount = test.readinessCheckMaxRetryCount
			mgr.runtimeReadinessChecker = &fakeRuntimeReadinessChecker{
				err: test.readinessCheck,
			}
			if test.rt != nil {
				mgr.runtimes[modelID] = test.rt
			}

			ctx := testutil.ContextWithLogger(t)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			go func() {
				if err := mgr.RunStateMachine(ctx); err != nil {
					assert.ErrorIs(t, err, context.Canceled)
				}
			}()

			var (
				chClosed     atomic.Bool
				gotErrReason atomic.Value
			)
			if test.wantChClose {
				waitCh := make(chan string)
				test.rt.waitChs = append(test.rt.waitChs, waitCh)
				go func() {
					for {
						select {
						case <-ctx.Done():
							return
						case errReason := <-waitCh:
							if errReason != "" {
								gotErrReason.Store(errReason)
							} else {
								chClosed.Store(true)
								return
							}
						}
					}
				}()
			}

			nn := types.NamespacedName{Name: name, Namespace: namespace}
			_, err := mgr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
			assert.NoError(t, err)

			if test.wantChClose {
				assert.Eventually(t, func() bool {
					return chClosed.Load()
				}, time.Second, 100*time.Millisecond)
			}
			if w, g := test.wantErrReason, gotErrReason.Load(); w != "" {
				assert.Equal(t, w, g)
			} else {
				assert.Nil(t, g)
			}

			assert.Eventually(t, func() bool {
				mgr.mu.Lock()
				defer mgr.mu.Unlock()
				rt := mgr.runtimes[modelID]
				return test.wantReady == (rt != nil && rt.ready)
			}, time.Second, 100*time.Millisecond)

			mgr.mu.Lock()
			defer mgr.mu.Unlock()

			if test.wantExtra != nil {
				test.wantExtra(t, mgr, scaler)
			}
		})
	}
}

func TestLoRAAdapterStatusUpdateEvent(t *testing.T) {
	const (
		modelID   = "fine-tuned-mid-0"
		modelName = "rt-fine-tuned-mid-0"

		podName   = "pod-0"
		podIP     = "pod-0-ip"
		podAddr   = "pod-0-ip:1234"
		namespace = "rt-0"
	)

	var tests = []struct {
		name      string
		rt        *runtime
		update    *loRAAdapterStatusUpdate
		isReady   bool
		wantAddrs []string
	}{
		{
			name: "add to existing runtime",
			update: &loRAAdapterStatusUpdate{
				podName:         podName,
				podIP:           podIP,
				gpu:             1,
				addedAdapterIDs: []string{modelID},
			},
			rt: &runtime{
				ready:                   true,
				isDynamicallyLoadedLoRA: true,
				name:                    modelName,
				addrSet: &runtimeAddressSet{
					addresses: map[string]bool{"other_addr": true},
				},
			},
			isReady:   true,
			wantAddrs: []string{"other_addr", podAddr},
		},
		{
			name: "create a new runtime",
			update: &loRAAdapterStatusUpdate{
				podName:         podName,
				podIP:           podIP,
				gpu:             1,
				addedAdapterIDs: []string{modelID},
			},

			isReady:   true,
			wantAddrs: []string{podAddr},
		},
		{
			name: "remove addr and delete runtime",
			update: &loRAAdapterStatusUpdate{
				podName:           podName,
				podIP:             podIP,
				gpu:               1,
				removedAdapterIDs: []string{modelID},
			},
			rt: &runtime{
				ready:                   true,
				isDynamicallyLoadedLoRA: true,
				name:                    modelName,
				addrSet: &runtimeAddressSet{
					addresses: map[string]bool{podAddr: true},
				},
			},
			isReady:   false,
			wantAddrs: []string{},
		},
		{
			name: "remove addr but keep runtime",
			update: &loRAAdapterStatusUpdate{
				podName:           podName,
				podIP:             podIP,
				gpu:               1,
				removedAdapterIDs: []string{modelID},
			},
			rt: &runtime{
				ready:                   true,
				isDynamicallyLoadedLoRA: true,
				name:                    modelName,
				addrSet: &runtimeAddressSet{
					addresses: map[string]bool{"other_addr": true},
				},
			},
			isReady:   true,
			wantAddrs: []string{"other_addr"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var objs []apiruntime.Object
			k8sClient := fake.NewFakeClient(objs...)

			rtClient := &fakeClient{
				deployed: map[string]bool{},
			}
			scaler := &fakeScalerRegister{registered: map[types.NamespacedName]bool{}}
			mgr := NewManager(
				k8sClient,
				&fakeClientFactory{c: rtClient},
				scaler,
				&fakeModelClient{
					models: map[string]*mv1.Model{
						"base-mid-0": {Id: "base-mid-0", IsBaseModel: true},
						modelID:      {Id: modelID, BaseModelId: "base-mid-0"},
					},
				},
				nil,
				true,
				9090,
				make(map[string]bool),
			)
			if test.rt != nil {
				mgr.runtimes[modelID] = test.rt
			}
			ctx, cancel := context.WithTimeout(testutil.ContextWithLogger(t), 2*time.Second)
			defer cancel()

			go func() {
				if err := mgr.RunStateMachine(ctx); err != nil {
					assert.ErrorIs(t, err, context.Canceled)
				}
			}()

			err := mgr.processLoRAAdapterUpdate(ctx, test.update)
			assert.NoError(t, err)

			mgr.mu.Lock()
			defer mgr.mu.Unlock()

			rt := mgr.runtimes[modelID]
			assert.Equal(t, test.isReady, rt != nil && rt.ready)
			if rt != nil {
				assert.True(t, rt.isDynamicallyLoadedLoRA)
			}
			if test.isReady {
				assert.ElementsMatch(t, test.wantAddrs, rt.addresses())
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

type fakeClientFactory struct {
	c *fakeClient
}

func (f *fakeClientFactory) New(modelID string) (Client, error) {
	return f.c, nil
}

type fakeClient struct {
	deployed      map[string]bool
	readyReplicas int32
	k8sClient     client.Client
	namespace     string
	runtimeName   string
}

func (c *fakeClient) GetName(modelID string) string {
	return fmt.Sprintf("rt-%s", modelID)
}

func (c *fakeClient) GetAddress(name string) string {
	return fmt.Sprintf("%s:1234", name)
}

func (c *fakeClient) DeployRuntime(ctx context.Context, model *mv1.Model, update bool) (*appsv1.StatefulSet, error) {
	c.deployed[model.Id] = true
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.GetName(model.Id),
			Namespace: c.namespace,
			Annotations: map[string]string{
				modelAnnotationKey: model.Id,
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: c.readyReplicas,
		},
	}
	if c.k8sClient != nil {
		if err := c.k8sClient.Create(ctx, sts); err != nil {
			return nil, err
		}
	}
	return sts, nil
}

func (c *fakeClient) DeleteRuntime(ctx context.Context, name, modelID string) error {
	c.deployed[modelID] = false
	if c.k8sClient != nil {
		if err := c.k8sClient.Delete(ctx, &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: c.namespace,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *fakeClient) Namespace() string {
	return c.namespace
}

func (c *fakeClient) RuntimeName() string {
	return c.runtimeName
}

type fakeScalerRegister struct {
	registered map[types.NamespacedName]bool
}

func (r *fakeScalerRegister) Register(ctx context.Context, modelID string, target *appsv1.StatefulSet) error {
	nn := types.NamespacedName{Name: target.Name, Namespace: target.Namespace}
	r.registered[nn] = true
	return nil
}

func (r *fakeScalerRegister) Unregister(nn types.NamespacedName) {
	delete(r.registered, nn)
}

type fakeRuntimeReadinessChecker struct {
	err error
}

func (c *fakeRuntimeReadinessChecker) check(addr string) error {
	// Return an error at most once.
	err := c.err
	return err
}

type fakeLoraAdapterLoader struct {
	pulled   map[string]bool
	loaded   map[string]bool
	unloaded map[string]bool
}

func (l *fakeLoraAdapterLoader) pullModel(ctx context.Context, pullerAddr, modelID string) error {
	l.pulled[modelID] = true
	return nil
}

func (l *fakeLoraAdapterLoader) checkModelPullStatus(ctx context.Context, pullerAddr, modelID string) (bool, error) {
	_, ok := l.pulled[modelID]
	return ok, nil
}

func (l *fakeLoraAdapterLoader) load(ctx context.Context, vllmAddr, modelID string) error {
	l.loaded[modelID] = true
	return nil
}

func (l *fakeLoraAdapterLoader) unload(ctx context.Context, vllmAddr, modelID string) error {
	l.unloaded[modelID] = true
	return nil
}

type fakeLoRAAdapterLoadingTargetSelector struct {
	pod              *corev1.Pod
	targetExistsResp bool
}

func (s *fakeLoRAAdapterLoadingTargetSelector) selectTarget(ctx context.Context, modelID, stsName string) (*corev1.Pod, error) {
	if s.pod == nil {
		return nil, errors.New("no pod")
	}
	return s.pod, nil
}

func (s *fakeLoRAAdapterLoadingTargetSelector) targetExists(ctx context.Context, modelID string, pod *corev1.Pod) (bool, error) {
	return s.targetExistsResp, nil
}

func newReadyRuntime(name, addr string, replicas int32) *runtime {
	return &runtime{
		ready: true,
		name:  name,
		addrSet: &runtimeAddressSet{
			addresses: map[string]bool{addr: true},
		},
		replicas: replicas,
	}
}

type fakePodMonitor struct {
}

func (f *fakePodMonitor) modelStatus(id string) *modelStatus {
	return &modelStatus{}
}
