package events

// Event reasons for operations in the hybrid gateway controller.
// Base reasons are combined with resource type prefixes (HTTPRoute, Gateway, etc.)
// by the TypedEventRecorder to create specific event reasons.

const (
	// Base event reasons - these are prefixed with resource type by TypedEventRecorder

	// EventReasonTranslationSucceeded is used when resource translation completes successfully.
	EventReasonTranslationSucceeded = "TranslationSucceeded"
	// EventReasonTranslationFailed is used when resource translation fails.
	EventReasonTranslationFailed = "TranslationFailed"

	// EventReasonStatusUpdateSucceeded is used when resource status is successfully updated.
	EventReasonStatusUpdateSucceeded = "StatusUpdateSucceeded"
	// EventReasonStatusUpdateFailed is used when resource status update fails.
	EventReasonStatusUpdateFailed = "StatusUpdateFailed"

	// EventReasonStateEnforcementSucceeded is used when Kong resources are successfully enforced.
	EventReasonStateEnforcementSucceeded = "StateEnforcementSucceeded"
	// EventReasonStateEnforcementFailed is used when Kong resource enforcement fails.
	EventReasonStateEnforcementFailed = "StateEnforcementFailed"

	// EventReasonOrphanCleanupSucceeded is used when orphaned Kong resources are successfully cleaned up.
	EventReasonOrphanCleanupSucceeded = "OrphanCleanupSucceeded"
	// EventReasonOrphanCleanupFailed is used when orphaned Kong resource cleanup fails.
	EventReasonOrphanCleanupFailed = "OrphanCleanupFailed"
)
