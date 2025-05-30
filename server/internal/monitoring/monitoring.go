package monitoring

import (
	"context"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricNamespace = "llmariner"

	metricsNameCompletionLatency         = "inference_manager_server_completion_latency"
	metricsNameCompletionRequest         = "inference_manager_server_completion_num_active_requests"
	metricsNameEmbeddingLatency          = "inference_manager_server_embedding_latency"
	metricsNameEmbeddingRequest          = "inference_manager_server_embedding_num_active_requests"
	metricsNameNumQueuedTasks            = "inference_manager_server_num_queued_tasks"
	metricsNameNumInProgressTasks        = "inference_manager_server_num_in_progress_tasks"
	metricsNameMaxInProgressTaskDuration = "inference_manager_server_max_in_progress_task_duration"
	metricsNameNumEngines                = "inference_manager_server_num_engines"
	metricsNameNumLocalEngines           = "inference_manager_server_num_local_engines"
	metricsNameRequestCount              = "inference_manager_server_request_count"
	metricsNameTaskErrorCount            = "inference_manager_server_task_error_count"

	metricsNameSinceLastEngineHeartbeat = "inference_manager_server_since_last_engine_heartbeat"

	metricLabelModelID = "model_id"

	metricLabelEngineID = "engine_id"

	metricLabelTenantID = "tenant_id"

	metricLabelStatusCode = "status_code"
)

// MetricsMonitor holds and updates Prometheus metrics.
type MetricsMonitor struct {
	p      *infprocessor.P
	logger logr.Logger

	completionLatencyHistVec       *prometheus.HistogramVec
	completionRequestGaugeVec      *prometheus.GaugeVec
	embeddingLatencyHistVec        *prometheus.HistogramVec
	embeddingRequestGaugeVec       *prometheus.GaugeVec
	numQueuedTasksGauge            prometheus.Gauge
	numInProgressTasksGauge        prometheus.Gauge
	maxInProgressTaskDurationGauge prometheus.Gauge
	numEnginesGaugeVec             *prometheus.GaugeVec
	numLocalEnginesGaugeVec        *prometheus.GaugeVec
	requestCounterVec              *prometheus.CounterVec
	taskErrorCounter               prometheus.Counter

	sinceLastEngineHeartbeatGaugeVec *prometheus.GaugeVec
}

// latencyBuckets are the buckets for the latencies from 100ms to 5 minutes.
var latencyBuckets []float64 = []float64{
	.1, .2, .5, 1, 2, 5, 10, 30, 60, 120, 180, 240, 300,
}

// NewMetricsMonitor returns a new MetricsMonitor.
func NewMetricsMonitor(p *infprocessor.P, logger logr.Logger) *MetricsMonitor {
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

	embeddingLatencyHistVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricNamespace,
			Name:      metricsNameEmbeddingLatency,
			Buckets:   latencyBuckets,
		},
		[]string{
			metricLabelModelID,
		},
	)

	embeddingRequestGaugeVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      metricsNameEmbeddingRequest,
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

	numEnginesGaugeVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      metricsNameNumEngines,
		},
		[]string{
			metricLabelTenantID,
		},
	)

	numLocalEnginesGaugeVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      metricsNameNumLocalEngines,
		},
		[]string{
			metricLabelTenantID,
		},
	)

	requestCounterVec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      metricsNameRequestCount,
		},
		[]string{
			metricLabelModelID,
			metricLabelTenantID,
			metricLabelStatusCode,
		},
	)

	taskErrorCounter := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      metricsNameTaskErrorCount,
		},
	)

	sinceLastEngineHeartbeatGaugeVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      metricsNameSinceLastEngineHeartbeat,
		},
		[]string{
			metricLabelEngineID,
		},
	)

	m := &MetricsMonitor{
		p:      p,
		logger: logger.WithName("monitor"),

		completionLatencyHistVec:         completionLatencyHistVec,
		completionRequestGaugeVec:        completionRequestGaugeVec,
		embeddingLatencyHistVec:          embeddingLatencyHistVec,
		embeddingRequestGaugeVec:         embeddingRequestGaugeVec,
		numQueuedTasksGauge:              numQueuedTasksGauge,
		numInProgressTasksGauge:          numInProgressTasksGauge,
		maxInProgressTaskDurationGauge:   maxInProgressTaskDurationGauge,
		numEnginesGaugeVec:               numEnginesGaugeVec,
		numLocalEnginesGaugeVec:          numLocalEnginesGaugeVec,
		requestCounterVec:                requestCounterVec,
		taskErrorCounter:                 taskErrorCounter,
		sinceLastEngineHeartbeatGaugeVec: sinceLastEngineHeartbeatGaugeVec,
	}

	prometheus.MustRegister(
		completionLatencyHistVec,
		completionRequestGaugeVec,
		embeddingLatencyHistVec,
		embeddingRequestGaugeVec,
		numQueuedTasksGauge,
		numInProgressTasksGauge,
		maxInProgressTaskDurationGauge,
		numEnginesGaugeVec,
		numLocalEnginesGaugeVec,
		requestCounterVec,
		taskErrorCounter,
		sinceLastEngineHeartbeatGaugeVec,
	)

	return m
}

// Run updates the metrics periodically.
func (m *MetricsMonitor) Run(ctx context.Context, interval time.Duration) error {
	m.logger.Info("Starting metrics monitor...", "interval", interval)
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

// ObserveEmbeddingLatency observes a new latency data for an embedding request.
func (m *MetricsMonitor) ObserveEmbeddingLatency(modelID string, latency time.Duration) {
	m.embeddingLatencyHistVec.WithLabelValues(modelID).Observe(float64(latency) / float64(time.Second))
}

// UpdateEmbeddingRequest updates the number of embedding requests.
func (m *MetricsMonitor) UpdateEmbeddingRequest(modelID string, c int) {
	m.embeddingRequestGaugeVec.WithLabelValues(modelID).Add(float64(c))
}

// ObserveRequestCount observes a new request count for a model and tenant.
func (m *MetricsMonitor) ObserveRequestCount(modelID, tenantID string, statusCode int32) {
	code := strconv.Itoa(int(statusCode))
	m.requestCounterVec.WithLabelValues(modelID, tenantID, code).Add(float64(1))
}

// ObserveTaskErrorCount increments the task error counter.
func (m *MetricsMonitor) ObserveTaskErrorCount() {
	m.taskErrorCounter.Add(float64(1))
}

func (m *MetricsMonitor) updatePeriodicMetrics() {
	m.numQueuedTasksGauge.Set(float64(m.p.NumQueuedTasks()))
	m.numInProgressTasksGauge.Set(float64(m.p.NumInProgressTasks()))
	m.maxInProgressTaskDurationGauge.Set(float64(m.p.MaxInProgressTaskDuration().Seconds()))

	m.numEnginesGaugeVec.Reset()
	for tenantID, numEngines := range m.p.NumEnginesByTenantID() {
		m.numEnginesGaugeVec.WithLabelValues(tenantID).Set(float64(numEngines))
	}

	m.numLocalEnginesGaugeVec.Reset()
	for tenantID, numLocalEngines := range m.p.NumLocalEnginesByTenantID() {
		m.numLocalEnginesGaugeVec.WithLabelValues(tenantID).Set(float64(numLocalEngines))
	}

	m.sinceLastEngineHeartbeatGaugeVec.Reset()
	for engineID, lastHeartbeat := range m.p.LastEngineHeartbeats() {
		d := time.Since(lastHeartbeat)
		m.sinceLastEngineHeartbeatGaugeVec.WithLabelValues(engineID).Set(float64(d / time.Second))
	}
}

// UnregisterAllCollectors unregisters all connectors.
func (m *MetricsMonitor) UnregisterAllCollectors() {
	prometheus.Unregister(m.completionLatencyHistVec)
	prometheus.Unregister(m.completionRequestGaugeVec)
	prometheus.Unregister(m.embeddingLatencyHistVec)
	prometheus.Unregister(m.embeddingRequestGaugeVec)
	prometheus.Unregister(m.numQueuedTasksGauge)
	prometheus.Unregister(m.numInProgressTasksGauge)
	prometheus.Unregister(m.maxInProgressTaskDurationGauge)
	prometheus.Unregister(m.numEnginesGaugeVec)
	prometheus.Unregister(m.numLocalEnginesGaugeVec)
	prometheus.Unregister(m.requestCounterVec)
	prometheus.Unregister(m.sinceLastEngineHeartbeatGaugeVec)
}
