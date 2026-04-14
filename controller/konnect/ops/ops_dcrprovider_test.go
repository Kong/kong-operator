package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	xkonnectv1alpha1 "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestCreateDCRProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockDCRProvidersSDK(t)
	provider := testDCRProvider()

	expectedRequest, err := provider.Spec.APISpec.ToCreateDcrProviderRequest()
	require.NoError(t, err)
	require.NoError(t, setCreateDCRProviderRequestLabels(expectedRequest, WithKubernetesMetadataLabels(provider, createDCRProviderRequestLabels(expectedRequest))))

	sdk.On("CreateDcrProvider", mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.CreateDcrProviderResponse{
			CreateDcrProviderResponse: &sdkkonnectcomp.CreateDcrProviderResponse{
				DCRProviderAuth0: &sdkkonnectcomp.DCRProviderAuth0{ID: "provider-1"},
				Type:             sdkkonnectcomp.CreateDcrProviderResponseTypeDcrProviderAuth0,
			},
		}, nil).Once()

	err = createDCRProvider(ctx, sdk, provider)
	require.NoError(t, err)
	assert.Equal(t, "provider-1", provider.GetKonnectID())
}

func TestUpdateDCRProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockDCRProvidersSDK(t)
	provider := testDCRProvider()
	provider.SetKonnectID("provider-1")

	expectedRequest, err := provider.Spec.APISpec.ToUpdateDcrProviderRequest()
	require.NoError(t, err)
	expectedRequest.Labels = toOptionalStringMap(WithKubernetesMetadataLabels(provider, fromOptionalStringMap(expectedRequest.Labels)))

	sdk.On("UpdateDcrProvider", mock.Anything, "provider-1", *expectedRequest).
		Return(&sdkkonnectops.UpdateDcrProviderResponse{
			DcrProviderResponse: &sdkkonnectcomp.DcrProviderResponse{
				DCRProviderAuth0DCRProviderAuth0: &sdkkonnectcomp.DCRProviderAuth0DCRProviderAuth0{ID: "provider-1"},
				Type:                             sdkkonnectcomp.DcrProviderResponseTypeDcrProviderAuth0,
			},
		}, nil).Once()

	err = updateDCRProvider(ctx, sdk, provider)
	require.NoError(t, err)
	assert.Equal(t, "provider-1", provider.GetKonnectID())
}

func TestDeleteDCRProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockDCRProvidersSDK(t)
	provider := testDCRProvider()
	provider.SetKonnectID("provider-1")

	sdk.On("DeleteDcrProvider", mock.Anything, "provider-1").
		Return(&sdkkonnectops.DeleteDcrProviderResponse{}, nil).Once()

	err := deleteDCRProvider(ctx, sdk, provider)
	require.NoError(t, err)
}

func TestGetDCRProviderForUID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockDCRProvidersSDK(t)
	provider := testDCRProvider()

	nameContains := "auth0-dcr-provider"
	sdk.On("ListDcrProviders", mock.Anything, sdkkonnectops.ListDcrProvidersRequest{
		FilterNameContains: &nameContains,
	}).
		Return(&sdkkonnectops.ListDcrProvidersResponse{
			ListDcrProvidersResponse: &sdkkonnectcomp.ListDcrProvidersResponse{
				Data: []sdkkonnectcomp.DcrProviderResponse{
					{
						DCRProviderAuth0DCRProviderAuth0: &sdkkonnectcomp.DCRProviderAuth0DCRProviderAuth0{
							ID:     "provider-other",
							Labels: map[string]string{KubernetesUIDLabelKey: "different-uid"},
						},
						Type: sdkkonnectcomp.DcrProviderResponseTypeDcrProviderAuth0,
					},
					{
						DCRProviderAuth0DCRProviderAuth0: &sdkkonnectcomp.DCRProviderAuth0DCRProviderAuth0{
							ID:     "provider-1",
							Labels: map[string]string{KubernetesUIDLabelKey: string(provider.GetUID())},
						},
						Type: sdkkonnectcomp.DcrProviderResponseTypeDcrProviderAuth0,
					},
				},
			},
		}, nil).Once()

	id, err := getDCRProviderForUID(ctx, sdk, provider)
	require.NoError(t, err)
	assert.Equal(t, "provider-1", id)
}

func testDCRProvider() *xkonnectv1alpha1.DcrProvider {
	return &xkonnectv1alpha1.DcrProvider{
		TypeMeta: metav1.TypeMeta{
			APIVersion: xkonnectv1alpha1.GroupVersion.String(),
			Kind:       "DcrProvider",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "auth0-dcr-provider",
			Namespace:  "default",
			UID:        "dcr-provider-uid",
			Generation: 3,
		},
		Spec: xkonnectv1alpha1.DcrProviderSpec{
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
					Name: "test-auth",
				},
			},
			APISpec: xkonnectv1alpha1.DcrProviderAPISpec{
				DcrProviderConfig: &xkonnectv1alpha1.DcrProviderConfig{
					Type: xkonnectv1alpha1.DcrProviderConfigTypeAuth0,
					Auth0: &xkonnectv1alpha1.CreateDcrProviderRequestAuth0{
						DcrConfig: xkonnectv1alpha1.CreateDcrConfigAuth0InRequest{
							InitialClientID:     "client-id",
							InitialClientSecret: "client-secret",
						},
						DisplayName:  "Auth0 Provider",
						Issuer:       "https://example.com",
						Labels:       xkonnectv1alpha1.Labels{"team": "platform"},
						Name:         "auth0-dcr-provider",
						ProviderType: "auth0",
					},
				},
			},
		},
	}
}
