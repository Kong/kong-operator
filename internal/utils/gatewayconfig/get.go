package gatewayconfig

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// GetFromParametersRef gets GatewayConfiguration from parametersRef.
// It returns the GatewayConfiguration if the parametersRef points to an existing GatewayConfiguration.
// When the group/kind does not match, or the referenced GatewayConfiguration not found, it returns an error.
func GetFromParametersRef(
	ctx context.Context,
	reader client.Reader,
	parametersRef *gatewayv1.ParametersReference,
) (*gwtypes.GatewayConfiguration, error) {
	// No parametersRef means using default configuration.
	// Return an empty GatewayConfiguration object.
	if parametersRef == nil {
		return new(gwtypes.GatewayConfiguration), nil
	}

	if string(parametersRef.Group) != operatorv2beta1.SchemeGroupVersion.Group ||
		string(parametersRef.Kind) != "GatewayConfiguration" {
		return nil, &k8serrors.StatusError{
			ErrStatus: metav1.Status{
				Status: metav1.StatusFailure,
				Code:   http.StatusBadRequest,
				Reason: metav1.StatusReasonInvalid,
				Message: fmt.Sprintf("controller only supports %s/%s resources for GatewayClass parametersRef",
					operatorv2beta1.SchemeGroupVersion.Group, "GatewayConfiguration"),
				Details: &metav1.StatusDetails{
					Kind: string(parametersRef.Kind),
					Causes: []metav1.StatusCause{{
						Type: metav1.CauseTypeFieldValueNotSupported,
						Message: fmt.Sprintf("controller only supports %s/%s resources for GatewayClass parametersRef",
							operatorv2beta1.SchemeGroupVersion.Group, "GatewayConfiguration"),
					}},
				},
			},
		}
	}

	if parametersRef.Namespace == nil ||
		*parametersRef.Namespace == "" {
		return nil, errors.New("ParametersRef: namespace must be provided")
	}

	if parametersRef.Name == "" {
		return nil, errors.New("ParametersRef: name must be provided")
	}

	var (
		gatewayConfig gwtypes.GatewayConfiguration
		nn            = types.NamespacedName{
			Namespace: string(*parametersRef.Namespace),
			Name:      parametersRef.Name,
		}
	)

	if err := reader.Get(ctx, nn, &gatewayConfig); err != nil {
		return nil, err
	}
	return &gatewayConfig, nil
}

// IsGatewayHybrid returns true if the GatewayConfiguration specifies the gateway to be a Konnect hybrid gateway.
func IsGatewayHybrid(gwConfig *gwtypes.GatewayConfiguration) bool {
	return gwConfig.Spec.Konnect != nil && gwConfig.Spec.Konnect.APIAuthConfigurationRef != nil
}
