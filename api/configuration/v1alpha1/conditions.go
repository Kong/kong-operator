package v1alpha1

const (
	// KongReferenceGrantConditionType is the condition type used to indicate
	// whether a KongReferenceGrant is valid for cross-namespace references.
	KongReferenceGrantConditionType = "KongReferenceGrantValid"

	// KongReferenceGrantReasonInvalid is the reason used when a KongReferenceGrant
	// is invalid or missing for a cross-namespace reference.
	KongReferenceGrantReasonInvalid = "KongReferenceGrantInvalid"
	// KongReferenceGrantReasonValid is the reason used when a KongReferenceGrant
	// is valid for a cross-namespace reference.
	KongReferenceGrantReasonValid = "KongReferenceGrantValid"
)
