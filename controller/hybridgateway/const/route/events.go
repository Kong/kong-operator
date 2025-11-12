package route

// Event reasons for HTTPRoute operations in the hybrid gateway controller.
// These constants are used when recording Kubernetes events to provide
// consistent event reasons across the controller.

const (
	// EventReasonHTTPRouteTranslationSucceeded is used when HTTPRoute translation completes successfully.
	EventReasonHTTPRouteTranslationSucceeded = "HTTPRouteTranslationSucceeded"
	// EventReasonHTTPRouteTranslationFailed is used when HTTPRoute translation fails.
	EventReasonHTTPRouteTranslationFailed = "HTTPRouteTranslationFailed"

	// EventReasonHTTPRouteStatusUpdateSucceeded is used when HTTPRoute status is successfully updated.
	EventReasonHTTPRouteStatusUpdateSucceeded = "HTTPRouteStatusUpdateSucceeded"
	// EventReasonHTTPRouteStatusUpdateFailed is used when HTTPRoute status update fails.
	EventReasonHTTPRouteStatusUpdateFailed = "HTTPRouteStatusUpdateFailed"

	// EventReasonStateEnforcementSucceeded is used when Kong resources are successfully enforced.
	EventReasonStateEnforcementSucceeded = "StateEnforcementSucceeded"
	// EventReasonStateEnforcementFailed is used when Kong resource enforcement fails.
	EventReasonStateEnforcementFailed = "StateEnforcementFailed"

	// EventReasonOrphanCleanupSucceeded is used when orphaned Kong resources are successfully cleaned up.
	EventReasonOrphanCleanupSucceeded = "OrphanCleanupSucceeded"
	// EventReasonOrphanCleanupFailed is used when orphaned Kong resource cleanup fails.
	EventReasonOrphanCleanupFailed = "OrphanCleanupFailed"
)
