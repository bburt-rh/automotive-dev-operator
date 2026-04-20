package imagebuild

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsNamespace = "ado"
	metricsSubsystem = "build"

	buildStatusSuccess = "success"
	buildStatusFailure = "failure"
)

var (
	// BuildDuration tracks the total build duration in seconds.
	BuildDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "duration_seconds",
			Help:      "Total build duration in seconds",
			Buckets:   []float64{30, 60, 120, 180, 240, 300, 420, 600, 900, 1200},
		},
		[]string{"mode", "distro", "target", "format", "arch", "status"},
	)

	// BuildPhaseDuration tracks duration of individual build phases in seconds.
	BuildPhaseDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "phase_duration_seconds",
			Help:      "Duration of individual build phases in seconds",
			Buckets:   []float64{1, 5, 10, 30, 60, 120, 180, 240, 300, 600},
		},
		[]string{"mode", "distro", "target", "phase"},
	)

	// BuildTotal counts total builds by status.
	BuildTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "total",
			Help:      "Total number of builds by status",
		},
		[]string{"mode", "distro", "target", "format", "arch", "status"},
	)

	// FlashTotal counts pipeline-triggered flash operations by status.
	FlashTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "flash",
			Name:      "total",
			Help:      "Total number of pipeline flash operations by status",
		},
		[]string{"target", "status"},
	)

	// FlashDuration tracks pipeline flash duration in seconds.
	FlashDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "flash",
			Name:      "duration_seconds",
			Help:      "Pipeline flash operation duration in seconds",
			Buckets:   []float64{10, 30, 60, 120, 180, 300, 600, 900},
		},
		[]string{"target", "status"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		BuildDuration,
		BuildPhaseDuration,
		BuildTotal,
		FlashTotal,
		FlashDuration,
	)
}
