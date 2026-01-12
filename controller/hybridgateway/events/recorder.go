package events

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// TypedEventRecorder wraps a record.EventRecorder and provides type-specific event reasons
// based on the object type being reconciled (HTTPRoute, Gateway, etc.).
type TypedEventRecorder struct {
	recorder record.EventRecorder
}

// NewTypedEventRecorder creates a new TypedEventRecorder that wraps the provided event recorder.
func NewTypedEventRecorder(recorder record.EventRecorder) *TypedEventRecorder {
	return &TypedEventRecorder{
		recorder: recorder,
	}
}

// Event records an event with a type-specific reason based on the object's kind.
func (r *TypedEventRecorder) Event(object runtime.Object, eventtype, baseReason, message string) {
	reason := r.getTypedReason(object, baseReason)
	r.recorder.Event(object, eventtype, reason, message)
}

// Eventf is like Event, but uses [fmt.Sprintf] to construct the message.
func (r *TypedEventRecorder) Eventf(object runtime.Object, eventtype, baseReason, messageFmt string, args ...any) {
	reason := r.getTypedReason(object, baseReason)
	r.recorder.Eventf(object, eventtype, reason, messageFmt, args...)
}

// getTypedReason returns a type-specific event reason based on the object's kind.
// Base reasons:
//   - TranslationSucceeded/TranslationFailed
//   - StatusUpdateSucceeded/StatusUpdateFailed
//   - StateEnforcementSucceeded/StateEnforcementFailed
//   - OrphanCleanupSucceeded/OrphanCleanupFailed
func (r *TypedEventRecorder) getTypedReason(object runtime.Object, baseReason string) string {
	switch object.(type) {
	case *gatewayv1.HTTPRoute:
		return fmt.Sprintf("HTTPRoute%s", baseReason)
	case *gatewayv1.Gateway:
		return fmt.Sprintf("Gateway%s", baseReason)
	default:
		// Fallback to base reason if type is unknown
		return baseReason
	}
}
