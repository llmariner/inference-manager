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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewMultiAutoscaler creates a new MultiAutoscaler.
func NewMultiAutoscaler(k8sClient client.Client, metricsClient *metrics.Client, config config.AutoscalerConfig) *MultiAutoscaler {
	return &MultiAutoscaler{
		k8sClient:     k8sClient,
		metricsClient: metricsClient,
		config:        config,
		scalers:       make(map[types.NamespacedName]scaler),
		stopCh:        make(chan struct{}),
	}
}

// MultiAutoscaler is a controller that manages multiple scalers.
type MultiAutoscaler struct {
	logger logr.Logger

	k8sClient     client.Client
	metricsClient *metrics.Client

	config config.AutoscalerConfig

	scalers map[types.NamespacedName]scaler
	mu      sync.Mutex

	stopCh chan struct{}
}

// SetupWithManager sets up the multi-autoscaler with the Manager.
func (m *MultiAutoscaler) SetupWithManager(mgr ctrl.Manager) error {
	m.logger = mgr.GetLogger().WithName("multiscaler")
	return mgr.Add(m)
}

// Start starts the multi-autoscaler.
func (m *MultiAutoscaler) Start(ctx context.Context) error {
	m.logger.Info("Starting multi-autoscaler")
	<-ctx.Done()
	close(m.stopCh)
	return nil
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
		metricsClient:          m.metricsClient,
		config:                 sc,
		scaleToZeroGracePeriod: m.config.ScaleToZeroGracePeriod,
	}
	m.scalers[target] = s
	s.start(m.logger, m.stopCh, m.config.SyncPeriod)
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

	k8sClient     client.Client
	metricsClient *metrics.Client

	config                 config.ScalingConfig
	scaleToZeroGracePeriod time.Duration

	lastTransitToZero time.Time

	cancel context.CancelFunc
}

func (s *scaler) start(log logr.Logger, stopCh <-chan struct{}, period time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	log = log.WithName("scaler").WithValues("modelID", s.modelID)
	ctx = ctrl.LoggerInto(ctx, log)

	ticker := time.NewTicker(period)
	go func() {
		log.Info("Starting autoscaler")
		for {
			select {
			case <-stopCh:
				cancel()
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				// TODO: rethink better error handling
				if err := retry.RetryOnConflict(
					retry.DefaultRetry,
					func() error { return s.scale(ctx) },
				); err != nil {
					log.Error(err, "Failed to scale")
				}
			}
		}
	}()
}

func (s *scaler) stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *scaler) scale(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)

	var sts appsv1.StatefulSet
	if err := s.k8sClient.Get(ctx, s.target, &sts); err != nil {
		return client.IgnoreNotFound(err)
	}

	totalObserved := s.metricsClient.Get(s.modelID)
	observedPerPods := totalObserved / float64(sts.Status.Replicas)

	desiredReplicas := math.Ceil(observedPerPods / s.config.TargetValue)

	rs := math.Max(float64(s.config.MinReplicas), desiredReplicas)
	if s.config.MaxReplicas > 0 {
		rs = math.Min(rs, float64(s.config.MaxReplicas))
	}
	cappedReplicas := int32(rs)

	// TODO(aya): Handles changes in both directions during bursts at separate rateshandle burstable both directions in separate rates
	// TODO(aya): Stop scaling when there are too many not-ready pods

	if cappedReplicas == 0 {
		if s.lastTransitToZero.IsZero() {
			s.lastTransitToZero = time.Now()
		}
		if since := time.Since(s.lastTransitToZero); since < s.scaleToZeroGracePeriod {
			log.V(4).Info("Within the scale to zero grace period", "since", since)
			return nil
		}
	} else {
		s.lastTransitToZero = time.Time{}
	}
	return s.scaleTo(ctx, &sts, cappedReplicas)
}

func (s *scaler) scaleTo(ctx context.Context, sts *appsv1.StatefulSet, replicas int32) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Scaling", "from", sts.Status.Replicas, "to", replicas)
	// TODO(aya): record scaling events
	scale := &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: replicas}}
	return s.k8sClient.SubResource("scale").Update(ctx, sts, client.WithSubResourceBody(scale))
}
