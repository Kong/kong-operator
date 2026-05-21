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

func TestCreatePortalTeam(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalTeamsSDK(t)
	team := testPortalTeam()

	expectedRequest, err := team.Spec.APISpec.ToPortalCreateTeamRequest()
	require.NoError(t, err)

	sdk.EXPECT().
		CreatePortalTeam(mock.Anything, "portal-1", expectedRequest).
		Return(&sdkkonnectops.CreatePortalTeamResponse{
			PortalTeamResponse: &sdkkonnectcomp.PortalTeamResponse{
				ID: new("portal-team-1"),
			},
		}, nil).
		Once()

	require.NoError(t, createPortalTeam(ctx, sdk, team))
	assert.Equal(t, "portal-team-1", team.GetKonnectID())
}

func TestUpdatePortalTeam(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalTeamsSDK(t)
	team := testPortalTeam()
	team.SetKonnectID("portal-team-1")

	expectedRequest, err := team.Spec.APISpec.ToPortalUpdateTeamRequest()
	require.NoError(t, err)

	sdk.EXPECT().
		UpdatePortalTeam(mock.Anything, sdkkonnectops.UpdatePortalTeamRequest{
			PortalID:                "portal-1",
			TeamID:                  "portal-team-1",
			PortalUpdateTeamRequest: expectedRequest,
		}).
		Return(&sdkkonnectops.UpdatePortalTeamResponse{}, nil).
		Once()

	require.NoError(t, updatePortalTeam(ctx, sdk, team))
}

func TestDeletePortalTeam(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalTeamsSDK(t)
	team := testPortalTeam()
	team.SetKonnectID("portal-team-1")

	sdk.EXPECT().
		DeletePortalTeam(mock.Anything, "portal-1", "portal-team-1").
		Return(&sdkkonnectops.DeletePortalTeamResponse{}, nil).
		Once()

	require.NoError(t, deletePortalTeam(ctx, sdk, team))
}

func TestGetPortalTeamForUID(t *testing.T) {
	t.Run("matches portal team by configured fields", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalTeamsSDK(t)
		team := testPortalTeam()

		sdk.EXPECT().
			ListPortalTeams(mock.Anything, sdkkonnectops.ListPortalTeamsRequest{
				PortalID: "portal-1",
			}).
			Return(&sdkkonnectops.ListPortalTeamsResponse{
				ListPortalTeamsResponse: &sdkkonnectcomp.ListPortalTeamsResponse{
					Data: []sdkkonnectcomp.PortalTeamResponse{
						{
							ID:          new("portal-team-1"),
							Name:        new("Developers"),
							Description: new("Portal developers"),
						},
					},
				},
			}, nil).
			Once()

		id, err := getPortalTeamForUID(ctx, sdk, team)
		require.NoError(t, err)
		require.Equal(t, "portal-team-1", id)
	})

	t.Run("does not match when name differs", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalTeamsSDK(t)
		team := testPortalTeam()

		sdk.EXPECT().
			ListPortalTeams(mock.Anything, sdkkonnectops.ListPortalTeamsRequest{
				PortalID: "portal-1",
			}).
			Return(&sdkkonnectops.ListPortalTeamsResponse{
				ListPortalTeamsResponse: &sdkkonnectcomp.ListPortalTeamsResponse{
					Data: []sdkkonnectcomp.PortalTeamResponse{
						{
							ID:   new("portal-team-1"),
							Name: new("Other team"),
						},
					},
				},
			}, nil).
			Once()

		id, err := getPortalTeamForUID(ctx, sdk, team)
		require.Empty(t, id)

		var notFoundErr EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFoundErr)
	})

	t.Run("matches portal team regardless of description drift", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalTeamsSDK(t)
		team := testPortalTeam()

		sdk.EXPECT().
			ListPortalTeams(mock.Anything, sdkkonnectops.ListPortalTeamsRequest{
				PortalID: "portal-1",
			}).
			Return(&sdkkonnectops.ListPortalTeamsResponse{
				ListPortalTeamsResponse: &sdkkonnectcomp.ListPortalTeamsResponse{
					Data: []sdkkonnectcomp.PortalTeamResponse{
						{
							ID:          new("portal-team-1"),
							Name:        new("Developers"),
							Description: new("Drifted description"),
						},
					},
				},
			}, nil).
			Once()

		id, err := getPortalTeamForUID(ctx, sdk, team)
		require.NoError(t, err)
		require.Equal(t, "portal-team-1", id)
	})
}

func testPortalTeam() *konnectv1alpha1.PortalTeam {
	return &konnectv1alpha1.PortalTeam{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "PortalTeam",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "portal-team",
			Namespace:  "default",
			UID:        "portal-team-uid",
			Generation: 2,
		},
		Spec: konnectv1alpha1.PortalTeamSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "portal",
				},
			},
			APISpec: konnectv1alpha1.PortalTeamAPISpec{
				Name:        "Developers",
				Description: "Portal developers",
			},
		},
		Status: konnectv1alpha1.PortalTeamStatus{
			PortalID: &konnectv1alpha1.KonnectEntityRef{
				ID: "portal-1",
			},
		},
	}
}
