package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// AIGateway API - Conditions
// -----------------------------------------------------------------------------

const (
	// AIGatewayConditionTypeAccepted indicates whether or not the controller
	// responsible for this AIGateway has accepted responsibility for the
	// resource and whether active work is underway.
	//
	// Possible reasons for this condition to be "True" include:
	//
	//   - "Accepted"
	//
	// Possible reasons for this condition to be "False" include:
	//
	//   - "Pending"
	//   - "Rejected"
	//
	// "Accepted" is not a terminal condition, if for instance the spec is
	// changed to switch to another (different) GatewayClass cleanup will be
	// performed by the previous GatewayClass and the condition will be set back
	// to "Pending".
	AIGatewayConditionTypeAccepted string = "Accepted"

	// AIGatewayConditionTypeProvisioning indicates whether the controller is
	// actively provisioning the AIGateway, including creating and configuring
	// any necessary resources.
	//
	// Possible reasons for this condition to be "True" include:
	//
	//   - "Deploying"
	//
	// Possible reasons for this condition to be "False" include:
	//
	//   - "Deployed"
	//   - "Failed"
	//
	// If this remains "True" for prolonged periods of time, it may indicate a
	// problem with the controller. Check the most recent status updates and
	// compare them with the controller logs for more details on what may have
	// gone wrong.
	AIGatewayConditionTypeProvisioning string = "Provisioning"

	// AIGatewayConditionTypeEndpointReady indicates whether an endpoint has
	// been provisioned and is ready for inference.
	//
	// Possible reasons for this condition to be "True" include:
	//
	//   - "Deployed"
	//
	// Possible reasons for this condition to be "False" include:
	//
	//   - "Deploying"
	//   - "Failed"
	//
	// While provisioning, some endpoints may be active and ready while others
	// are still being provisioned, or may have failed. Check the AIGateway's
	// status to see the individual status of each endpoint.
	//
	// If this remains "False" for prolonged periods of time, look at the
	// condition's message to determine which endpoints are having trouble.
	// These can include helpful information about what went wrong, e.g.:
	//
	//   - "endpoint foo error: connection refused to https://api.openai.com"
	//
	// You may want to reference the controller logs as well for additional
	// details about what may have gone wrong.
	AIGatewayConditionTypeEndpointReady string = "EndpointReady"
)

// -----------------------------------------------------------------------------
// AIGateway API - Conditions - "Accepted" Reasons
// -----------------------------------------------------------------------------

const (
	// AIGatewayConditionReasonAccepted indicates that the controller has
	// accepted responsibility for the AIGateway.
	AIGatewayConditionReasonAccepted string = "Accepted"
)

const (
	// AIGatewayConditionReasonPending indicates that the controller has not yet
	// taken responsibility for the AIGateway.
	//
	// If a resource remains in a prolonged "Pending" state, it may indicate a
	// misconfiguration of the resource (e.g. wrong GatewayClassName) or a
	// problem with the controller. Check the controller logs for more details.
	AIGatewayConditionReasonPending string = "Pending"

	// AIGatewayConditionReasonRejected indicates that the controller has
	// rejected responsibility of the AIGateway.
	//
	// This state requires external intervention to resolve. Check all
	// conditions, condition messages, and perhaps controller logs for help
	// determining the cause.
	AIGatewayConditionReasonRejected string = "Rejected"
)

// -----------------------------------------------------------------------------
// AIGateway API - Conditions - "Provisioning" Reasons
// -----------------------------------------------------------------------------

const (
	// AIGatewayConditionReasonDeploying indicates that the controller is
	// actively working on deployment.
	//
	// This is a transient condition and should not remain "True" for prolonged
	// periods of time.
	//
	// If this condition remains "True" for a prolonged period of time, check
	// all conditions, condition messages, and perhaps controller logs for help
	// determining the cause.
	AIGatewayConditionReasonDeploying string = "Deploying"
)

const (
	// AIGatewayConditionReasonDeployed indicates that the controller has
	// completed provisioning.
	//
	// This is not a terminal condition, updates to the specification may
	// trigger new provisioning work.
	AIGatewayConditionReasonDeployed string = "Deployed"

	// AIGatewayConditionReasonFailed indicates that the controller has
	// failed to provision the AIGateway.
	//
	// This is a terminal condition and requires external intervention to
	// resolve. Check all conditions, condition messages, and perhaps controller
	// logs for help finding the cause.
	AIGatewayConditionReasonFailed string = "Failed"
)

// -----------------------------------------------------------------------------
// AIGateway - ConditionsAware Implementation
// -----------------------------------------------------------------------------

// GetConditions returns the status conditions.
func (a *AIGateway) GetConditions() []metav1.Condition {
	return a.Status.Conditions
}

// SetConditions sets the status conditions.
func (a *AIGateway) SetConditions(conditions []metav1.Condition) {
	a.Status.Conditions = conditions
}
