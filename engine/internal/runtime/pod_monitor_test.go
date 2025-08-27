package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fakek "k8s.io/client-go/kubernetes/fake"
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
	pm := NewPodMonitor(k8sClient, fakek.NewSimpleClientset())

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

func TestExtractErrMsg(t *testing.T) {
	tcs := []struct {
		errMsg string
		want   string
	}{
		{
			errMsg: `...
log0
log1
log2

`,
			want: "log2",
		},
		{
			errMsg: `...
ERROR 08-26 10:58:20 [core.py:489]   File "/usr/local/lib/python3.12/dist-packages/vllm/v1/core/kv_cache_utils.py", line 532, in check_enough_kv_cache_memory
ERROR 08-26 10:58:20 [core.py:489]     raise ValueError("No available memory for the cache blocks. "
ERROR 08-26 10:58:20 [core.py:489] ValueError: No available memory for the cache blocks. Try increasing gpu_memory_utilization when initializing the engine.
Traceback (most recent call last):
  File "/usr/lib/python3.12/multiprocessing/process.py", line 314, in _bootstrap
...
  File "/usr/local/lib/python3.12/dist-packages/vllm/v1/engine/core_client.py", line 418, in __init__
    self._wait_for_engine_startup(output_address, parallel_config)
  File "/usr/local/lib/python3.12/dist-packages/vllm/v1/engine/core_client.py", line 484, in _wait_for_engine_startup
    raise RuntimeError("Engine core initialization failed. "
RuntimeError: Engine core initialization failed. See root cause above. Failed core proc(s): {}
`,
			want: "ERROR 08-26 10:58:20 [core.py:489] ValueError: No available memory for the cache blocks. Try increasing gpu_memory_utilization when initializing the engine.",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.errMsg, func(t *testing.T) {
			got := extractErrMsg(strings.Split(tc.errMsg, "\n"))
			assert.Equal(t, tc.want, got)
		})
	}
}
