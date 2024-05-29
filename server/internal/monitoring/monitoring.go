package monitoring

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricNamespace = "llm_operator"

	metricsNameCompletionLatency = "inference_manager_serever_completion_latency"
	metricLabelModelID           = "model_id"
)

// MetricsMonitoring is an interface for monitoring metrics.
type MetricsMonitoring interface {
	ObserveCompletionLatency(modelID string, latency time.Duration)
}

// MetricsMonitor holds and updates Prometheus metrics.
type MetricsMonitor struct {
	completionLatencyHistVec *prometheus.HistogramVec
}

// latencyBuckets are the buckets for the latencies from 100ms to 5 minutes.
var latencyBuckets []float64 = []float64{
	.1, .2, .5, 1, 2, 5, 10, 30, 60, 120, 180, 240, 300,
}

// NewMetricsMonitor returns a new MetricsMonitor.
func NewMetricsMonitor() *MetricsMonitor {
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

	m := &MetricsMonitor{
		completionLatencyHistVec: completionLatencyHistVec,
	}

	prometheus.MustRegister(
		completionLatencyHistVec,
	)

	return m
}

// ObserveCompletionLatency observes a new latency data for a completion request.
func (m *MetricsMonitor) ObserveCompletionLatency(modelID string, latency time.Duration) {
	m.completionLatencyHistVec.WithLabelValues(modelID).Observe(float64(latency) / float64(time.Second))
}

// UnregisterAllCollectors unregisters all connectors.
func (m *MetricsMonitor) UnregisterAllCollectors() {
	prometheus.Unregister(m.completionLatencyHistVec)
}
