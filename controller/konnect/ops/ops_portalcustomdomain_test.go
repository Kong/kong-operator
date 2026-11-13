package ops

import (
	"net/http"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	opsdkmocks "github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestCreatePortalCustomDomain(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalCustomDomainsSDK(t)
	domain := testPortalCustomDomain()

	expectedRequest, err := domain.Spec.APISpec.ToCreatePortalCustomDomainRequest()
	require.NoError(t, err)

	sdk.EXPECT().
		CreatePortalCustomDomain(mock.Anything, "portal-1", *expectedRequest).
		Return(&sdkkonnectops.CreatePortalCustomDomainResponse{
			PortalCustomDomain: &sdkkonnectcomp.PortalCustomDomain{
				Hostname: domain.Spec.APISpec.Hostname,
			},
		}, nil).
		Once()

	require.NoError(t, createPortalCustomDomain(ctx, sdk, domain))
	assert.Empty(t, domain.GetKonnectID())
}

// TestCreatePortalCustomDomainConflictRecovery verifies that shared Create still
// recovers singleton custom domains by hostname without requiring ID persistence.
func TestCreatePortalCustomDomainConflictRecovery(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		hostname   string
		programmed metav1.ConditionStatus
		id         string
	}{
		{
			name:       "matching existing domain recovers",
			hostname:   "developer.example.com",
			programmed: metav1.ConditionTrue,
			id:         "developer.example.com",
		},
		{
			name:       "different existing domain is not adopted",
			hostname:   "other.example.com",
			programmed: metav1.ConditionFalse,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			domain := testPortalCustomDomain()
			require.False(t, domain.PersistsKonnectID())
			sdk := opsdkmocks.NewMockSDKWrapperWithT(t)
			expectedRequest, err := domain.Spec.APISpec.ToCreatePortalCustomDomainRequest()
			require.NoError(t, err)
			sdk.PortalCustomDomainsSDK.EXPECT().
				CreatePortalCustomDomain(mock.Anything, "portal-1", *expectedRequest).
				Return(nil, &sdkkonnecterrs.SDKError{StatusCode: http.StatusConflict, Message: "custom domain already exists"}).
				Once()
			sdk.PortalCustomDomainsSDK.EXPECT().
				GetPortalCustomDomain(mock.Anything, "portal-1").
				Return(&sdkkonnectops.GetPortalCustomDomainResponse{
					PortalCustomDomain: &sdkkonnectcomp.PortalCustomDomain{Hostname: tt.hostname},
				}, nil).
				Once()

			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
			_, err = Create(t.Context(), sdk, cl, &metricsmocks.MockRecorder{}, domain)
			require.NoError(t, err)
			programmed := apimeta.FindStatusCondition(domain.Status.Conditions, "Programmed")
			require.NotNil(t, programmed)
			assert.Equal(t, tt.programmed, programmed.Status)
			assert.Equal(t, tt.id, domain.GetKonnectID())
		})
	}
}

func TestUpdatePortalCustomDomain(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalCustomDomainsSDK(t)
	domain := testPortalCustomDomain()

	expectedRequest, err := domain.Spec.APISpec.ToUpdatePortalCustomDomainRequest()
	require.NoError(t, err)

	sdk.EXPECT().
		UpdatePortalCustomDomain(mock.Anything, "portal-1", *expectedRequest).
		Return(&sdkkonnectops.UpdatePortalCustomDomainResponse{}, nil).
		Once()

	require.NoError(t, updatePortalCustomDomain(ctx, sdk, domain))
}

func TestDeletePortalCustomDomain(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalCustomDomainsSDK(t)
	domain := testPortalCustomDomain()

	sdk.EXPECT().
		DeletePortalCustomDomain(mock.Anything, "portal-1").
		Return(&sdkkonnectops.DeletePortalCustomDomainResponse{}, nil).
		Once()

	require.NoError(t, deletePortalCustomDomain(ctx, sdk, domain))
}

func TestPortalCustomDomainPersistsKonnectID(t *testing.T) {
	t.Parallel()

	assert.False(t, EntityPersistsKonnectID(&konnectv1alpha1.PortalCustomDomain{}))
	assert.True(t, EntityPersistsKonnectID(&konnectv1alpha1.Portal{}))
}

func TestGetPortalCustomDomainForUID(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalCustomDomainsSDK(t)
	domain := testPortalCustomDomain()

	sdk.EXPECT().
		GetPortalCustomDomain(mock.Anything, "portal-1").
		Return(&sdkkonnectops.GetPortalCustomDomainResponse{
			PortalCustomDomain: &sdkkonnectcomp.PortalCustomDomain{
				Hostname: domain.Spec.APISpec.Hostname,
			},
		}, nil).
		Once()

	id, err := getPortalCustomDomainForUID(ctx, sdk, domain)
	require.NoError(t, err)
	assert.Equal(t, domain.Spec.APISpec.Hostname, id)
}

func TestGetPortalCustomDomainForUIDNotFound(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalCustomDomainsSDK(t)
	domain := testPortalCustomDomain()

	sdk.EXPECT().
		GetPortalCustomDomain(mock.Anything, "portal-1").
		Return(&sdkkonnectops.GetPortalCustomDomainResponse{
			PortalCustomDomain: &sdkkonnectcomp.PortalCustomDomain{
				Hostname: "other.example.com",
			},
		}, nil).
		Once()

	id, err := getPortalCustomDomainForUID(ctx, sdk, domain)
	require.Empty(t, id)

	var notFoundErr EntityWithMatchingUIDNotFoundError
	require.ErrorAs(t, err, &notFoundErr)
}

func testPortalCustomDomain() *konnectv1alpha1.PortalCustomDomain {
	return &konnectv1alpha1.PortalCustomDomain{
		APIVersion: konnectv1alpha1.GroupVersion.String(),
		Kind:       "PortalCustomDomain",
		Name:       "portal-custom-domain",
		Namespace:  "default",
		UID:        "portal-custom-domain-uid",
		Generation: 2,
		Spec: konnectv1alpha1.PortalCustomDomainSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "portal",
				},
			},
			APISpec: konnectv1alpha1.PortalCustomDomainAPISpec{
				Enabled:  "Enabled",
				Hostname: "developer.example.com",
				SSL: &konnectv1alpha1.PortalCustomDomainSSL{
					Type: konnectv1alpha1.PortalCustomDomainSSLTypeStandard,
					Standard: &konnectv1alpha1.CreatePortalCustomDomainSSLStandard{
						DomainVerificationMethod: "http",
					},
				},
			},
		},
		Status: konnectv1alpha1.PortalCustomDomainStatus{
			PortalID: &konnectv1alpha1.KonnectEntityRef{
				ID: "portal-1",
			},
		},
	}
}
