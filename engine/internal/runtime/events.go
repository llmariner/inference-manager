package runtime

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type pullModelEvent struct {
	modelID     string
	readyWaitCh chan string
}

type deleteModelEvent struct {
	modelID     string
	eventWaitCh chan error
}

type updateModelEvent struct {
	modelID     string
	eventWaitCh chan error
}

type reconcileStatefulSetEvent struct {
	namespacedName types.NamespacedName
	eventWaitCh    chan error
}

type readinessCheckEvent struct {
	modelID string

	address  string
	gpu      int32
	replicas int32

	// pod is set when the readiness check is for a specific pod (for LoRA loading).
	pod *corev1.Pod

	retryCount int

	eventWaitCh chan error
}

type loraAdapterPullStatusCheckEvent struct {
	modelID string

	pod *corev1.Pod
	gpu int32

	eventWaitCh chan error
}

type loraAdapterStatusUpdateEvent struct {
	update *loRAAdapterStatusUpdate

	eventWaitCh chan error
}

type loadLoRAAdapterEvent struct {
	modelID string
	pod     *corev1.Pod

	eventWaitCh chan error
}
