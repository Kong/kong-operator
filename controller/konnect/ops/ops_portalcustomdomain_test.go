package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
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
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "PortalCustomDomain",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "portal-custom-domain",
			Namespace:  "default",
			UID:        "portal-custom-domain-uid",
			Generation: 2,
		},
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
