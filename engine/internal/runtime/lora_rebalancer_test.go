package runtime

import (
	"context"
	"fmt"
	"testing"

	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRebalance(t *testing.T) {
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod0",
				Namespace: "default",
			},
			Status: corev1.PodStatus{
				PodIP: "ip0",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
			},
			Status: corev1.PodStatus{
				PodIP: "ip1",
			},
		},
	}

	statuses := []*loRAAdapterStatus{
		{
			baseModelID: "base0",
			adapterIDs: map[string]struct{}{
				"adapter0": {},
			},
		},
		{
			baseModelID: "base0",
			adapterIDs:  map[string]struct{}{},
		},
	}

	loader := &fakeLoRAAdapterPullAndLoader{}

	r := newLoRARebalancer(
		fake.NewFakeClient(),
		loader,
		&fakeLoRAAdapterStatusGetter{},
	)
	r.logger = testutil.NewTestLogger(t)
	for _, pod := range pods {
		r.podsByName[pod.Name] = pod
	}

	podStatuses := []*podStatus{
		{
			pod:     pods[0],
			lstatus: statuses[0],
		},
		{
			pod:     pods[1],
			lstatus: statuses[1],
		},
	}
	r.rebalance(context.Background(), podStatuses)

	assert.ElementsMatch(t, []string{"adapter0"}, loader.pulledModelIDs)
	var ips []string
	for _, pod := range loader.pulledPods {
		ips = append(ips, pod.Status.PodIP)
	}
	assert.ElementsMatch(t, []string{"ip1"}, ips)
}

func TestBuildStatusMap(t *testing.T) {
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod0",
				Namespace: "default",
			},
			Status: corev1.PodStatus{
				PodIP: "ip0",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
			},
			Status: corev1.PodStatus{
				PodIP: "ip1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "default",
			},
			Status: corev1.PodStatus{
				PodIP: "ip2",
			},
		},
	}

	statuses := []*loRAAdapterStatus{
		{
			baseModelID: "base0",
			adapterIDs: map[string]struct{}{
				"adapter0": {},
			},
		},
		{
			baseModelID: "base0",
			adapterIDs: map[string]struct{}{
				"adapter1": {},
			},
		},
		{
			baseModelID: "base2",
			adapterIDs: map[string]struct{}{
				"adapter2": {},
			},
		},
	}

	statusGetter := &fakeLoRAAdapterStatusGetter{
		statuses: map[string]*loRAAdapterStatus{
			fmt.Sprintf("ip0:%d", vllmHTTPPort): statuses[0],
			fmt.Sprintf("ip1:%d", vllmHTTPPort): statuses[1],
			fmt.Sprintf("ip2:%d", vllmHTTPPort): statuses[2],
		},
	}
	r := newLoRARebalancer(
		fake.NewFakeClient(),
		nil,
		statusGetter,
	)
	r.logger = testutil.NewTestLogger(t)
	for _, pod := range pods {
		r.podsByName[pod.Name] = pod
	}

	got, err := r.buildStatusMap(context.Background())
	assert.NoError(t, err)

	want := map[string][]*podStatus{
		"base0": {
			{
				pod:     pods[0],
				lstatus: statuses[0],
			},
			{
				pod:     pods[1],
				lstatus: statuses[1],
			},
		},
		"base2": {
			{
				pod:     pods[2],
				lstatus: statuses[2],
			},
		},
	}
	assert.Len(t, got, len(want))
	for k, g := range got {
		w, ok := want[k]
		assert.True(t, ok)
		assert.ElementsMatch(t, g, w)
	}
}

type fakeLoRAAdapterPullAndLoader struct {
	pulledModelIDs []string
	pulledPods     []*corev1.Pod
}

func (f *fakeLoRAAdapterPullAndLoader) loadLoRAAdapter(ctx context.Context, modelID string, pod *corev1.Pod) error {
	f.pulledModelIDs = append(f.pulledModelIDs, modelID)
	f.pulledPods = append(f.pulledPods, pod)
	return nil
}
