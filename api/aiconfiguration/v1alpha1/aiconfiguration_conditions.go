package v1alpha1

const (
	// AIGatewayConsumerGroupRefsValidConditionType is the type of the condition that
	// indicates whether the AIGatewayConsumerGroups referenced by an AIGatewayConsumer
	// are valid and all point to existing, Programmed AIGatewayConsumerGroups.
	AIGatewayConsumerGroupRefsValidConditionType = "ConsumerGroupRefsValid"

	// AIGatewayConsumerGroupRefsReasonValid is the reason used with the
	// ConsumerGroupRefsValid condition type indicating that all AIGatewayConsumerGroup
	// references are valid.
	AIGatewayConsumerGroupRefsReasonValid = "Valid"
	// AIGatewayConsumerGroupRefsReasonInvalid is the reason used with the
	// ConsumerGroupRefsValid condition type indicating that one or more
	// AIGatewayConsumerGroup references are invalid.
	AIGatewayConsumerGroupRefsReasonInvalid = "Invalid"
)
