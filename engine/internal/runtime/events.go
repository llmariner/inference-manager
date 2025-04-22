package runtime

import (
	"k8s.io/apimachinery/pkg/types"
)

type pullModelEvent struct {
	modelID     string
	readyWaitCh chan string
}

type deleteModelEvent struct {
	modelID     string
	eventWaitCh chan struct{}
}

type reconcileStatefulSetEvent struct {
	namespacedName types.NamespacedName
	eventWaitCh    chan struct{}
}

type readinessCheckEvent struct {
	modelID string

	address  string
	gpu      int32
	replicas int32

	retryCount int
}

type loraAdapterPullStatusCheckEvent struct {
	modelID string

	podIP string
	gpu   int32
}

type loraAdapterStatusUpdateEvent struct {
	update *loRAAdapterStatusUpdate

	eventWaitCh chan struct{}
}
