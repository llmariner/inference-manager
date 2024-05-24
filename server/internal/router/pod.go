package router

import (
	"context"
	"fmt"
	"log"

	"github.com/llm-operator/inference-manager/server/internal/k8s"
	apiv1 "k8s.io/api/core/v1"
)

// podEventHandler is the event handler for managing pod.
type podEventHandler struct {
	m     *routeMap
	errCh chan error
}

var _ k8s.EventHandlerWithContext = &podEventHandler{}

// newPodEventHandler creates a new PodEventHandler.
func newPodEventHandler(
	m *routeMap,
	errCh chan error,
) *podEventHandler {
	return &podEventHandler{
		m:     m,
		errCh: errCh,
	}
}

// ProcessAdd processed the add event.
func (h *podEventHandler) ProcessAdd(ctx context.Context, obj interface{}) {
	pod, ok := obj.(*apiv1.Pod)
	if !ok {
		h.errCh <- fmt.Errorf("cast runtime.Object type")
		return
	}
	if pod.Status.PodIP != "" {
		log.Printf("Registering a pod (IP: %s) in the routing table\n", pod.Status.PodIP)
		h.m.addServer(pod.Status.PodIP)
	}
}

// ProcessUpdate processes the update event.
func (h *podEventHandler) ProcessUpdate(ctx context.Context, oldObj, newObj interface{}) {
	newPod, ok := newObj.(*apiv1.Pod)
	if !ok {
		h.errCh <- fmt.Errorf("cast runtime.Object type")
		return
	}
	oldPod, ok := oldObj.(*apiv1.Pod)
	if !ok {
		h.errCh <- fmt.Errorf("cast runtime.Object type")
		return
	}
	if newPod.Status.PodIP == oldPod.Status.PodIP {
		return
	}
	if oldPod.Status.PodIP != "" {
		h.errCh <- fmt.Errorf("unexpected ip change")
		return
	}
	log.Printf("Registering a pod (IP: %s) in the routing table\n", newPod.Status.PodIP)
	h.m.addServer(newPod.Status.PodIP)
}

// ProcessDelete processes the delete event.
func (h *podEventHandler) ProcessDelete(ctx context.Context, obj interface{}) {
	pod, ok := obj.(*apiv1.Pod)
	if !ok {
		h.errCh <- fmt.Errorf("cast runtime.Object type")
		return
	}
	if pod.Status.PodIP == "" {
		return
	}
	log.Printf("Removing a pod (IP: %s) from the routing table\n", pod.Status.PodIP)
	h.m.deleteServer(pod.Status.PodIP)
}
