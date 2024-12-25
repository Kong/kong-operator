package metrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Recorder is the interface for recording metrics for provided operation.
type Recorder interface {
	RecordKonnectEntityOperationSuccess(serverURL string, operationType KonnectEntityOperation, entityType string, duration time.Duration)
	RecordKonnectEntityOperationFailure(serverURL string, operationType KonnectEntityOperation, entityType string, duration time.Duration, statusCode int)
}

type KonnectEntityOperation string

const (
	KonnectServerURLKey                                  = "server_url"
	KonnectEntityOperationTypeKey                        = "operation_type"
	KonnectEntityOperationCreate  KonnectEntityOperation = "create"
	KonnectEntityOperationUpdate  KonnectEntityOperation = "update"
	KonnectEntityOperationDelete  KonnectEntityOperation = "delete"

	KonnectEntityTypeKey = "entity_type"

	SuccessKey   = "success"
	SuccessTrue  = "true"
	SuccessFalse = "false"

	StatusCodeKey = "status_code"
)

// metric names for konnect entity operations.
const (
	MetricNameKonnectEntityOperationCount    = "gateway_operator_konnect_entity_operation_count"
	MetricNameKonnectEntityOperationDuration = "gateway_operator_konnect_entity_operation_duration_milliseconds"
)

var (
	konnectEntityOperationCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameKonnectEntityOperationCount,
			Help: fmt.Sprintf(
				"Count of successful/failed entity operations in Konnect. "+
					"`%s` describes the URL of the Konnect server. "+
					"`%s` describes the operation type (`%s`, `%s`, or `%s`)."+
					"`%s` describes the type of the operated entity. "+
					"`%s` describes whether the operation is successful (`%s`) or not (`%s`). "+
					"`%s` is populated in case of `%s=\"%s\"` and describes the status code returned from Konnect API. "+
					"`%s`=\"0\" and %s=\"%s\" means we cannot collect the status code or error happens in the process other than Konnect API call.",
				KonnectServerURLKey,
				KonnectEntityOperationTypeKey, KonnectEntityOperationCreate, KonnectEntityOperationUpdate, KonnectEntityOperationDelete,
				KonnectEntityTypeKey,
				SuccessKey, SuccessTrue, SuccessFalse,
				StatusCodeKey, SuccessKey, SuccessFalse,
				StatusCodeKey, SuccessKey, SuccessFalse,
			),
		},
		[]string{KonnectServerURLKey, KonnectEntityOperationTypeKey, KonnectEntityTypeKey, SuccessKey, StatusCodeKey},
	)

	konnectEntityOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: MetricNameKonnectEntityOperationDuration,
			Help: fmt.Sprintf(
				"How long did the Konnect entity operation take in milliseconds. "+
					"`%s` describes the URL of the Konnect server. "+
					"`%s` describes the operation type (`%s`, `%s`, or `%s`)."+
					"`%s` describes the type of the operated entity. "+
					"`%s` describes whether the operation is successful (`%s`) or not (`%s`). "+
					"`%s` is populated in case of `%s=\"%s\"` and describes the status code returned from Konnect API. "+
					"`%s`=\"0\" and %s=\"%s\" means we cannot collect the status code or error happens in the process other than Konnect API call.",
				KonnectServerURLKey,
				KonnectEntityOperationTypeKey, KonnectEntityOperationCreate, KonnectEntityOperationUpdate, KonnectEntityOperationDelete,
				KonnectEntityTypeKey,
				SuccessKey, SuccessTrue, SuccessFalse,
				StatusCodeKey, SuccessKey, SuccessFalse,
				StatusCodeKey, SuccessKey, SuccessFalse,
			),
			// Duration range from 1ms to 10min.
			Buckets: prometheus.ExponentialBucketsRange(1, 10*float64(time.Minute.Milliseconds()), 20),
		},
		[]string{KonnectServerURLKey, KonnectEntityOperationTypeKey, KonnectEntityTypeKey, SuccessKey, StatusCodeKey},
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
	serverURL string, operationType KonnectEntityOperation, entityType string, duration time.Duration) {
	r.recordKonnectEntityOperationCount(serverURL, operationType, entityType, true, 0)
	r.recordKonnectEntityOperationDuration(serverURL, operationType, entityType, true, 0, duration)
}

func (r *GlobalCtrlRuntimeMetricsRecorder) RecordKonnectEntityOperationFailure(
	serverURL string, operationType KonnectEntityOperation, entityType string, duration time.Duration, statusCode int) {
	r.recordKonnectEntityOperationCount(serverURL, operationType, entityType, false, statusCode)
	r.recordKonnectEntityOperationDuration(serverURL, operationType, entityType, false, statusCode, duration)
}

func (r *GlobalCtrlRuntimeMetricsRecorder) recordKonnectEntityOperationCount(
	serverURL string, operationType KonnectEntityOperation, entityType string, success bool, statusCode int,
) {
	labels := konnectEntityOperationLabels(serverURL, operationType, entityType, success, statusCode)
	konnectEntityOperationCount.With(labels).Inc()
}

func (r *GlobalCtrlRuntimeMetricsRecorder) recordKonnectEntityOperationDuration(
	serverURL string, operationType KonnectEntityOperation, entityType string, success bool, statusCode int, duration time.Duration,
) {
	labels := konnectEntityOperationLabels(serverURL, operationType, entityType, success, statusCode)
	konnectEntityOperationDuration.With(labels).Observe(duration.Seconds())
}

func konnectEntityOperationLabels(
	serverURL string, operationType KonnectEntityOperation, entityType string, success bool, statusCode int,
) prometheus.Labels {
	labels := prometheus.Labels{
		KonnectServerURLKey:           serverURL,
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

// TODO: move all the mocks to a place inside `/test`:
// https://github.com/Kong/gateway-operator/issues/955
type MockRecorder struct{}

var _ Recorder = &MockRecorder{}

func (m *MockRecorder) RecordKonnectEntityOperationSuccess(
	serverURL string, operationType KonnectEntityOperation, entityType string, duration time.Duration) {
}

func (m *MockRecorder) RecordKonnectEntityOperationFailure(
	serverURL string, operationType KonnectEntityOperation, entityType string, duration time.Duration, statusCode int) {
}
