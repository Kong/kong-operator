package gatewayconfig

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestGetFromParametersRef(t *testing.T) {
	type testCase struct {
		name                   string
		parametersRef          *gwtypes.ParametersReference
		existingGatewayConfigs []client.Object
		expectedGatewayConfig  *operatorv2beta1.GatewayConfiguration
		expectedError          error
	}

	tests := []testCase{
		{
			name:          "parameters ref is nil",
			parametersRef: nil,
			existingGatewayConfigs: []client.Object{
				&operatorv2beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-config-1",
						Namespace: "default",
					},
				},
			},
			expectedGatewayConfig: new(operatorv2beta1.GatewayConfiguration),
			expectedError:         nil,
		},
		{
			name: "gateway config exists for parameters ref",
			parametersRef: &gwtypes.ParametersReference{
				Group:     "gateway-operator.konghq.com",
				Kind:      gwtypes.Kind("GatewayConfiguration"),
				Name:      "gateway-config-1",
				Namespace: new(gwtypes.Namespace("default")),
			},
			existingGatewayConfigs: []client.Object{
				&operatorv2beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "gateway-config-1",
						Namespace:       "default",
						ResourceVersion: "123",
					},
				},
			},
			expectedGatewayConfig: &operatorv2beta1.GatewayConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "gateway-config-1",
					Namespace:       "default",
					ResourceVersion: "123",
				},
			},
			expectedError: nil,
		},
		{
			name: "parameters ref group not match",
			parametersRef: &gwtypes.ParametersReference{
				Group:     "another.group.com",
				Kind:      gwtypes.Kind("GatewayConfiguration"),
				Name:      "gateway-config-1",
				Namespace: new(gwtypes.Namespace("default")),
			},
			expectedGatewayConfig: nil,
			expectedError: &k8serrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Code:   http.StatusBadRequest,
					Reason: metav1.StatusReasonInvalid,
					Message: fmt.Sprintf("controller only supports %s/%s resources for GatewayClass parametersRef",
						operatorv2beta1.SchemeGroupVersion.Group, "GatewayConfiguration"),
					Details: &metav1.StatusDetails{
						Kind: "GatewayConfiguration",
						Causes: []metav1.StatusCause{{
							Type: metav1.CauseTypeFieldValueNotSupported,
							Message: fmt.Sprintf("controller only supports %s/%s resources for GatewayClass parametersRef",
								operatorv2beta1.SchemeGroupVersion.Group, "GatewayConfiguration"),
						}},
					},
				},
			},
		},
		{
			name: "gateway config does not exist for parameters ref",
			parametersRef: &gwtypes.ParametersReference{
				Group:     "gateway-operator.konghq.com",
				Kind:      gwtypes.Kind("GatewayConfiguration"),
				Name:      "non-existing-gateway-config",
				Namespace: new(gwtypes.Namespace("default")),
			},
			existingGatewayConfigs: []client.Object{
				&operatorv2beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "gateway-config-1",
						Namespace:       "default",
						ResourceVersion: "123",
					},
				},
			},
			expectedGatewayConfig: nil,
			expectedError: &k8serrors.StatusError{
				ErrStatus: metav1.Status{
					Status:  metav1.StatusFailure,
					Code:    http.StatusNotFound,
					Reason:  metav1.StatusReasonNotFound,
					Message: "GatewayConfiguration.gateway-operator.konghq.com \"non-existing-gateway-config\" not found",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.existingGatewayConfigs...).
				Build()

			ctx := t.Context()
			gatewayConfig, err := GetFromParametersRef(ctx, cl, tc.parametersRef)
			// Check error matching
			if tc.expectedError != nil {
				assert.Error(t, err)
				// For NotFound errors, check specific fields
				if statusErr, ok := errors.AsType[*k8serrors.StatusError](tc.expectedError); ok {
					if actualStatusErr, ok := errors.AsType[*k8serrors.StatusError](err); ok {
						assert.Equal(t, statusErr.Status().Reason, actualStatusErr.Status().Reason)
					} else {
						t.Errorf("Expected StatusError, got %T", err)
					}
				} else {
					assert.Equal(t, tc.expectedError.Error(), err.Error())
				}
			} else {
				assert.NoError(t, err)
			}

			// Check returned gateway config
			assert.Equal(t, tc.expectedGatewayConfig, gatewayConfig)
		})
	}
}

func TestIsGatewayHybrid(t *testing.T) {
	require.NoError(t, konnectv1alpha2.AddToScheme(scheme.Get()))

	type testCase struct {
		name         string
		konnect      *operatorv2beta1.KonnectOptions
		expectHybrid bool
	}

	tests := []testCase{
		{
			name:         "no konnect configuration",
			konnect:      nil,
			expectHybrid: false,
		},
		{
			name: "konnect with source Origin and auth ref",
			konnect: &operatorv2beta1.KonnectOptions{
				Source: new(commonv1alpha1.EntitySourceOrigin),
				APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
					Name: "test-auth",
				},
			},
			expectHybrid: true,
		},
		{
			name: "konnect with source Origin but no auth ref",
			konnect: &operatorv2beta1.KonnectOptions{
				Source: new(commonv1alpha1.EntitySourceOrigin),
			},
			expectHybrid: false,
		},
		{
			name: "konnect with source Mirror and auth ref",
			konnect: &operatorv2beta1.KonnectOptions{
				Source: new(commonv1alpha1.EntitySourceMirror),
				APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
					Name: "test-auth",
				},
			},
			expectHybrid: true,
		},
		{
			name: "konnect with default source (Origin) and auth ref",
			konnect: &operatorv2beta1.KonnectOptions{
				APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
					Name: "test-auth",
				},
			},
			expectHybrid: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gatewayConfig := &operatorv2beta1.GatewayConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway-config",
					Namespace: "test-ns",
				},
				Spec: operatorv2beta1.GatewayConfigurationSpec{
					Konnect: tc.konnect,
				},
			}

			isHybrid := IsGatewayHybrid(gatewayConfig)
			assert.Equal(t, tc.expectHybrid, isHybrid)
		})
	}
}
