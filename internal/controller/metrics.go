package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricSubsystem = "workload_host_alias"
)

var (
	ReconciliationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metricSubsystem,
			Name:      "reconciliation_total",
			Help:      "Total number of reconciliations",
		},
		[]string{"result"},
	)

	ReconciliationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: metricSubsystem,
			Name:      "reconciliation_duration_seconds",
			Help:      "Duration of reconciliation in seconds",
			Buckets:   prometheus.DefBuckets,
		},
	)

	ManagedWorkloads = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: metricSubsystem,
			Name:      "managed_workloads",
			Help:      "Number of workloads currently managed by the operator",
		},
	)
)

func init() {
	metrics.Registry.MustRegister(
		ReconciliationTotal,
		ReconciliationDuration,
		ManagedWorkloads,
	)
}
