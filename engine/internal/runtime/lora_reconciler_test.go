package runtime

import (
	"context"
	"testing"

	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLoRAReconciler_Reconcile(t *testing.T) {
	tcs := []struct {
		name          string
		pods          []*corev1.Pod
		req           ctrl.Request
		podsByName    map[string]*podStatus
		wantPodNames  []string
		wantProcessed *loRAAdapterStatusUpdate
	}{
		{
			name: "add pod",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod0",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						PodIP: "ip0",
					},
				},
			},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "pod0",
					Namespace: "default",
				},
			},
			podsByName:    map[string]*podStatus{},
			wantPodNames:  []string{"pod0"},
			wantProcessed: nil,
		},
		{
			name: "add pod of no ip",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod0",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						PodIP: "",
					},
				},
			},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "pod0",
					Namespace: "default",
				},
			},
			podsByName:    map[string]*podStatus{},
			wantPodNames:  []string{},
			wantProcessed: nil,
		},
		{
			name: "delete pod",
			pods: []*corev1.Pod{},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "pod0",
					Namespace: "default",
				},
			},
			podsByName: map[string]*podStatus{
				"pod0": {
					pod: &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod0",
							Namespace: "default",
						},
					},
					lstatus: &loRAAdapterStatus{
						baseModelID: "base0",
						adapterIDs: map[string]struct{}{
							"adapter0": {},
							"adapter1": {},
						},
					},
				},
			},
			wantPodNames: []string{},
			wantProcessed: &loRAAdapterStatusUpdate{
				removedAdapterIDs: []string{"adapter0", "adapter1"},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var objs []apiruntime.Object
			for _, pod := range tc.pods {
				objs = append(objs, pod)
			}

			k8sClient := fake.NewFakeClient(objs...)
			processor := &fakeUpdateProcessor{}
			r := newLoRAReconciler(k8sClient, processor, &fakeLoRAAdapterStatusGetter{})
			r.podsByName = tc.podsByName

			_, err := r.Reconcile(context.Background(), tc.req)
			assert.NoError(t, err)

			var gotPodNames []string
			for name := range r.podsByName {
				gotPodNames = append(gotPodNames, name)
			}
			assert.ElementsMatch(t, tc.wantPodNames, gotPodNames)

			if tc.wantProcessed == nil {
				assert.Len(t, processor.processedUpdates, 0)
				return
			}

			assert.Len(t, processor.processedUpdates, 1)
			gotUpdate := processor.processedUpdates[0]
			assert.ElementsMatch(t, tc.wantProcessed.addedAdapterIDs, gotUpdate.addedAdapterIDs)
			assert.ElementsMatch(t, tc.wantProcessed.removedAdapterIDs, gotUpdate.removedAdapterIDs)
		})
	}
}

func TestLoRAReconciler_Run(t *testing.T) {
	k8sClient := fake.NewFakeClient()
	processor := &fakeUpdateProcessor{}
	lister := &fakeLoRAAdapterStatusGetter{
		s: &loRAAdapterStatus{
			baseModelID: "base0",
			adapterIDs: map[string]struct{}{
				"adapter0": {},
			},
		},
	}

	r := newLoRAReconciler(k8sClient, processor, lister)
	r.podsByName = map[string]*podStatus{
		"pod0": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod0",
					Namespace: "default",
				},
			},
			lstatus: &loRAAdapterStatus{
				baseModelID: "base0",
				adapterIDs: map[string]struct{}{
					"adapter1": {},
				},
			},
		},
	}

	err := r.run(context.Background())
	assert.NoError(t, err)

	assert.Len(t, processor.processedUpdates, 1)
	gotUpdate := processor.processedUpdates[0]
	assert.ElementsMatch(t, []string{"adapter0"}, gotUpdate.addedAdapterIDs)
	assert.ElementsMatch(t, []string{"adapter1"}, gotUpdate.removedAdapterIDs)
}

func TestUpdateLoRALoadingStatus(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod0",
		},
	}

	tcs := []struct {
		name string
		oldS *podStatus
		newS *podStatus
		want *loRAAdapterStatusUpdate
	}{
		{
			name: "new status",
			oldS: &podStatus{
				pod: pod,
			},
			newS: &podStatus{
				pod: pod,
				lstatus: &loRAAdapterStatus{
					baseModelID: "base0",
					adapterIDs: map[string]struct{}{
						"adapter0": {},
						"adapter1": {},
					},
				},
			},
			want: &loRAAdapterStatusUpdate{
				podName:         pod.Name,
				baseModelID:     "base0",
				addedAdapterIDs: []string{"adapter0", "adapter1"},
			},
		},
		{
			name: "new no status",
			oldS: &podStatus{
				pod: pod,
				lstatus: &loRAAdapterStatus{
					baseModelID: "base0",
					adapterIDs: map[string]struct{}{
						"adapter0": {},
						"adapter1": {},
					},
				},
			},
			newS: nil,
			want: &loRAAdapterStatusUpdate{
				podName:           pod.Name,
				baseModelID:       "base0",
				removedAdapterIDs: []string{"adapter0", "adapter1"},
			},
		},
		{
			name: "adapter added and removed",
			oldS: &podStatus{
				pod: pod,
				lstatus: &loRAAdapterStatus{
					baseModelID: "base0",
					adapterIDs: map[string]struct{}{
						"adapter0": {},
						"adapter1": {},
					},
				},
			},
			newS: &podStatus{
				pod: pod,
				lstatus: &loRAAdapterStatus{
					baseModelID: "base0",
					adapterIDs: map[string]struct{}{
						"adapter1": {},
						"adapter2": {},
					},
				},
			},
			want: &loRAAdapterStatusUpdate{
				podName:           pod.Name,
				baseModelID:       "base0",
				addedAdapterIDs:   []string{"adapter2"},
				removedAdapterIDs: []string{"adapter0"},
			},
		},
		{
			name: "no change",
			oldS: &podStatus{
				pod: pod,
				lstatus: &loRAAdapterStatus{
					baseModelID: "base0",
					adapterIDs: map[string]struct{}{
						"adapter0": {},
						"adapter1": {},
					},
				},
			},
			newS: &podStatus{
				pod: pod,
				lstatus: &loRAAdapterStatus{
					baseModelID: "base0",
					adapterIDs: map[string]struct{}{
						"adapter0": {},
						"adapter1": {},
					},
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, gotHasUpdate, err := updateLoRALoadingStatusForPod(tc.oldS, tc.newS, testutil.NewTestLogger(t))
			assert.NoError(t, err)
			if tc.want == nil {
				assert.False(t, gotHasUpdate)
				return
			}
			assert.True(t, gotHasUpdate)
			assert.Equal(t, tc.want.podName, got.podName)
			assert.Equal(t, tc.want.baseModelID, got.baseModelID)
			assert.ElementsMatch(t, tc.want.addedAdapterIDs, got.addedAdapterIDs)
		})
	}
}

type fakeUpdateProcessor struct {
	processedUpdates []*loRAAdapterStatusUpdate
}

func (f *fakeUpdateProcessor) processLoRAAdapterUpdate(ctx context.Context, update *loRAAdapterStatusUpdate) error {
	f.processedUpdates = append(f.processedUpdates, update)
	return nil
}

type fakeLoRAAdapterStatusGetter struct {
	s *loRAAdapterStatus
}

func (f *fakeLoRAAdapterStatusGetter) get(ctx context.Context, addr string) (*loRAAdapterStatus, error) {
	return f.s, nil
}
