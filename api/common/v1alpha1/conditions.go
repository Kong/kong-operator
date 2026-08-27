package v1alpha1

const (
	// ConsumerGroupRefsValidConditionType is the type of the condition that indicates
	// whether the ConsumerGroups referenced by the entity are valid and all point to
	// existing ConsumerGroups.
	ConsumerGroupRefsValidConditionType = "ConsumerGroupRefsValid"

	// ConsumerGroupRefsReasonValid is the reason used with the ConsumerGroupRefsValid
	// condition type indicating that all ConsumerGroup references are valid.
	ConsumerGroupRefsReasonValid = "Valid"
	// ConsumerGroupRefsReasonInvalid is the reason used with the ConsumerGroupRefsValid
	// condition type indicating that one or more ConsumerGroup references are invalid.
	ConsumerGroupRefsReasonInvalid = "Invalid"
)
