package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestGetPortalIPAllowListForUID_MatchesAllowedIPs(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := mocks.NewMockPortalsIPAllowListSDK(t)
	ipAllowList := testGeneratedPortalIPAllowListForGetForUID()
	ipAllowList.SetPortalID("portal-1")

	sdk.EXPECT().
		ListPortalIPAllowList(mock.Anything, sdkkonnectops.ListPortalIPAllowListRequest{
			PortalID: "portal-1",
		}).
		Return(&sdkkonnectops.ListPortalIPAllowListResponse{
			PortalSourceIPRestrictionPaginatedResponse: &sdkkonnectcomp.PortalSourceIPRestrictionPaginatedResponse{
				Data: []sdkkonnectcomp.IPEntry{
					{
						ID:         "allow-list-other",
						AllowedIps: []string{"10.0.1.0/24"},
					},
					{
						ID:         "allow-list-1",
						AllowedIps: []string{"10.0.0.0/24", "2001:db8::/32"},
					},
				},
			},
		}, nil).
		Once()

	id, err := getPortalIPAllowListForUID(ctx, sdk, ipAllowList)
	require.NoError(t, err)
	assert.Equal(t, "allow-list-1", id)
}

func testGeneratedPortalIPAllowListForGetForUID() *konnectv1alpha1.PortalIPAllowList {
	return &konnectv1alpha1.PortalIPAllowList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "PortalIPAllowList",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "portal-ip-allow-list",
			Namespace: "default",
			UID:       "portal-ip-allow-list-uid",
		},
		Spec: konnectv1alpha1.PortalIPAllowListSpec{
			APISpec: konnectv1alpha1.PortalIPAllowListAPISpec{
				AllowedIps: []string{"10.0.0.0/24", "2001:db8::/32"},
			},
		},
	}
}
