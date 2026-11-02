package ops

import (
	"errors"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

func testAIGatewayDatastore() *aiconfigurationv1alpha1.AIGatewayDatastore {
	return &aiconfigurationv1alpha1.AIGatewayDatastore{
		Name:      "aigatewaydatastore",
		Namespace: "default",
		UID:       "aigatewaydatastore-uid",
		Spec: aiconfigurationv1alpha1.AIGatewayDatastoreSpec{
			APISpec: aiconfigurationv1alpha1.AIGatewayDatastoreAPISpec{
				AIGatewayDatastoreConfig: &aiconfigurationv1alpha1.AIGatewayDatastoreConfig{
					Type: aiconfigurationv1alpha1.AIGatewayDatastoreConfigTypeRedisCE,
					RedisCE: &aiconfigurationv1alpha1.RedisCEDatastore{
						Name: "my-redis",
					},
				},
			},
		},
	}
}

func TestDeleteAIGatewayDatastoreGuarded(t *testing.T) {
	t.Parallel()

	const (
		gatewayID   = "gateway-1"
		datastoreID = "datastore-id"
	)

	// Each subtest uses its own instance: the ops layer mutates the conflict
	// error in place (clearing Instance), and subtests run in parallel.
	newConflictErr := func() *sdkkonnecterrs.ConflictError {
		return &sdkkonnecterrs.ConflictError{
			Status: 409,
			Title:  "Conflict",
			Detail: "server wording is not part of the contract",
		}
	}
	newDatastore := func() *aiconfigurationv1alpha1.AIGatewayDatastore {
		obj := testAIGatewayDatastore()
		obj.SetGatewayID(gatewayID)
		obj.SetKonnectID(datastoreID)
		return obj
	}
	usageResponse := func(entries ...sdkkonnectcomp.AIGatewayDatastoreUsageEntry) *sdkkonnectops.ListAiGatewayDatastoreUsageResponse {
		return &sdkkonnectops.ListAiGatewayDatastoreUsageResponse{
			ListAIGatewayDatastoreUsageResponse: &sdkkonnectcomp.ListAIGatewayDatastoreUsageResponse{Data: entries},
		}
	}

	t.Run("delete succeeds when the datastore is unused", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockAIGatewayDatastoresSDK(t)
		sdk.EXPECT().
			DeleteAiGatewayDatastore(mock.Anything, gatewayID, datastoreID).
			Return(&sdkkonnectops.DeleteAiGatewayDatastoreResponse{}, nil).
			Once()

		require.NoError(t, deleteAIGatewayDatastoreGuarded(t.Context(), sdk, newDatastore()))
	})

	t.Run("a datastore already deleted in Konnect is not an error", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockAIGatewayDatastoresSDK(t)
		sdk.EXPECT().
			DeleteAiGatewayDatastore(mock.Anything, gatewayID, datastoreID).
			Return(nil, &sdkkonnecterrs.NotFoundError{Status: 404, Title: "Not Found"}).
			Once()

		require.NoError(t, deleteAIGatewayDatastoreGuarded(t.Context(), sdk, newDatastore()))
	})

	t.Run("in-use deletion names the entities using it", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockAIGatewayDatastoresSDK(t)
		sdk.EXPECT().
			DeleteAiGatewayDatastore(mock.Anything, gatewayID, datastoreID).
			Return(nil, newConflictErr()).
			Once()
		sdk.EXPECT().
			ListAiGatewayDatastoreUsage(mock.Anything, sdkkonnectops.ListAiGatewayDatastoreUsageRequest{
				GatewayID:         gatewayID,
				DatastoreIDOrName: datastoreID,
			}).
			Return(usageResponse(
				sdkkonnectcomp.AIGatewayDatastoreUsageEntry{Type: sdkkonnectcomp.AIGatewayDatastoreUsageEntryTypePolicy, Name: "p1"},
				sdkkonnectcomp.AIGatewayDatastoreUsageEntry{Type: sdkkonnectcomp.AIGatewayDatastoreUsageEntryTypeModel, Name: "m1"},
			), nil).
			Once()

		err := deleteAIGatewayDatastoreGuarded(t.Context(), sdk, newDatastore())
		require.Error(t, err)
		var blocked DeletionBlockedError
		require.ErrorAs(t, err, &blocked, "expected a DeletionBlockedError, got %T", err)
		require.Contains(t, blocked.DeletionBlockedMessage(), "policy/p1, model/m1")

		var inUse AIGatewayDatastoreInUseError
		require.ErrorAs(t, err, &inUse)
		require.Equal(t, "my-redis", inUse.DatastoreName)
	})

	t.Run("in-use deletion is still reported when the usage lookup fails", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockAIGatewayDatastoresSDK(t)
		sdk.EXPECT().
			DeleteAiGatewayDatastore(mock.Anything, gatewayID, datastoreID).
			Return(nil, newConflictErr()).
			Once()
		sdk.EXPECT().
			ListAiGatewayDatastoreUsage(mock.Anything, mock.Anything).
			Return(nil, errors.New("boom")).
			Once()

		err := deleteAIGatewayDatastoreGuarded(t.Context(), sdk, newDatastore())
		var blocked DeletionBlockedError
		require.ErrorAs(t, err, &blocked, "expected a DeletionBlockedError, got %T", err)
		require.Contains(t, blocked.DeletionBlockedMessage(), "still in use")
	})

	t.Run("a bad request naming users is reported as a blocked deletion", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockAIGatewayDatastoresSDK(t)
		sdk.EXPECT().
			DeleteAiGatewayDatastore(mock.Anything, gatewayID, datastoreID).
			Return(nil, &sdkkonnecterrs.BadRequestError{Status: 400, Title: "Bad Request"}).
			Once()
		sdk.EXPECT().
			ListAiGatewayDatastoreUsage(mock.Anything, mock.Anything).
			Return(usageResponse(
				sdkkonnectcomp.AIGatewayDatastoreUsageEntry{Type: sdkkonnectcomp.AIGatewayDatastoreUsageEntryTypePolicy, Name: "p1"},
			), nil).
			Once()

		err := deleteAIGatewayDatastoreGuarded(t.Context(), sdk, newDatastore())
		var blocked DeletionBlockedError
		require.ErrorAs(t, err, &blocked, "expected a DeletionBlockedError, got %T", err)
		require.Contains(t, blocked.DeletionBlockedMessage(), "policy/p1")
	})

	t.Run("a conflict with no reported users is not a blocked deletion", func(t *testing.T) {
		t.Parallel()

		conflictErr := newConflictErr()
		sdk := sdkmocks.NewMockAIGatewayDatastoresSDK(t)
		sdk.EXPECT().
			DeleteAiGatewayDatastore(mock.Anything, gatewayID, datastoreID).
			Return(nil, conflictErr).
			Once()
		sdk.EXPECT().
			ListAiGatewayDatastoreUsage(mock.Anything, mock.Anything).
			Return(usageResponse(), nil).
			Once()

		err := deleteAIGatewayDatastoreGuarded(t.Context(), sdk, newDatastore())
		require.ErrorIs(t, err, conflictErr)
		var blocked DeletionBlockedError
		require.NotErrorAs(t, err, &blocked, "unexpected DeletionBlockedError: %v", err)
	})

	t.Run("other Konnect errors are not reported as a blocked deletion", func(t *testing.T) {
		t.Parallel()

		sdk := sdkmocks.NewMockAIGatewayDatastoresSDK(t)
		sdk.EXPECT().
			DeleteAiGatewayDatastore(mock.Anything, gatewayID, datastoreID).
			Return(nil, &sdkkonnecterrs.InternalServerError{Status: 500, Title: "Internal Server Error"}).
			Once()

		err := deleteAIGatewayDatastoreGuarded(t.Context(), sdk, newDatastore())
		require.Error(t, err)
		var blocked DeletionBlockedError
		require.NotErrorAs(t, err, &blocked, "unexpected DeletionBlockedError: %v", err)
	})
}
