package runtime

import (
	"errors"

	appsv1 "k8s.io/api/apps/v1"
)

// ErrRequestCanceled is returned when the request is canceled.
var ErrRequestCanceled = errors.New("request is canceled")

func newPendingRuntime(name string) *runtime {
	return &runtime{
		name:  name,
		ready: false,
	}
}

type runtime struct {
	name string

	// isDynamicallyLoadedLoRA is true if the model is dynamically loaded LoRA.
	isDynamicallyLoadedLoRA bool

	ready bool

	// waitChs is used to notify when the runtime becomes ready or the error happens.
	// An error reason is sent when the runtime gets an error
	waitChs []chan string

	// pendingPullModelRequests is the list of model IDs that are queued for pull.
	// The map is keyed by the base model IDs.
	pendingPullModelRequests []*pullModelEvent

	// The following fields are only used when the runtime is ready.

	// addresses is a set of runtime addresses. Each address is a host and port pair.
	addresses []string
	// replicas is the number of ready replicas.
	replicas int32
	// gpu is the GPU limit of the runtime.
	gpu int32
}

func (r *runtime) addPendingPullModelRequest(e *pullModelEvent) {
	r.pendingPullModelRequests = append(r.pendingPullModelRequests, e)
}

func (r *runtime) dequeuePendingPullModelRequests() []*pullModelEvent {
	reqs := r.pendingPullModelRequests
	r.pendingPullModelRequests = nil
	return reqs
}

func (r *runtime) closeWaitChs(errReason string) {
	for _, ch := range r.waitChs {
		if errReason != "" {
			ch <- errReason
		}
		close(ch)
	}
	r.waitChs = nil

	for _, req := range r.pendingPullModelRequests {
		if errReason != "" {
			req.readyWaitCh <- errReason
		}
		close(req.readyWaitCh)
	}
	r.pendingPullModelRequests = nil
}

func (r *runtime) becomeReady(
	address string,
	gpu,
	replicas int32,
) {
	r.ready = true
	r.addresses = []string{address}
	r.gpu = gpu
	r.replicas = replicas
}

func getGPU(sts *appsv1.StatefulSet) int32 {
	gpu := int32(0)
	for _, con := range sts.Spec.Template.Spec.Containers {
		limit := con.Resources.Limits
		if limit == nil {
			continue
		}

		// TODO(guangrui): Support non-Nvidia GPU.
		v, ok := limit[nvidiaGPUResource]
		if !ok {
			continue
		}
		count, ok := v.AsInt64()
		if !ok {
			continue
		}
		gpu += int32(count)
	}
	return gpu
}
