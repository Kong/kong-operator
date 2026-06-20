package ops

import (
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/metrics"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	wrappersdkmocks "github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestGetIdentityProviderRequestForUID(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
	provider := testIdentityProviderRequest()

	sdk.On(
		"GetPortalIdentityProviders",
		mock.Anything,
		"portal-1",
		mock.MatchedBy(func(filter *sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter) bool {
			require.NotNil(t, filter)
			require.NotNil(t, filter.Type)
			require.NotNil(t, filter.Type.Eq)
			return *filter.Type.Eq == "oidc"
		}),
	).Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{
		IdentityProviders: []sdkkonnectcomp.IdentityProvider{
			{
				ID:        toPtr("idp-other"),
				Type:      toPtr(sdkkonnectcomp.IdentityProviderTypeSaml),
				Enabled:   toPtr(false),
				LoginPath: toPtr("/login"),
			},
			{
				ID:        toPtr("idp-1"),
				Type:      toPtr(sdkkonnectcomp.IdentityProviderTypeOidc),
				Enabled:   toPtr(false),
				LoginPath: toPtr("/login"),
				Config: &sdkkonnectcomp.IdentityProviderConfig{
					OIDCIdentityProviderConfigOutput: &sdkkonnectcomp.OIDCIdentityProviderConfigOutput{
						IssuerURL: "https://issuer.example.com",
						ClientID:  "client-id",
						Scopes:    []string{"email", "openid", "profile"},
						ClaimMappings: &sdkkonnectcomp.OIDCIdentityProviderClaimMappings{
							Email:  toPtr("email"),
							Groups: toPtr("groups"),
							Name:   toPtr("name"),
						},
					},
					Type: sdkkonnectcomp.IdentityProviderConfigTypeOIDCIdentityProviderConfigOutput,
				},
			},
		},
	}, nil).Once()

	id, err := getIdentityProviderRequestForUID(ctx, sdk, provider)
	require.NoError(t, err)
	assert.Equal(t, "idp-1", id)
}

func TestCreateAdoptsExistingIdentityProviderRequestOnConflict(t *testing.T) {
	ctx := t.Context()
	sdk := wrappersdkmocks.NewMockSDKWrapperWithT(t)
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	provider := testIdentityProviderRequest()

	expectedCreateRequest, err := provider.Spec.APISpec.ToCreateIdentityProvider()
	require.NoError(t, err)

	sdk.PortalAuthSettingsSDK.On(
		"CreatePortalIdentityProvider",
		mock.Anything,
		"portal-1",
		*expectedCreateRequest,
	).Return(nil, &sdkkonnecterrs.ConflictError{
		Status: 409,
		Detail: "conflict",
	}).Once()

	sdk.PortalAuthSettingsSDK.On(
		"GetPortalIdentityProviders",
		mock.Anything,
		"portal-1",
		mock.MatchedBy(func(filter *sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter) bool {
			require.NotNil(t, filter)
			require.NotNil(t, filter.Type)
			require.NotNil(t, filter.Type.Eq)
			return *filter.Type.Eq == "oidc"
		}),
	).Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{
		IdentityProviders: []sdkkonnectcomp.IdentityProvider{
			{
				ID:        toPtr("idp-1"),
				Type:      toPtr(sdkkonnectcomp.IdentityProviderTypeOidc),
				Enabled:   toPtr(false),
				LoginPath: toPtr("/login"),
				Config: &sdkkonnectcomp.IdentityProviderConfig{
					OIDCIdentityProviderConfigOutput: &sdkkonnectcomp.OIDCIdentityProviderConfigOutput{
						IssuerURL: "https://issuer.example.com",
						ClientID:  "client-id",
						Scopes:    []string{"email", "openid", "profile"},
						ClaimMappings: &sdkkonnectcomp.OIDCIdentityProviderClaimMappings{
							Email:  toPtr("email"),
							Groups: toPtr("groups"),
							Name:   toPtr("name"),
						},
					},
					Type: sdkkonnectcomp.IdentityProviderConfigTypeOIDCIdentityProviderConfigOutput,
				},
			},
		},
	}, nil).Once()

	_, err = Create(ctx, sdk, cl, noopMetricsRecorder{}, provider)
	require.NoError(t, err)
	assert.Equal(t, "idp-1", provider.GetKonnectID())
}

type noopMetricsRecorder struct{}

var _ metrics.Recorder = noopMetricsRecorder{}

func (noopMetricsRecorder) RecordKonnectEntityOperationSuccess(string, metrics.KonnectEntityOperation, string, time.Duration) {
}

func (noopMetricsRecorder) RecordKonnectEntityOperationFailure(string, metrics.KonnectEntityOperation, string, time.Duration, int) {
}

func testIdentityProviderRequest() *konnectv1alpha1.IdentityProviderRequest {
	provider := &konnectv1alpha1.IdentityProviderRequest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "IdentityProviderRequest",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "identity-provider",
			Namespace: "default",
			UID:       "identity-provider-uid",
		},
		Spec: konnectv1alpha1.IdentityProviderRequestSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "portal",
				},
			},
			APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
				Type:      konnectv1alpha1.IdentityProviderType("oidc"),
				Enabled:   konnectv1alpha1.IdentityProviderEnabledDisabled,
				LoginPath: "/login",
				Config: &konnectv1alpha1.IdentityProviderRequestConfig{
					Type: konnectv1alpha1.IdentityProviderRequestConfigTypeOIDC,
					OIDC: &konnectv1alpha1.OIDCIdentityProviderConfig{
						ClientID:     "client-id",
						ClientSecret: "top-secret",
						IssuerURL:    "https://issuer.example.com",
						Scopes:       []string{"email", "openid", "profile"},
						ClaimMappings: konnectv1alpha1.OIDCIdentityProviderClaimMappings{
							Email:  "email",
							Groups: "groups",
							Name:   "name",
						},
					},
				},
			},
		},
	}
	provider.SetPortalID("portal-1")
	return provider
}

func toPtr[T any](v T) *T {
	return &v
}
