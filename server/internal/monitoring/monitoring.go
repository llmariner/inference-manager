package monitoring

import (
	"context"
	"time"

	"github.com/llm-operator/inference-manager/server/internal/infprocessor"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricNamespace = "llm_operator"

	metricsNameCompletionLatency         = "inference_manager_server_completion_latency"
	metricsNameCompletionRequest         = "inference_manager_server_completion_num_active_requests"
	metricsNameNumQueuedTasks            = "inference_manager_server_num_queued_tasks"
	metricsNameNumInProgressTasks        = "inference_manager_server_num_in_progress_tasks"
	metricsNameMaxInProgressTaskDuration = "inference_manager_server_max_in_progress_task_duration"

	metricLabelModelID = "model_id"
)

// MetricsMonitoring is an interface for monitoring metrics.
type MetricsMonitoring interface {
	ObserveCompletionLatency(modelID string, latency time.Duration)
	UpdateCompletionRequest(modelID string, c int)
}

// MetricsMonitor holds and updates Prometheus metrics.
type MetricsMonitor struct {
	p *infprocessor.P

	completionLatencyHistVec       *prometheus.HistogramVec
	completionRequestGaugeVec      *prometheus.GaugeVec
	numQueuedTasksGauge            prometheus.Gauge
	numInProgressTasksGauge        prometheus.Gauge
	maxInProgressTaskDurationGauge prometheus.Gauge
}

// latencyBuckets are the buckets for the latencies from 100ms to 5 minutes.
var latencyBuckets []float64 = []float64{
	.1, .2, .5, 1, 2, 5, 10, 30, 60, 120, 180, 240, 300,
}

// NewMetricsMonitor returns a new MetricsMonitor.
func NewMetricsMonitor(p *infprocessor.P) *MetricsMonitor {
	completionLatencyHistVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricNamespace,
			Name:      metricsNameCompletionLatency,
			Buckets:   latencyBuckets,
		},
		[]string{
			metricLabelModelID,
		},
	)

	completionRequestGaugeVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      metricsNameCompletionRequest,
		},
		[]string{
			metricLabelModelID,
		},
	)

	numQueuedTasksGauge := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      metricsNameNumQueuedTasks,
		},
	)

	numInProgressTasksGauge := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      metricsNameNumInProgressTasks,
		},
	)

	maxInProgressTaskDurationGauge := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      metricsNameMaxInProgressTaskDuration,
		},
	)

	m := &MetricsMonitor{
		p: p,

		completionLatencyHistVec:       completionLatencyHistVec,
		completionRequestGaugeVec:      completionRequestGaugeVec,
		numQueuedTasksGauge:            numQueuedTasksGauge,
		numInProgressTasksGauge:        numInProgressTasksGauge,
		maxInProgressTaskDurationGauge: maxInProgressTaskDurationGauge,
	}

	prometheus.MustRegister(
		completionLatencyHistVec,
		completionRequestGaugeVec,
		numQueuedTasksGauge,
		numInProgressTasksGauge,
		maxInProgressTaskDurationGauge,
	)

	return m
}

// Run updates the metrics periodically.
func (m *MetricsMonitor) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ticker.C:
			m.updatePeriodicMetrics()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ObserveCompletionLatency observes a new latency data for a completion request.
func (m *MetricsMonitor) ObserveCompletionLatency(modelID string, latency time.Duration) {
	m.completionLatencyHistVec.WithLabelValues(modelID).Observe(float64(latency) / float64(time.Second))
}

// UpdateCompletionRequest updates the number of completion requests.
func (m *MetricsMonitor) UpdateCompletionRequest(modelID string, c int) {
	m.completionRequestGaugeVec.WithLabelValues(modelID).Add(float64(c))
}

func (m *MetricsMonitor) updatePeriodicMetrics() {
	m.numQueuedTasksGauge.Set(float64(m.p.NumQueuedTasks()))
	m.numInProgressTasksGauge.Set(float64(m.p.NumInProgressTasks()))
	m.maxInProgressTaskDurationGauge.Set(float64(m.p.MaxInProgressTaskDuration().Seconds()))
}

// UnregisterAllCollectors unregisters all connectors.
func (m *MetricsMonitor) UnregisterAllCollectors() {
	prometheus.Unregister(m.completionLatencyHistVec)
	prometheus.Unregister(m.completionRequestGaugeVec)
	prometheus.Unregister(m.numInProgressTasksGauge)
	prometheus.Unregister(m.maxInProgressTaskDurationGauge)
}
