package route

// TODO: move these conditions to the proper package

// Backends-related conditions for Routes
const (
	// ConditionTypeBackendsProgrammed is a condition type for Routes
	// that indicates whether all backends are programmed.
	ConditionTypeBackendsProgrammed = "BackendsProgrammed"
	// ConditionReasonBackendsProgrammed is a condition reason for Routes
	// that indicates all backends are programmed.
	ConditionReasonBackendsProgrammed = "BackendsProgrammed"
	// ConditionReasonBackendsNotProgrammed is a condition reason for Routes
	// that indicates not all backends are programmed.
	ConditionReasonBackendsNotProgrammed = "BackendsNotProgrammed"
)

// Routing-related conditions for Routes
const (
	// ConditionTypeRoutesProgrammed is a condition type for Routes
	// that indicates whether all routes are programmed to the gateway.
	ConditionTypeRoutesProgrammed = "RoutesProgrammed"
	// ConditionReasonRoutesProgrammed is a condition reason for Routes
	// that indicates all routes are programmed to the gateway.
	ConditionReasonRoutesProgrammed = "RoutesProgrammed"
	// ConditionReasonRoutesNotProgrammed is a condition reason for Routes
	// that indicates not all routes are programmed to the gateway.
	ConditionReasonRoutesNotProgrammed = "RoutesNotProgrammed"
)
