package v1alpha1

const (
	// KongReferenceGrantConditionTypeResolvedRefs is the condition type used to indicate
	// whether a KongReferenceGrant is valid for cross-namespace references.
	KongReferenceGrantConditionTypeResolvedRefs = "ResolvedRefs"

	// KongReferenceGrantReasonRefNotPermitted is the reason used when a KongReferenceGrant
	// is invalid or missing for a cross-namespace reference.
	KongReferenceGrantReasonRefNotPermitted = "RefNotPermitted"
	// KongReferenceGrantReasonResolvedRefs is the reason used when a valid
	// KongReferenceGrant is found and it permits for a cross-namespace reference.
	KongReferenceGrantReasonResolvedRefs = "RefNotPermitted"
)
