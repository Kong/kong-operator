package metrics

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/go-database-reconciler/pkg/crud"
	deckutils "github.com/kong/go-database-reconciler/pkg/utils"
	"github.com/kong/go-kong/kong"
	prom "github.com/prometheus/client_model/go"
	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/deckerrors"
)

func TestNewGlobalCtrlRuntimeMetricsRecorder_DoesNotPanicWhenCalledTwice(t *testing.T) {
	require.NotPanics(t, func() {
		_ = NewGlobalCtrlRuntimeMetricsRecorder(uuid.New())
	})
	require.NotPanics(t, func() {
		_ = NewGlobalCtrlRuntimeMetricsRecorder(uuid.New())
	})
}

func TestGlobalCtrlRuntimeMetricsRecorder_AnyInstanceWritesToTheSameRegistry(t *testing.T) {
	m1 := NewGlobalCtrlRuntimeMetricsRecorder(uuid.New())
	m2 := NewGlobalCtrlRuntimeMetricsRecorder(uuid.New())

	const (
		firstDPHost  = "https://1.host"
		secondDPHost = "https://2.host"
	)
	m1.RecordPushSuccess(ProtocolDBLess, time.Millisecond, mo.Some(22), firstDPHost)
	m2.RecordPushSuccess(ProtocolDBLess, time.Millisecond, mo.Some(22), secondDPHost)

	// Verify that both instances write to the same registry (the controller-runtime's global registry).
	ctrlRuntimeGlobalRegistry := metrics.Registry
	metricFamilies, err := ctrlRuntimeGlobalRegistry.Gather()
	require.NoError(t, err)
	metricFamily, ok := lo.Find(metricFamilies, func(family *prom.MetricFamily) bool {
		return family.Name != nil && family.GetName() == MetricNameConfigPushCount
	})
	require.True(t, ok, "expected to find %q metric", MetricNameConfigPushCount)

	assertMetricsContainHost := func(host string) {
		assert.True(t, lo.ContainsBy(metricFamily.GetMetric(), func(metric *prom.Metric) bool {
			return lo.ContainsBy(metric.GetLabel(), func(label *prom.LabelPair) bool {
				return label.GetName() == DataplaneKey && label.GetValue() == host
			})
		}), "expected to find %q metric with dataplane label %q", MetricNameConfigPushCount, host)
	}
	assertMetricsContainHost(firstDPHost)
	assertMetricsContainHost(secondDPHost)
}

func TestRecordPush(t *testing.T) {
	mockSizeOfCfg := mo.Some(22)
	m := NewGlobalCtrlRuntimeMetricsRecorder(uuid.New())

	t.Run("recording push success works", func(t *testing.T) {
		require.NotPanics(t, func() {
			m.RecordPushSuccess(ProtocolDBLess, time.Millisecond, mockSizeOfCfg, "https://10.0.0.1:8080")
		})
	})
	t.Run("recording push failure works", func(t *testing.T) {
		require.NotPanics(t, func() {
			m.RecordPushFailure(ProtocolDBLess, time.Millisecond, mockSizeOfCfg, "https://10.0.0.1:8080", 5, fmt.Errorf("custom error"))
		})
	})
	// Verify that multiple call of NewGlobalCtrlRuntimeMetricsRecorder keeps all created metrics work.
	m2 := NewGlobalCtrlRuntimeMetricsRecorder(uuid.New())
	t.Run("recording push success works for old metrics", func(t *testing.T) {
		require.NotPanics(t, func() {
			m.RecordPushSuccess(ProtocolDBLess, time.Millisecond, mockSizeOfCfg, "https://10.0.0.1:8080")
		})
	})
	t.Run("recording push success works for new metrics", func(t *testing.T) {
		require.NotPanics(t, func() {
			m2.RecordPushSuccess(ProtocolDBLess, time.Millisecond, mockSizeOfCfg, "https://10.0.0.2:8080")
		})
	})
}

func TestRecordTranslation(t *testing.T) {
	m := NewGlobalCtrlRuntimeMetricsRecorder(uuid.New())
	t.Run("recording translation success works", func(t *testing.T) {
		require.NotPanics(t, func() {
			m.RecordTranslationSuccess(10 * time.Millisecond)
			m.RecordTranslationBrokenResources(0)
		})
	})
	t.Run("recording translation failure works", func(t *testing.T) {
		require.NotPanics(t, func() {
			m.RecordTranslationFailure(10 * time.Millisecond)
			m.RecordTranslationBrokenResources(9)
		})
	})
}

func TestPushFailureReason(t *testing.T) {
	apiConflictErr := kong.NewAPIError(http.StatusConflict, "conflict api error")
	networkErr := net.UnknownNetworkError("network error")
	genericError := errors.New("generic error")

	testCases := []struct {
		name           string
		err            error
		expectedReason string
	}{
		{
			name:           "generic_error",
			err:            genericError,
			expectedReason: FailureReasonOther,
		},
		{
			name:           "api_conflict_error",
			err:            apiConflictErr,
			expectedReason: FailureReasonConflict,
		},
		{
			name:           "api_conflict_error_wrapped",
			err:            fmt.Errorf("wrapped conflict api err: %w", apiConflictErr),
			expectedReason: FailureReasonConflict,
		},
		{
			name:           "deck_config_conflict_error_empty",
			err:            deckerrors.ConfigConflictError{},
			expectedReason: FailureReasonConflict,
		},
		{
			name:           "deck_config_conflict_error_with_generic_error",
			err:            deckerrors.ConfigConflictError{Err: genericError},
			expectedReason: FailureReasonConflict,
		},
		{
			name:           "deck_err_array_with_api_conflict_error",
			err:            deckutils.ErrArray{Errors: []error{apiConflictErr}},
			expectedReason: FailureReasonConflict,
		},
		{
			name:           "wrapped_deck_err_array_with_api_conflict_error",
			err:            fmt.Errorf("wrapped: %w", deckutils.ErrArray{Errors: []error{apiConflictErr}}),
			expectedReason: FailureReasonConflict,
		},
		{
			name:           "deck_err_array_with_generic_error",
			err:            deckutils.ErrArray{Errors: []error{genericError}},
			expectedReason: FailureReasonOther,
		},
		{
			name:           "deck_err_array_empty",
			err:            deckutils.ErrArray{Errors: []error{genericError}},
			expectedReason: FailureReasonOther,
		},
		{
			name:           "network_error",
			err:            networkErr,
			expectedReason: FailureReasonNetwork,
		},
		{
			name:           "network_error_wrapped_in_deck_config_conflict_error",
			err:            deckerrors.ConfigConflictError{Err: networkErr},
			expectedReason: FailureReasonNetwork,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reason := pushFailureReason(tc.err)
			require.Equal(t, tc.expectedReason, reason)
		})
	}
}

func TestRecordKonnectConfigSync(t *testing.T) {
	instanceID := uuid.New()
	m := NewGlobalCtrlRuntimeMetricsRecorder(instanceID)
	serverURL := fmt.Sprintf("https://%s.konghq.tech/kic/api/control-planes/%s", uuid.NewString(), uuid.NewString())

	m.RecordKonnectConfigSyncSuccess(KonnectConfigSync{
		ServerURL:   serverURL,
		Concurrency: 4,
		Duration:    time.Second,
		EntityCount: 10,
		CreateOps:   3,
		UpdateOps:   2,
		DeleteOps:   1,
	})
	m.RecordKonnectConfigSyncFailure(KonnectConfigSync{
		ServerURL:   serverURL,
		Concurrency: 4,
		Duration:    time.Second,
		EntityCount: 20,
		CreateOps:   5,
	}, fmt.Errorf("wrapped: %w", kong.NewAPIError(http.StatusBadRequest, "bad request")))

	metricFamilies, err := metrics.Registry.Gather()
	require.NoError(t, err)

	// findMetric returns the metric of the given family matching all given labels.
	findMetric := func(t *testing.T, familyName string, labels map[string]string) *prom.Metric {
		t.Helper()
		family, ok := lo.Find(metricFamilies, func(family *prom.MetricFamily) bool {
			return family.GetName() == familyName
		})
		require.True(t, ok, "expected to find %q metric", familyName)
		labels[ServerURLKey] = serverURL
		labels[ConcurrencyKey] = "4"
		labels[InstanceIDKey] = instanceID.String()
		metric, ok := lo.Find(family.GetMetric(), func(metric *prom.Metric) bool {
			return lo.EveryBy(lo.Entries(labels), func(e lo.Entry[string, string]) bool {
				return lo.ContainsBy(metric.GetLabel(), func(label *prom.LabelPair) bool {
					return label.GetName() == e.Key && label.GetValue() == e.Value
				})
			})
		})
		require.True(t, ok, "expected to find %q metric with labels %v", familyName, labels)
		return metric
	}

	successLabels := func() map[string]string {
		return map[string]string{SuccessKey: SuccessTrue, StatusCodeKey: ""}
	}
	failureLabels := func() map[string]string {
		return map[string]string{SuccessKey: SuccessFalse, StatusCodeKey: "400"}
	}

	t.Run("duration", func(t *testing.T) {
		require.Equal(t, uint64(1), findMetric(t, MetricNameKonnectConfigSyncDuration, successLabels()).GetHistogram().GetSampleCount())
		require.Equal(t, uint64(1), findMetric(t, MetricNameKonnectConfigSyncDuration, failureLabels()).GetHistogram().GetSampleCount())
	})
	t.Run("entity count", func(t *testing.T) {
		require.InDelta(t, 10, findMetric(t, MetricNameKonnectConfigSyncEntityCount, successLabels()).GetGauge().GetValue(), 0)
		require.InDelta(t, 20, findMetric(t, MetricNameKonnectConfigSyncEntityCount, failureLabels()).GetGauge().GetValue(), 0)
	})
	t.Run("operations", func(t *testing.T) {
		for operation, expected := range map[string]float64{
			OperationCreate: 3,
			OperationUpdate: 2,
			OperationDelete: 1,
		} {
			labels := successLabels()
			labels[OperationKey] = operation
			require.InDelta(t, expected, findMetric(t, MetricNameKonnectConfigSyncOperations, labels).GetCounter().GetValue(), 0)
		}
		labels := failureLabels()
		labels[OperationKey] = OperationCreate
		require.InDelta(t, 5, findMetric(t, MetricNameKonnectConfigSyncOperations, labels).GetCounter().GetValue(), 0)
	})
}

func TestKonnectSyncFailureStatusCode(t *testing.T) {
	badRequestErr := kong.NewAPIError(http.StatusBadRequest, "bad request")
	notFoundErr := kong.NewAPIError(http.StatusNotFound, "not found")

	testCases := []struct {
		name               string
		err                error
		expectedStatusCode string
	}{
		{
			name:               "api_error",
			err:                badRequestErr,
			expectedStatusCode: "400",
		},
		{
			name:               "api_error_wrapped",
			err:                fmt.Errorf("wrapped: %w", badRequestErr),
			expectedStatusCode: "400",
		},
		{
			name:               "deck_err_array_with_api_errors",
			err:                deckutils.ErrArray{Errors: []error{errors.New("generic error"), notFoundErr, badRequestErr}},
			expectedStatusCode: "404",
		},
		{
			name: "wrapped_deck_err_array_with_crud_action_errors",
			err: fmt.Errorf("wrapped: %w", deckutils.ErrArray{Errors: []error{
				&crud.ActionError{OperationType: crud.Create, Kind: "service", Name: "svc", Err: errors.New("generic error")},
				&crud.ActionError{OperationType: crud.Update, Kind: "route", Name: "route", Err: badRequestErr},
			}}),
			expectedStatusCode: "400",
		},
		{
			name:               "network_error",
			err:                net.UnknownNetworkError("network error"),
			expectedStatusCode: "",
		},
		{
			name:               "generic_error",
			err:                errors.New("generic error"),
			expectedStatusCode: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.expectedStatusCode, konnectSyncFailureStatusCode(tc.err))
		})
	}
}
