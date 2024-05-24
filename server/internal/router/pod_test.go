package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	apiv1 "k8s.io/api/core/v1"
)

func TestProcessAdd(t *testing.T) {
	errCh := make(chan error)
	m := newRouteMap()
	h := newPodEventHandler(m, errCh)

	pod := &apiv1.Pod{
		Status: apiv1.PodStatus{
			PodIP: "",
		},
	}
	h.ProcessAdd(context.Background(), pod)
	assert.Len(t, m.engines, 0)

	pod = &apiv1.Pod{
		Status: apiv1.PodStatus{
			PodIP: "1.2.3.4",
		},
	}
	h.ProcessAdd(context.Background(), pod)
	assert.Len(t, m.engines, 1)
	_, ok := m.engines["1.2.3.4"]
	assert.True(t, ok)
}

func TestProcessUpdate(t *testing.T) {
	errCh := make(chan error)
	m := newRouteMap()
	h := newPodEventHandler(m, errCh)

	pod1 := &apiv1.Pod{
		Status: apiv1.PodStatus{
			PodIP: "",
		},
	}
	pod2 := &apiv1.Pod{
		Status: apiv1.PodStatus{
			PodIP: "1.2.3.4",
		},
	}
	h.ProcessUpdate(context.Background(), pod1, pod2)
	assert.Len(t, m.engines, 1)
	_, ok := m.engines["1.2.3.4"]
	assert.True(t, ok)
}

func TestProcessDelete(t *testing.T) {
	errCh := make(chan error)
	m := newRouteMap()
	h := newPodEventHandler(m, errCh)

	pod := &apiv1.Pod{
		Status: apiv1.PodStatus{
			PodIP: "1.2.3.4",
		},
	}
	h.ProcessAdd(context.Background(), pod)
	assert.Len(t, m.engines, 1)
	_, ok := m.engines["1.2.3.4"]
	assert.True(t, ok)

	h.ProcessDelete(context.Background(), pod)
	assert.Len(t, m.engines, 0)
}
