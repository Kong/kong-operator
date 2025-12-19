package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestNewTypedEventRecorder(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	recorder := NewTypedEventRecorder(fakeRecorder)

	require.NotNil(t, recorder)
	assert.Equal(t, fakeRecorder, recorder.recorder)
}

func TestTypedEventRecorder_Event(t *testing.T) {
	tests := []struct {
		name              string
		object            runtime.Object
		eventType         string
		baseReason        string
		message           string
		expectedEventType string
		expectedReason    string
		expectedMessage   string
	}{
		{
			name:              "HTTPRoute with TranslationSucceeded",
			object:            &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "default"}},
			eventType:         corev1.EventTypeNormal,
			baseReason:        "TranslationSucceeded",
			message:           "Translation completed successfully",
			expectedEventType: "Normal",
			expectedReason:    "HTTPRouteTranslationSucceeded",
			expectedMessage:   "Translation completed successfully",
		},
		{
			name:              "Gateway with TranslationFailed",
			object:            &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "test-gateway", Namespace: "default"}},
			eventType:         corev1.EventTypeWarning,
			baseReason:        "TranslationFailed",
			message:           "Translation failed due to error",
			expectedEventType: "Warning",
			expectedReason:    "GatewayTranslationFailed",
			expectedMessage:   "Translation failed due to error",
		},
		{
			name:              "Unknown type falls back to base reason",
			object:            &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"}},
			eventType:         corev1.EventTypeNormal,
			baseReason:        "StatusUpdateSucceeded",
			message:           "Status updated",
			expectedEventType: "Normal",
			expectedReason:    "StatusUpdateSucceeded",
			expectedMessage:   "Status updated",
		},
		{
			name:              "HTTPRoute with StateEnforcementSucceeded",
			object:            &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "test-route"}},
			eventType:         corev1.EventTypeNormal,
			baseReason:        "StateEnforcementSucceeded",
			message:           "State enforced",
			expectedEventType: "Normal",
			expectedReason:    "HTTPRouteStateEnforcementSucceeded",
			expectedMessage:   "State enforced",
		},
		{
			name:              "Gateway with OrphanCleanupFailed",
			object:            &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "test-gateway"}},
			eventType:         corev1.EventTypeWarning,
			baseReason:        "OrphanCleanupFailed",
			message:           "Cleanup failed",
			expectedEventType: "Warning",
			expectedReason:    "GatewayOrphanCleanupFailed",
			expectedMessage:   "Cleanup failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeRecorder := record.NewFakeRecorder(10)
			recorder := NewTypedEventRecorder(fakeRecorder)

			recorder.Event(tt.object, tt.eventType, tt.baseReason, tt.message)

			event := <-fakeRecorder.Events
			assert.Contains(t, event, tt.expectedEventType)
			assert.Contains(t, event, tt.expectedReason)
			assert.Contains(t, event, tt.expectedMessage)
		})
	}
}

func TestTypedEventRecorder_Eventf(t *testing.T) {
	tests := []struct {
		name              string
		object            runtime.Object
		eventType         string
		baseReason        string
		messageFmt        string
		args              []any
		expectedEventType string
		expectedReason    string
		expectedMessage   string
	}{
		{
			name:              "HTTPRoute with formatted message",
			object:            &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "default"}},
			eventType:         corev1.EventTypeNormal,
			baseReason:        "StateEnforcementSucceeded",
			messageFmt:        "Enforced state for %d resources",
			args:              []any{5},
			expectedEventType: "Normal",
			expectedReason:    "HTTPRouteStateEnforcementSucceeded",
			expectedMessage:   "Enforced state for 5 resources",
		},
		{
			name:              "Gateway with formatted message",
			object:            &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "test-gateway", Namespace: "default"}},
			eventType:         corev1.EventTypeWarning,
			baseReason:        "OrphanCleanupFailed",
			messageFmt:        "Failed to clean up %d orphaned resources",
			args:              []any{3},
			expectedEventType: "Warning",
			expectedReason:    "GatewayOrphanCleanupFailed",
			expectedMessage:   "Failed to clean up 3 orphaned resources",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeRecorder := record.NewFakeRecorder(10)
			recorder := NewTypedEventRecorder(fakeRecorder)

			recorder.Eventf(tt.object, tt.eventType, tt.baseReason, tt.messageFmt, tt.args...)

			event := <-fakeRecorder.Events
			assert.Contains(t, event, tt.expectedEventType)
			assert.Contains(t, event, tt.expectedReason)
			assert.Contains(t, event, tt.expectedMessage)
		})
	}
}

func TestTypedEventRecorder_AllBaseReasons(t *testing.T) {
	baseReasons := []string{
		"TranslationSucceeded",
		"TranslationFailed",
		"StatusUpdateSucceeded",
		"StatusUpdateFailed",
		"StateEnforcementSucceeded",
		"StateEnforcementFailed",
		"OrphanCleanupSucceeded",
		"OrphanCleanupFailed",
	}

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "test-route"},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gateway"},
	}

	for _, baseReason := range baseReasons {
		t.Run("HTTPRoute_"+baseReason, func(t *testing.T) {
			fakeRecorder := record.NewFakeRecorder(10)
			recorder := NewTypedEventRecorder(fakeRecorder)

			recorder.Event(httpRoute, corev1.EventTypeNormal, baseReason, "test message")

			event := <-fakeRecorder.Events
			assert.Contains(t, event, "HTTPRoute"+baseReason)
		})

		t.Run("Gateway_"+baseReason, func(t *testing.T) {
			fakeRecorder := record.NewFakeRecorder(10)
			recorder := NewTypedEventRecorder(fakeRecorder)

			recorder.Event(gateway, corev1.EventTypeNormal, baseReason, "test message")

			event := <-fakeRecorder.Events
			assert.Contains(t, event, "Gateway"+baseReason)
		})
	}
}

func TestTypedEventRecorder_getTypedReason(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	recorder := NewTypedEventRecorder(fakeRecorder)

	tests := []struct {
		name       string
		object     runtime.Object
		baseReason string
		expected   string
	}{
		{
			name:       "HTTPRoute with base reason",
			object:     &gatewayv1.HTTPRoute{},
			baseReason: "TranslationSucceeded",
			expected:   "HTTPRouteTranslationSucceeded",
		},
		{
			name:       "Gateway with base reason",
			object:     &gatewayv1.Gateway{},
			baseReason: "StatusUpdateFailed",
			expected:   "GatewayStatusUpdateFailed",
		},
		{
			name:       "Unknown type falls back to base reason",
			object:     &corev1.Pod{},
			baseReason: "SomeReason",
			expected:   "SomeReason",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := recorder.getTypedReason(tt.object, tt.baseReason)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTypedEventRecorder_MultipleEvents(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	recorder := NewTypedEventRecorder(fakeRecorder)

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "route1"},
	}
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gateway1"},
	}

	recorder.Event(httpRoute, corev1.EventTypeNormal, "TranslationSucceeded", "message1")
	recorder.Event(gateway, corev1.EventTypeWarning, "TranslationFailed", "message2")
	recorder.Eventf(httpRoute, corev1.EventTypeNormal, "StatusUpdateSucceeded", "message3")

	event1 := <-fakeRecorder.Events
	event2 := <-fakeRecorder.Events
	event3 := <-fakeRecorder.Events

	assert.Contains(t, event1, "HTTPRouteTranslationSucceeded")
	assert.Contains(t, event2, "GatewayTranslationFailed")
	assert.Contains(t, event3, "HTTPRouteStatusUpdateSucceeded")
}
