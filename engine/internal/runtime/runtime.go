package runtime

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// ErrRequestCanceled is returned when the request is canceled.
var ErrRequestCanceled = errors.New("request is canceled")

const (
	defaultBlacklistDuration = 10 * time.Second
)

func newRuntimeAddressSet(logger logr.Logger) *runtimeAddressSet {
	return &runtimeAddressSet{
		addresses:            make(map[string]bool),
		blacklistedAddresses: make(map[string]time.Time),
		blacklistDuration:    defaultBlacklistDuration,
		logger:               logger.WithName("runtimeAddressSet"),
	}
}

type runtimeAddressSet struct {
	// addresses is a set of runtime addresses. Each address is a host and port pair.
	addresses map[string]bool

	// blacklistedAddresses is a set of blacklisted addresses. The value tracks the time when the address
	// was blacklisted.
	blacklistedAddresses map[string]time.Time

	blacklistDuration time.Duration

	nextTarget int

	mu sync.Mutex

	logger logr.Logger
}

func (s *runtimeAddressSet) add(address string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addresses[address] = true
}

func (s *runtimeAddressSet) remove(address string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.addresses, address)
}

func (s *runtimeAddressSet) getAll() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var addrs []string
	for address := range s.addresses {
		addrs = append(addrs, address)
	}
	return addrs
}

func (s *runtimeAddressSet) get(now time.Time) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// First update the blacklist.
	for addr, t := range s.blacklistedAddresses {
		if now.Sub(t) >= s.blacklistDuration {
			// The address is not blacklisted anymore.
			delete(s.blacklistedAddresses, addr)
		}
	}

	s.logger.Info("Get runtime address", "addresses", s.addresses, "blacklistedAddresses", s.blacklistedAddresses)

	if len(s.addresses) == 1 {
		// If there is only one address, return it directly.
		for addr := range s.addresses {
			s.logger.Info("Selected runtime address", "address", addr)
			return addr, true
		}
	}

	// Find addresses that are not blacklisted.
	var addrs []string
	for address := range s.addresses {
		if _, ok := s.blacklistedAddresses[address]; !ok {
			addrs = append(addrs, address)
		}
	}

	// If there are no addresses, return empty string.
	if len(addrs) == 0 {
		return "", false
	}

	// Sort the addresses for consistency.
	sort.Strings(addrs)

	// Pick up th address as a round-robin manner.
	// TODO(kenji): Improve (e.g., perform routing that considers KV cache).

	i := s.nextTarget % len(addrs)

	addr := addrs[i]
	s.nextTarget = (s.nextTarget + 1) % len(addrs)

	s.logger.Info("Selected runtime address", "address", addr)

	return addr, true
}

func (s *runtimeAddressSet) blacklistAddress(addr string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blacklistedAddresses[addr] = now
}

func newPendingRuntime(name string) *runtime {
	return &runtime{
		name:  name,
		ready: false,
	}
}

type runtime struct {
	// name is the name of the statefulset managing the runtime.
	name string

	// isDynamicallyLoadedLoRA is true if the model is dynamically loaded LoRA.
	isDynamicallyLoadedLoRA bool

	ready bool

	// waitChs is used to notify when the runtime becomes ready or the error happens.
	// An error reason is sent when the runtime gets an error
	waitChs []chan string

	lastErrReason string

	// pendingPullModelRequests is the list of model IDs that are queued for pull.
	// The map is keyed by the base model IDs.
	pendingPullModelRequests []*pullModelEvent

	// The following fields are only used when the runtime is ready.

	addrSet *runtimeAddressSet

	// replicas is the number of ready replicas.
	replicas int32
	// gpu is the GPU limit of the runtime.
	gpu int32
}

func (r *runtime) addAddress(address string) {
	r.addrSet.add(address)
}

func (r *runtime) removeAddress(address string) {
	r.addrSet.remove(address)
}

func (r *runtime) addresses() []string {
	if r.addrSet == nil {
		return nil
	}
	return r.addrSet.getAll()
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
	r.lastErrReason = errReason

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
	logger logr.Logger,
) {
	r.ready = true
	r.addrSet = newRuntimeAddressSet(logger)
	r.addrSet.add(address)
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

func getGPUForPod(pod *corev1.Pod) int32 {
	gpu := int32(0)
	for _, con := range pod.Spec.Containers {
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
