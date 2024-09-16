package autoscaler

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/llm-operator/inference-manager/engine/internal/config"
	"github.com/llm-operator/inference-manager/engine/internal/metrics"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewMultiAutoscaler creates a new MultiAutoscaler.
func NewMultiAutoscaler(
	k8sClient client.Client,
	metricsProvider metrics.Provider,
	config config.AutoscalerConfig,
) *MultiAutoscaler {
	return &MultiAutoscaler{
		k8sClient: k8sClient,
		metrics:   metricsProvider,
		config:    config,
		scalers:   make(map[types.NamespacedName]scaler),
		startCh:   make(chan struct{}),
		stopCh:    make(chan struct{}),
	}
}

// MultiAutoscaler is a controller that manages multiple scalers.
type MultiAutoscaler struct {
	logger logr.Logger

	k8sClient client.Client
	metrics   metrics.Provider

	config config.AutoscalerConfig

	scalers map[types.NamespacedName]scaler
	mu      sync.Mutex

	startCh chan struct{}
	stopCh  chan struct{}
}

// SetupWithManager sets up the multi-autoscaler with the Manager.
func (m *MultiAutoscaler) SetupWithManager(mgr ctrl.Manager) error {
	m.logger = mgr.GetLogger().WithName("multiscaler")
	return mgr.Add(m)
}

// Start starts the multi-autoscaler.
func (m *MultiAutoscaler) Start(ctx context.Context) error {
	m.logger.Info("Starting multi-autoscaler")
	close(m.startCh)
	<-ctx.Done()
	close(m.stopCh)
	m.logger.Info("Stopped multi-autoscaler")
	return nil
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (m *MultiAutoscaler) NeedLeaderElection() bool {
	return true
}

// Register registers a new scaler for the given runtime.
func (m *MultiAutoscaler) Register(modelID string, target types.NamespacedName) {
	m.logger.Info("Registering scaler", "modelID", modelID, "target", target)
	sc := m.config.DefaultScaler
	if c, ok := m.config.RuntimeScalers[modelID]; ok {
		sc = c
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.scalers[target]; ok {
		// already registered
		return
	}
	s := scaler{
		modelID:                modelID,
		target:                 target,
		k8sClient:              m.k8sClient,
		metrics:                m.metrics,
		config:                 sc,
		scaleToZeroGracePeriod: m.config.ScaleToZeroGracePeriod,
	}
	m.scalers[target] = s
	go func() {
		<-m.startCh
		s.start(m.logger, m.stopCh, m.config.InitialDelay, m.config.SyncPeriod)
	}()
}

// Unregister unregisters the scaler for the given runtime.
func (m *MultiAutoscaler) Unregister(target types.NamespacedName) {
	m.logger.Info("Unregistering scaler", "target", target)
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.scalers[target]
	if !ok {
		return
	}
	s.stop()
	delete(m.scalers, target)
}

type scaler struct {
	modelID string
	target  types.NamespacedName

	k8sClient client.Client
	metrics   metrics.Provider

	config                 config.ScalingConfig
	scaleToZeroGracePeriod time.Duration

	lastTransitToZero time.Time

	cancel context.CancelFunc
}

func (s *scaler) start(log logr.Logger, stopCh <-chan struct{}, initialDelay, period time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	log = log.WithName("scaler").WithValues("modelID", s.modelID)
	ctx = ctrl.LoggerInto(ctx, log)

	scale := func() {
		// TODO: rethink better error handling
		if err := retry.RetryOnConflict(
			retry.DefaultRetry,
			func() error {
				_, err := s.scale(ctx)
				return err
			},
		); err != nil {
			log.Error(err, "Failed to scale")
		}
	}
	log.Info("Starting autoscaler", "initialDelay", initialDelay)
	time.Sleep(initialDelay)
	scale()
	ticker := time.NewTicker(period)
	for {
		select {
		case <-stopCh:
			log.Info("Stopped autoscaler by stop channel")
			cancel()
			return
		case <-ctx.Done():
			log.Info("Stopped autoscaler by context", "ctx", ctx.Err())
			return
		case <-ticker.C:
			scale()
		}
	}
}

func (s *scaler) stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *scaler) scale(ctx context.Context) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	var sts appsv1.StatefulSet
	if err := s.k8sClient.Get(ctx, s.target, &sts); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	// TODO(aya): Stop scaling when there are too many not-ready pods

	replicas := sts.Status.ReadyReplicas
	metricsVal := s.metrics.Get(s.modelID)
	metricsValPerPods := metricsVal / float64(max(replicas, 1))
	desiredReplicas := ceil(metricsValPerPods / s.config.TargetValue)
	log.V(6).Info("Scaling calculation",
		"total", metricsVal,
		"per-pods", metricsValPerPods,
		"spec-replicas", sts.Spec.Replicas,
		"ready-replicas", replicas,
		"desired", desiredReplicas)

	newReplicas := max(s.config.MinReplicas, desiredReplicas)
	if s.config.MaxReplicas > 0 {
		newReplicas = min(newReplicas, s.config.MaxReplicas)
	}

	if newReplicas == 0 {
		if s.lastTransitToZero.IsZero() {
			s.lastTransitToZero = time.Now()
		}
		if since := time.Since(s.lastTransitToZero); since < s.scaleToZeroGracePeriod {
			log.V(4).Info("Within the scale to zero grace period", "since", since)
			newReplicas = 1
		}
	} else {
		s.lastTransitToZero = time.Time{}
	}

	specReplicas := ptr.Deref(sts.Spec.Replicas, 0)

	if newReplicas < specReplicas {
		maxScaleDown := floor(float64(replicas) * s.config.MaxScaleDownRate)
		newReplicas = max(newReplicas, maxScaleDown)
	} else if newReplicas > specReplicas {
		maxScaleUp := ceil(float64(max(1, replicas)) * s.config.MaxScaleUpRate)
		newReplicas = min(newReplicas, maxScaleUp)
	}
	if newReplicas != specReplicas {
		err := s.scaleTo(ctx, &sts, newReplicas)
		return err == nil, err
	}
	return false, nil
}

func (s *scaler) scaleTo(ctx context.Context, sts *appsv1.StatefulSet, replicas int32) error {
	ctrl.LoggerFrom(ctx).Info("Scaling", "from", sts.Spec.Replicas, "to", replicas)
	// TODO(aya): record scaling events
	scale := &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: replicas}}
	return s.k8sClient.SubResource("scale").Update(ctx, sts, client.WithSubResourceBody(scale))
}

func min(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func ceil(a float64) int32 {
	return int32(math.Ceil(a))
}

func floor(a float64) int32 {
	return int32(math.Floor(a))
}
