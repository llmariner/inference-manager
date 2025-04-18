package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
				pod:             pod,
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
				pod:               pod,
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
				pod:               pod,
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
			got, gotHasUpdate, err := updateLoRALoadingStatusForPod(context.Background(), tc.oldS, tc.newS)
			assert.NoError(t, err)
			if tc.want == nil {
				assert.False(t, gotHasUpdate)
				return
			}
			assert.True(t, gotHasUpdate)
			assert.Equal(t, tc.want.pod, got.pod)
			assert.Equal(t, tc.want.baseModelID, got.baseModelID)
			assert.ElementsMatch(t, tc.want.addedAdapterIDs, got.addedAdapterIDs)
		})
	}

}
