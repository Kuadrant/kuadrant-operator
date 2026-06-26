package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	reconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kuadrant_reconciliation_duration_seconds",
			Help:    "Duration of each top-level workflow phase in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"workflow"})

	effectivePolicyDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kuadrant_effective_policy_duration_seconds",
			Help:    "Duration of effective policy calculation in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"policy_type"})

	topologyRebuildDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "kuadrant_topology_rebuild_duration_seconds",
			Help:    "Duration of topology reconciliation including ToDot serialization and ConfigMap write in seconds",
			Buckets: prometheus.DefBuckets,
		})

	authconfigGenerationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "kuadrant_authconfig_generation_duration_seconds",
			Help:    "Duration of AuthConfig reconciliation in seconds",
			Buckets: prometheus.DefBuckets,
		})

	limitadorLimitsGenerationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "kuadrant_limitador_limits_generation_duration_seconds",
			Help:    "Duration of Limitador limits reconciliation in seconds",
			Buckets: prometheus.DefBuckets,
		})

	topologyObjectsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuadrant_topology_objects_total",
			Help: "Number of objects in the topology DAG by kind",
		},
		[]string{"kind"})

	authconfigsGeneratedTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kuadrant_authconfigs_generated_total",
			Help: "Number of AuthConfigs generated in the last reconciliation cycle",
		})
)

func init() {
	metrics.Registry.MustRegister(
		reconciliationDuration,
		effectivePolicyDuration,
		topologyRebuildDuration,
		authconfigGenerationDuration,
		limitadorLimitsGenerationDuration,
		topologyObjectsTotal,
		authconfigsGeneratedTotal,
	)
}

func ObserveReconciliationDuration(workflow string, start time.Time) {
	reconciliationDuration.WithLabelValues(workflow).Observe(time.Since(start).Seconds())
}

func ObserveEffectivePolicyDuration(policyType string, start time.Time) {
	effectivePolicyDuration.WithLabelValues(policyType).Observe(time.Since(start).Seconds())
}

func ObserveTopologyRebuildDuration(start time.Time) {
	topologyRebuildDuration.Observe(time.Since(start).Seconds())
}

func ObserveAuthconfigGenerationDuration(start time.Time) {
	authconfigGenerationDuration.Observe(time.Since(start).Seconds())
}

func ObserveLimitadorLimitsGenerationDuration(start time.Time) {
	limitadorLimitsGenerationDuration.Observe(time.Since(start).Seconds())
}

func SetTopologyObjectsTotal(kind string, count int) {
	topologyObjectsTotal.WithLabelValues(kind).Set(float64(count))
}

func SetAuthconfigsGeneratedTotal(count int) {
	authconfigsGeneratedTotal.Set(float64(count))
}
