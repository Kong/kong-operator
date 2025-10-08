package route

// Condition types and reasons for Kong resources programmed status on Routes.
// For each resource, if any related resource is not programmed, the corresponding condition
// will be set to False with a reason indicating which resource is not set up as expected.

const (
	// ConditionTypeKongRouteProgrammed is set on a Route to indicate whether its KongRoute resource is programmed.
	ConditionTypeKongRouteProgrammed = "KongRouteProgrammed"
	// ConditionReasonKongRouteProgrammed is used when the KongRoute resource is successfully programmed.
	ConditionReasonKongRouteProgrammed = "KongRouteProgrammed"
	// ConditionReasonKongRouteNotProgrammed is used when the KongRoute resource is not programmed.
	ConditionReasonKongRouteNotProgrammed = "KongRouteNotProgrammed"

	// ConditionTypeKongServiceProgrammed is set on a Route to indicate whether its KongService resource is programmed.
	ConditionTypeKongServiceProgrammed = "KongServiceProgrammed"
	// ConditionReasonKongServiceProgrammed is used when the KongService resource is successfully programmed.
	ConditionReasonKongServiceProgrammed = "KongServiceProgrammed"
	// ConditionReasonKongServiceNotProgrammed is used when the KongService resource is not programmed.
	ConditionReasonKongServiceNotProgrammed = "KongServiceNotProgrammed"

	// ConditionTypeKongTargetProgrammed is set on a Route to indicate whether its KongTarget resource is programmed.
	ConditionTypeKongTargetProgrammed = "KongTargetProgrammed"
	// ConditionReasonKongTargetProgrammed is used when the KongTarget resource is successfully programmed.
	ConditionReasonKongTargetProgrammed = "KongTargetProgrammed"
	// ConditionReasonKongTargetNotProgrammed is used when the KongTarget resource is not programmed.
	ConditionReasonKongTargetNotProgrammed = "KongTargetNotProgrammed"

	// ConditionTypeKongUpstreamProgrammed is set on a Route to indicate whether its KongUpstream resource is programmed.
	ConditionTypeKongUpstreamProgrammed = "KongUpstreamProgrammed"
	// ConditionReasonKongUpstreamProgrammed is used when the KongUpstream resource is successfully programmed.
	ConditionReasonKongUpstreamProgrammed = "KongUpstreamProgrammed"
	// ConditionReasonKongUpstreamNotProgrammed is used when the KongUpstream resource is not programmed.
	ConditionReasonKongUpstreamNotProgrammed = "KongUpstreamNotProgrammed"

	// ConditionTypeKongPluginBindingProgrammed is set on a Route to indicate whether its KongPluginBinding resource is programmed.
	ConditionTypeKongPluginBindingProgrammed = "KongPluginBindingProgrammed"
	// ConditionReasonKongPluginBindingProgrammed is used when the KongPluginBinding resource is successfully programmed.
	ConditionReasonKongPluginBindingProgrammed = "KongPluginBindingProgrammed"
	// ConditionReasonKongPluginBindingNotProgrammed is used when the KongPluginBinding resource is not programmed.
	ConditionReasonKongPluginBindingNotProgrammed = "KongPluginBindingNotProgrammed"
)
