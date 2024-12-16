package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsLabelModel             = "model"
	metricsNameActiveRequestCount = "llmariner_active_inference_request_count"
)

var (
	requestCountVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: metricsNameActiveRequestCount,
		Help: "The number of active inference request count",
	}, []string{metricsLabelModel})
)

func init() {
	metrics.Registry.MustRegister(requestCountVec)
}
