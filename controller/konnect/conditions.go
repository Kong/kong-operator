package konnect

// TODO(pmalek): move this to Konnect API directory so that it's part of the API contract.

const (
	// KonnectEntityProgrammedConditionType is the type of the condition that
	// indicates whether the entity has been programmed in Konnect.
	KonnectEntityProgrammedConditionType = "Programmed"

	// KonnectEntityProgrammedReason is the reason for the Programmed condition.
	// It is set when the entity has been programmed in Konnect.
	KonnectEntityProgrammedReason = "Programmed"
)

const (
	// KonnectAPIAuthConfigurationValidConditionType is the type of the condition
	// that indicates whether the APIAuth configuration is valid.
	KonnectAPIAuthConfigurationValidConditionType = "Valid"

	// KonnectAPIAuthConfigurationReasonValid is the reason used with the Valid
	// condition type indiciating that the APIAuth configuration is valid.
	KonnectAPIAuthConfigurationReasonValid = "Valid"
	// KonnectAPIAuthConfigurationReasonValid is the reason used with the Valid
	// condition type indiciating that the APIAuth configuration is invalid.
	KonnectAPIAuthConfigurationReasonInvalid = "Invalid"
)

const (
	// KonnectEntityAPIAuthConfigurationRefValidConditionType is the type of the
	// condition that indicates whether the APIAuth configuration reference is
	// valid and points to an existing, valid APIAuth configuration.
	KonnectEntityAPIAuthConfigurationRefValidConditionType = "APIAuthRefValid"

	// KonnectEntityAPIAuthConfigurationRefReasonValid is the reason used with the
	// APIAuthRefValid condition type indicating that the APIAuth configuration
	// referenced by the entity is valid.
	KonnectEntityAPIAuthConfigurationRefReasonValid = "Valid"
	// KonnectEntityAPIAuthConfigurationRefReasonValid is the reason used with the
	// APIAuthRefValid condition type indicating that the APIAuth configuration
	// referenced by the entity is invalid.
	KonnectEntityAPIAuthConfigurationRefReasonInvalid = "Invalid"
)
