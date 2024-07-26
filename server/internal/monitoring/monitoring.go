package monitoring

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricNamespace = "llm_operator"

	metricsNameCompletionLatency = "inference_manager_server_completion_latency"
	metricsNameCompletionRequest = "inference_manager_server_completion_num_active_requests"
	metricLabelModelID           = "model_id"
)

// MetricsMonitoring is an interface for monitoring metrics.
type MetricsMonitoring interface {
	ObserveCompletionLatency(modelID string, latency time.Duration)
	UpdateCompletionRequest(modelID string, c int)
}

// MetricsMonitor holds and updates Prometheus metrics.
type MetricsMonitor struct {
	completionLatencyHistVec  *prometheus.HistogramVec
	completionRequestGaugeVec *prometheus.GaugeVec
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

	completionRequestGaugeVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      metricsNameCompletionRequest,
		},
		[]string{
			metricLabelModelID,
		},
	)

	m := &MetricsMonitor{
		completionLatencyHistVec:  completionLatencyHistVec,
		completionRequestGaugeVec: completionRequestGaugeVec,
	}

	prometheus.MustRegister(
		completionLatencyHistVec,
		completionRequestGaugeVec,
	)

	return m
}

// ObserveCompletionLatency observes a new latency data for a completion request.
func (m *MetricsMonitor) ObserveCompletionLatency(modelID string, latency time.Duration) {
	m.completionLatencyHistVec.WithLabelValues(modelID).Observe(float64(latency) / float64(time.Second))
}

// UpdateCompletionRequest updates the number of completion requests.
func (m *MetricsMonitor) UpdateCompletionRequest(modelID string, c int) {
	m.completionRequestGaugeVec.WithLabelValues(modelID).Add(float64(c))
}

// UnregisterAllCollectors unregisters all connectors.
func (m *MetricsMonitor) UnregisterAllCollectors() {
	prometheus.Unregister(m.completionLatencyHistVec)
	prometheus.Unregister(m.completionRequestGaugeVec)
}
