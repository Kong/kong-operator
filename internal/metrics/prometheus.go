package metrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Recorder is the interface fir recording metrics on a certain operation.
type Recorder interface {
	// REVIEW: add URL of Konnect API as a label?
	RecordKonnectEntityOperationSuccess(operationType KonnectEntityOperation, entityType string, duration time.Duration)
	RecordKonnectEntityOperationFailure(operationType KonnectEntityOperation, entityType string, duration time.Duration, statusCode int)
}

type KonnectEntityOperation string

const (
	KonnectEntityOperationTypeKey                        = "operation_type"
	KonnectEntityOperationCreate  KonnectEntityOperation = "create"
	KonnectEnttiyOperationUpdate  KonnectEntityOperation = "update"
	KonnectEntityOperationDelete  KonnectEntityOperation = "delete"

	KonnectEntityTypeKey = "entity_type"

	SuccessKey   = "success"
	SuccessTrue  = "true"
	SuccessFalse = "false"

	StatusCodeKey = "status_code"
)

// metric names for konnect entity operations.
const (
	// REVIEW: define a Namespace `gateway_operator` for creating prometheus metrics here?
	MetricNameKonnectEntityOperationCount    = "gateway_operator_konnect_entity_operation_count"
	MetricNameKonnectEntityOperationDuration = "gateway_operator_konnect_entity_operation_duration"
)

var (
	konnectEntityOperationCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameKonnectEntityOperationCount,
			Help: fmt.Sprintf(
				"Count of successful/failed entity operations in Konnect. "+
					"`%s` describes the operation type (`%s`, `%s`, or `%s`)."+
					"`%s` describes the type of the operated entity. "+
					"`%s` describes whether the operation is successful (`%s`) or not (`%s`). "+
					"`%s` is populated in case of `%s=\"%s\"` and describes the status code returned from Konnect API.",
				KonnectEntityOperationTypeKey, KonnectEntityOperationCreate, KonnectEnttiyOperationUpdate, KonnectEntityOperationDelete,
				KonnectEntityTypeKey,
				SuccessKey, SuccessTrue, SuccessFalse,
				StatusCodeKey, SuccessKey, SuccessFalse,
			),
		},
		[]string{KonnectEntityOperationTypeKey, KonnectEntityTypeKey, SuccessKey, StatusCodeKey},
	)

	konnectEntityOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: MetricNameKonnectEntityOperationDuration,
			Help: fmt.Sprintf(
				"How long did the Konnect entity operation take in seconds. "+
					"`%s` describes the operation type (`%s`, `%s`, or `%s`)."+
					"`%s` describes the type of the operated entity. "+
					"`%s` describes whether the operation is successful (`%s`) or not (`%s`). "+
					"`%s` is populated in case of `%s=\"%s\"` and describes the status code returned from Konnect API.",
				KonnectEntityOperationTypeKey, KonnectEntityOperationCreate, KonnectEnttiyOperationUpdate, KonnectEntityOperationDelete,
				KonnectEntityTypeKey,
				SuccessKey, SuccessTrue, SuccessFalse,
				StatusCodeKey, SuccessKey, SuccessFalse,
			),
			// Duration range from 0.1s to 10min (600s).
			Buckets: prometheus.ExponentialBucketsRange(0.1, 600, 20),
		},
		[]string{KonnectEntityOperationTypeKey, KonnectEntityTypeKey, SuccessKey, StatusCodeKey},
	)
)

// GlobalCtrlRuntimeMetricsRecorder is a metrics recorder that uses a global Prometheus registry
// provided by the controller-runtime. Any instance of it will record metrics to the same registry.
//
// We want to expose Gateway operator's custom metrics on the same endpoint as controller-runtime's built-in
// ones. Because of that, we have to use its global registry as CR doesn't allow injecting a custom one.
// Upstream issue regarding this: https://github.com/kubernetes-sigs/controller-runtime/issues/210.
type GlobalCtrlRuntimeMetricsRecorder struct{}

var _ Recorder = &GlobalCtrlRuntimeMetricsRecorder{}

func NewGlobalCtrlRuntimeMetricsRecorder() *GlobalCtrlRuntimeMetricsRecorder {
	return &GlobalCtrlRuntimeMetricsRecorder{}
}

func (r *GlobalCtrlRuntimeMetricsRecorder) RecordKonnectEntityOperationSuccess(
	operationType KonnectEntityOperation, entityType string, duration time.Duration) {
	r.recordKonnectEntityOperationCount(operationType, entityType, true, 0)
	r.recordKonnectEntityOperationDuration(operationType, entityType, true, 0, duration)
}

func (r *GlobalCtrlRuntimeMetricsRecorder) RecordKonnectEntityOperationFailure(
	operationType KonnectEntityOperation, entityType string, duration time.Duration, statusCode int) {
	r.recordKonnectEntityOperationCount(operationType, entityType, false, statusCode)
	r.recordKonnectEntityOperationDuration(operationType, entityType, false, statusCode, duration)
}

func (r *GlobalCtrlRuntimeMetricsRecorder) recordKonnectEntityOperationCount(
	operationType KonnectEntityOperation, entityType string, success bool, statusCode int,
) {
	labels := konnectEntityOperationLabels(operationType, entityType, success, statusCode)
	konnectEntityOperationCount.With(labels).Inc()
}

func (r *GlobalCtrlRuntimeMetricsRecorder) recordKonnectEntityOperationDuration(
	operationType KonnectEntityOperation, entityType string, success bool, statusCode int, duration time.Duration,
) {
	labels := konnectEntityOperationLabels(operationType, entityType, success, statusCode)
	konnectEntityOperationDuration.With(labels).Observe(duration.Seconds())
}

func konnectEntityOperationLabels(operationType KonnectEntityOperation, entityType string, success bool, statusCode int,
) prometheus.Labels {
	labels := prometheus.Labels{
		KonnectEntityOperationTypeKey: string(operationType),
		KonnectEntityTypeKey:          entityType,
	}
	if success {
		labels[SuccessKey] = SuccessTrue
		labels[StatusCodeKey] = ""
	} else {
		labels[SuccessKey] = SuccessFalse
		labels[StatusCodeKey] = strconv.Itoa(statusCode)
	}
	return labels
}

func init() {
	allMetrics := []prometheus.Collector{
		konnectEntityOperationCount,
		konnectEntityOperationDuration,
	}
	for _, m := range allMetrics {
		ctrlmetrics.Registry.MustRegister(m)
	}
}

type MockRecorder struct{}

var _ Recorder = &MockRecorder{}

func (m *MockRecorder) RecordKonnectEntityOperationSuccess(
	operationType KonnectEntityOperation, entityType string, duration time.Duration) {
}

func (m *MockRecorder) RecordKonnectEntityOperationFailure(
	operationType KonnectEntityOperation, entityType string, duration time.Duration, statusCode int) {
}
