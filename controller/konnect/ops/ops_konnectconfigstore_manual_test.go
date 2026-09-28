package ops

import (
	"errors"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestErrorIsBadRequest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "unrelated error",
			err:      errors.New("boom"),
			expected: false,
		},
		{
			name: "BadRequestError",
			err: &sdkkonnecterrs.BadRequestError{
				Status: 400,
				Title:  "Bad Request",
				Detail: "invalid name",
			},
			expected: true,
		},
		{
			name: "SDKError 400",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       `{"detail":"invalid name"}`,
			},
			expected: true,
		},
		{
			name: "SDKError 500",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 500,
				Body:       `{"detail":"server error"}`,
			},
			expected: false,
		},
		{
			name: "wrapped BadRequestError",
			err: KonnectOperationFailedError{
				Op: DeleteOp,
				Err: &sdkkonnecterrs.BadRequestError{
					Status: 400,
					Detail: "server wording is not part of the contract",
				},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, errorIsBadRequest(tc.err))
		})
	}
}

func TestDeleteKonnectConfigStoreGuarded(t *testing.T) {
	t.Parallel()

	const (
		parentID = "parentID-1"
		storeID  = "konnect_configstore-id"
	)

	// Each subtest must use its own instance: the ops layer mutates the
	// BadRequestError in place (clearing Instance), and subtests run in
	// parallel, so sharing one value is a data race.
	newBadRequestErr := func() *sdkkonnecterrs.BadRequestError {
		return &sdkkonnecterrs.BadRequestError{
			Status: 400,
			Title:  "Bad Request",
			Detail: "server wording is not part of the contract",
		}
	}

	newObject := func() *konnectv1alpha1.KonnectConfigStore {
		obj := testGeneratedKonnectConfigStoreForSDKOps()
		obj.SetControlPlaneID(parentID)
		obj.SetKonnectID(storeID)
		return obj
	}

	t.Run("delete succeeds when the store is empty", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()

		configStoresSDK.EXPECT().
			DeleteConfigStore(
				mock.Anything,
				sdkkonnectops.DeleteConfigStoreRequest{
					ControlPlaneID: parentID,
					ConfigStoreID:  storeID,
				},
			).
			Return(&sdkkonnectops.DeleteConfigStoreResponse{}, nil).
			Once()

		require.NoError(t, deleteKonnectConfigStoreGuarded(ctx, configStoresSDK, secretsSDK, obj))
	})

	t.Run("delete of a missing store is treated as deleted", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()

		configStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.Anything).
			Return(nil, &sdkkonnecterrs.NotFoundError{}).
			Once()

		require.NoError(t, deleteKonnectConfigStoreGuarded(ctx, configStoresSDK, secretsSDK, obj))
	})

	t.Run("bad request with a secret entry returns not-empty error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()
		badRequestErr := newBadRequestErr()

		configStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.Anything).
			Return(nil, badRequestErr).
			Once()
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(
				mock.Anything,
				sdkkonnectops.ListConfigStoreSecretsRequest{
					ControlPlaneID: parentID,
					ConfigStoreID:  storeID,
					PageSize:       new(int64(configStoreSecretsProbePageSize)),
				},
			).
			Return(&sdkkonnectops.ListConfigStoreSecretsResponse{
				ListConfigStoreSecretsResponse: &sdkkonnectcomp.ListConfigStoreSecretsResponse{
					Data: []sdkkonnectcomp.ConfigStoreSecret{
						{Key: new("cert-a")},
					},
				},
			}, nil).
			Once()

		err := deleteKonnectConfigStoreGuarded(ctx, configStoresSDK, secretsSDK, obj)
		require.Error(t, err)

		errNotEmpty, ok := errors.AsType[KonnectConfigStoreNotEmptyError](err)
		require.True(t, ok, "expected KonnectConfigStoreNotEmptyError, got %T (%v)", err, err)
		assert.Equal(t, storeID, errNotEmpty.ConfigStoreID)
		require.ErrorIs(t, err, badRequestErr)
	})

	t.Run("SDKError 400 with a secret entry returns not-empty error", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()
		sdkErr := &sdkkonnecterrs.SDKError{
			StatusCode: 400,
			Body:       `{"detail":"some unrecognized wording"}`,
		}

		configStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.Anything).
			Return(nil, sdkErr).
			Once()
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, mock.Anything).
			Return(&sdkkonnectops.ListConfigStoreSecretsResponse{
				ListConfigStoreSecretsResponse: &sdkkonnectcomp.ListConfigStoreSecretsResponse{
					Data: []sdkkonnectcomp.ConfigStoreSecret{{Key: new("cert-a")}},
				},
			}, nil).
			Once()

		err := deleteKonnectConfigStoreGuarded(ctx, configStoresSDK, secretsSDK, obj)
		require.Error(t, err)

		errNotEmpty, ok := errors.AsType[KonnectConfigStoreNotEmptyError](err)
		require.True(t, ok, "expected KonnectConfigStoreNotEmptyError, got %T (%v)", err, err)
		require.ErrorIs(t, errNotEmpty, sdkErr)
	})

	t.Run("bad request with no secret entries passes through untouched", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()
		badRequestErr := newBadRequestErr()

		configStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.Anything).
			Return(nil, badRequestErr).
			Once()
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, mock.Anything).
			Return(&sdkkonnectops.ListConfigStoreSecretsResponse{
				ListConfigStoreSecretsResponse: &sdkkonnectcomp.ListConfigStoreSecretsResponse{},
			}, nil).
			Once()

		err := deleteKonnectConfigStoreGuarded(ctx, configStoresSDK, secretsSDK, obj)
		require.Error(t, err)
		_, ok := errors.AsType[KonnectConfigStoreNotEmptyError](err)
		assert.False(t, ok, "expected a non-KonnectConfigStoreNotEmptyError error")
		require.ErrorIs(t, err, badRequestErr)
	})

	t.Run("bad request with a failing secret probe passes through untouched", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()
		badRequestErr := newBadRequestErr()

		configStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.Anything).
			Return(nil, badRequestErr).
			Once()
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, mock.Anything).
			Return(nil, errors.New("list boom")).
			Once()

		err := deleteKonnectConfigStoreGuarded(ctx, configStoresSDK, secretsSDK, obj)
		require.Error(t, err)
		_, ok := errors.AsType[KonnectConfigStoreNotEmptyError](err)
		assert.False(t, ok, "expected the original bad request")
		require.ErrorIs(t, err, badRequestErr)
	})

	t.Run("non-bad-request error does not probe secret entries", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()
		serverErr := errors.New("server error")

		configStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.Anything).
			Return(nil, serverErr).
			Once()

		err := deleteKonnectConfigStoreGuarded(ctx, configStoresSDK, secretsSDK, obj)
		require.ErrorIs(t, err, serverErr)
	})
}

func TestKonnectConfigStoreNotEmptyError_DeletionBlockedMessage(t *testing.T) {
	t.Parallel()

	err := KonnectConfigStoreNotEmptyError{ConfigStoreID: "store-id"}
	msg := err.DeletionBlockedMessage()
	assert.Contains(t, msg, "deletion blocked")
	assert.Contains(t, msg, "still holds secret entries")
	assert.Contains(t, msg, "remove the entries from Konnect")
}

func TestConfigStoreHasSecretEntries(t *testing.T) {
	t.Parallel()

	const (
		parentID = "parentID-1"
		storeID  = "konnect_configstore-id"
	)

	newObject := func() *konnectv1alpha1.KonnectConfigStore {
		obj := testGeneratedKonnectConfigStoreForSDKOps()
		obj.SetControlPlaneID(parentID)
		obj.SetKonnectID(storeID)
		return obj
	}

	newListResponse := func(keys ...string) *sdkkonnectops.ListConfigStoreSecretsResponse {
		data := make([]sdkkonnectcomp.ConfigStoreSecret, 0, len(keys))
		for _, key := range keys {
			data = append(data, sdkkonnectcomp.ConfigStoreSecret{Key: new(key)})
		}
		return &sdkkonnectops.ListConfigStoreSecretsResponse{
			ListConfigStoreSecretsResponse: &sdkkonnectcomp.ListConfigStoreSecretsResponse{
				Data: data,
			},
		}
	}

	t.Run("reports true when at least one entry exists", func(t *testing.T) {
		t.Parallel()

		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, sdkkonnectops.ListConfigStoreSecretsRequest{
				ControlPlaneID: parentID,
				ConfigStoreID:  storeID,
				PageSize:       new(int64(configStoreSecretsProbePageSize)),
			}).
			Return(newListResponse("cert-a"), nil).
			Once()

		hasEntries, err := configStoreHasSecretEntries(t.Context(), secretsSDK, newObject())
		require.NoError(t, err)
		assert.True(t, hasEntries)
	})

	t.Run("reports false when no entries exist", func(t *testing.T) {
		t.Parallel()

		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, mock.Anything).
			Return(newListResponse(), nil).
			Once()

		hasEntries, err := configStoreHasSecretEntries(t.Context(), secretsSDK, newObject())
		require.NoError(t, err)
		assert.False(t, hasEntries)
	})

	t.Run("returns list error", func(t *testing.T) {
		t.Parallel()

		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		listErr := errors.New("list boom")
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, mock.Anything).
			Return(nil, listErr).
			Once()

		hasEntries, err := configStoreHasSecretEntries(t.Context(), secretsSDK, newObject())
		assert.False(t, hasEntries)
		require.ErrorIs(t, err, listErr)
	})

	t.Run("returns an error for a nil response", func(t *testing.T) {
		t.Parallel()

		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, mock.Anything).
			Return(nil, nil).
			Once()

		hasEntries, err := configStoreHasSecretEntries(t.Context(), secretsSDK, newObject())
		assert.False(t, hasEntries)
		require.ErrorIs(t, err, ErrNilResponse)
	})
}

// TestClearInstanceFromErrorPreservesNotEmptyWrapper is a regression test:
// ClearInstanceFromError must keep the KonnectConfigStoreNotEmptyError wrapper
// intact (while still clearing the trace instance in place) so the reconciler
// can detect the blocked deletion regardless of which typed SDK error shape
// sits underneath.
func TestClearInstanceFromErrorPreservesNotEmptyWrapper(t *testing.T) {
	t.Parallel()

	t.Run("BadRequestError form", func(t *testing.T) {
		t.Parallel()

		badRequest := &sdkkonnecterrs.BadRequestError{
			Status:   400,
			Detail:   "server wording is not part of the contract",
			Instance: "trace-id",
		}
		err := KonnectConfigStoreNotEmptyError{
			ConfigStoreID: "store-id",
			Err:           badRequest,
		}

		cleared := ClearInstanceFromError(err)
		errNotEmpty, ok := errors.AsType[KonnectConfigStoreNotEmptyError](cleared)
		require.True(t, ok, "wrapper must be preserved, got %T (%v)", cleared, cleared)
		assert.Equal(t, "store-id", errNotEmpty.ConfigStoreID)
		assert.Empty(t, badRequest.Instance, "instance must be cleared in place")
	})

	t.Run("SDKError form", func(t *testing.T) {
		t.Parallel()

		sdkErr := &sdkkonnecterrs.SDKError{
			StatusCode: 400,
			Body:       `{"detail":"server wording is not part of the contract"}`,
		}
		err := KonnectConfigStoreNotEmptyError{
			ConfigStoreID: "store-id",
			Err:           sdkErr,
		}

		cleared := ClearInstanceFromError(err)
		errNotEmpty, ok := errors.AsType[KonnectConfigStoreNotEmptyError](cleared)
		require.True(t, ok, "wrapper must be preserved, got %T (%v)", cleared, cleared)
		assert.Equal(t, "store-id", errNotEmpty.ConfigStoreID)
	})
}
