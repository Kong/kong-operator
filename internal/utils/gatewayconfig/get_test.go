package gatewayconfig

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestGetFromParamertersRef(t *testing.T) {
	type testCase struct {
		name                string
		objects             []client.Object
		parametersRef       *gatewayv1.ParametersReference
		expectErrorContains *string
	}

	tests := []testCase{
		{
			name:          "nil parametersRef should return a default GatewayConfiguration",
			parametersRef: nil,
		},
		{
			name: "Reference to existing GatewayConfiguration",
			objects: []client.Object{
				&operatorv2beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test",
						Name:      "test",
					},
				},
			},
			parametersRef: &gatewayv1.ParametersReference{
				Group:     gatewayv1.Group(operatorv2beta1.SchemeGroupVersion.Group),
				Kind:      "GatewayConfiguration",
				Name:      "test",
				Namespace: lo.ToPtr(gatewayv1.Namespace("test")),
			},
		},
		{
			name: "Group is not supported",
			parametersRef: &gatewayv1.ParametersReference{
				Group:     gatewayv1.Group("other.group"),
				Kind:      "GatewayConfiguration",
				Name:      "test",
				Namespace: lo.ToPtr(gatewayv1.Namespace("test")),
			},
			expectErrorContains: lo.ToPtr(
				fmt.Sprintf("controller only supports %s/%s resources for GatewayClass parametersRef",
					operatorv2beta1.SchemeGroupVersion.Group, "GatewayConfiguration"),
			),
		},
		{
			name: "Namespace is missing",
			parametersRef: &gatewayv1.ParametersReference{
				Group: gatewayv1.Group(operatorv2beta1.SchemeGroupVersion.Group),
				Kind:  "GatewayConfiguration",
				Name:  "test",
			},
			expectErrorContains: lo.ToPtr("ParametersRef: namespace must be provided"),
		},
		{
			name: "Name is missing",
			parametersRef: &gatewayv1.ParametersReference{
				Group:     gatewayv1.Group(operatorv2beta1.SchemeGroupVersion.Group),
				Kind:      "GatewayConfiguration",
				Name:      "",
				Namespace: lo.ToPtr(gatewayv1.Namespace("test")),
			},
			expectErrorContains: lo.ToPtr("ParametersRef: name must be provided"),
		},
		{
			name: "referenced GatewayConfiguration not found",
			parametersRef: &gatewayv1.ParametersReference{
				Group:     gatewayv1.Group(operatorv2beta1.SchemeGroupVersion.Group),
				Kind:      "GatewayConfiguration",
				Name:      "test",
				Namespace: lo.ToPtr(gatewayv1.Namespace("test")),
			},
			expectErrorContains: lo.ToPtr("gatewayconfigurations.gateway-operator.konghq.com \"test\" not found"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get()).WithObjects(tc.objects...).Build()
			GetFromParametersRef(t.Context(), cl, tc.parametersRef)

			_, err := GetFromParametersRef(t.Context(), cl, tc.parametersRef)
			if tc.expectErrorContains == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), *tc.expectErrorContains, "Error message should contain expected content")
			}
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
				Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
				APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
					Name: "test-auth",
				},
			},
			expectHybrid: true,
		},
		{
			name: "konnect with source Origin but no auth ref",
			konnect: &operatorv2beta1.KonnectOptions{
				Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
			},
			expectHybrid: false,
		},
		{
			name: "konnect with source Mirror and auth ref",
			konnect: &operatorv2beta1.KonnectOptions{
				Source: lo.ToPtr(commonv1alpha1.EntitySourceMirror),
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
