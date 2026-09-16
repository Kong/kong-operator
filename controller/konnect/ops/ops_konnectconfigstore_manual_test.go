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

func TestErrorIsConfigStoreNotEmpty(t *testing.T) {
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
			name: "BadRequestError with not-empty detail",
			err: &sdkkonnecterrs.BadRequestError{
				Status: 400,
				Title:  "Bad Request",
				Detail: "can not delete config store with secrets",
			},
			expected: true,
		},
		{
			name: "BadRequestError with unrelated detail",
			err: &sdkkonnecterrs.BadRequestError{
				Status: 400,
				Title:  "Bad Request",
				Detail: "invalid name",
			},
			expected: false,
		},
		{
			name: "SDKError 400 with not-empty body",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       `{"detail":"can not delete config store with secrets"}`,
			},
			expected: true,
		},
		{
			name: "SDKError 400 with unrelated body",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       `{"detail":"invalid name"}`,
			},
			expected: false,
		},
		{
			name: "SDKError 500 with matching body",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 500,
				Body:       `{"detail":"can not delete config store with secrets"}`,
			},
			expected: false,
		},
		{
			name: "wrapped BadRequestError with not-empty detail",
			err: KonnectOperationFailedError{
				Op: DeleteOp,
				Err: &sdkkonnecterrs.BadRequestError{
					Status: 400,
					Detail: "can not delete config store with secrets",
				},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, ErrorIsConfigStoreNotEmpty(tc.err))
		})
	}
}

func TestDeleteKonnectConfigStoreGuarded(t *testing.T) {
	t.Parallel()

	const (
		parentID = "parentID-1"
		storeID  = "konnect_configstore-id"
	)

	notEmptyErr := &sdkkonnecterrs.BadRequestError{
		Status: 400,
		Title:  "Bad Request",
		Detail: "can not delete config store with secrets",
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

	t.Run("blocked delete returns not-empty error with sorted entry keys", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()

		configStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.Anything).
			Return(nil, notEmptyErr).
			Once()
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(
				mock.Anything,
				sdkkonnectops.ListConfigStoreSecretsRequest{
					ControlPlaneID: parentID,
					ConfigStoreID:  storeID,
					PageSize:       new(int64(configStoreSecretsListPageSize)),
				},
			).
			Return(&sdkkonnectops.ListConfigStoreSecretsResponse{
				ListConfigStoreSecretsResponse: &sdkkonnectcomp.ListConfigStoreSecretsResponse{
					Data: []sdkkonnectcomp.ConfigStoreSecret{
						{Key: new("cert-b")},
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
		assert.Equal(t, []string{"cert-a", "cert-b"}, errNotEmpty.Keys)
		require.ErrorIs(t, err, notEmptyErr)
	})

	t.Run("blocked delete with failing keys listing still reports blocked", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()

		configStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.Anything).
			Return(nil, notEmptyErr).
			Once()
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, mock.Anything).
			Return(nil, errors.New("list boom")).
			Once()

		err := deleteKonnectConfigStoreGuarded(ctx, configStoresSDK, secretsSDK, obj)
		require.Error(t, err)

		errNotEmpty, ok := errors.AsType[KonnectConfigStoreNotEmptyError](err)
		require.True(t, ok, "expected KonnectConfigStoreNotEmptyError, got %T (%v)", err, err)
		assert.Nil(t, errNotEmpty.Keys)
		assert.Contains(t, err.Error(), "listing them failed")
	})

	t.Run("unrelated 400 error passes through untouched", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		configStoresSDK := mocks.NewMockConfigStoresSDK(t)
		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		obj := newObject()

		unrelatedErr := &sdkkonnecterrs.BadRequestError{
			Status: 400,
			Title:  "Bad Request",
			Detail: "some other validation failure",
		}
		configStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.Anything).
			Return(nil, unrelatedErr).
			Once()

		err := deleteKonnectConfigStoreGuarded(ctx, configStoresSDK, secretsSDK, obj)
		require.Error(t, err)
		_, ok := errors.AsType[KonnectConfigStoreNotEmptyError](err)
		assert.False(t, ok, "expected a non-KonnectConfigStoreNotEmptyError error")
		require.ErrorIs(t, err, unrelatedErr)
	})
}

func TestKonnectConfigStoreNotEmptyError_DeletionBlockedMessage(t *testing.T) {
	t.Parallel()

	manyKeys := make([]string, 12)
	for i := range manyKeys {
		manyKeys[i] = string(rune('a' + i))
	}

	testCases := []struct {
		name        string
		err         KonnectConfigStoreNotEmptyError
		contains    []string
		notContains []string
	}{
		{
			name: "keys unknown",
			err:  KonnectConfigStoreNotEmptyError{ConfigStoreID: "store-id"},
			contains: []string{
				"deletion blocked",
				"still holds secret entries",
			},
			notContains: []string{"keys"},
		},
		{
			name: "no keys listed",
			err: KonnectConfigStoreNotEmptyError{
				ConfigStoreID: "store-id",
				Keys:          []string{},
			},
			contains: []string{"deletion blocked", "could not list"},
		},
		{
			name: "few keys",
			err: KonnectConfigStoreNotEmptyError{
				ConfigStoreID: "store-id",
				Keys:          []string{"cert-a", "cert-b"},
			},
			contains: []string{"2 secret entries", "cert-a", "cert-b"},
		},
		{
			name: "many keys are capped",
			err: KonnectConfigStoreNotEmptyError{
				ConfigStoreID: "store-id",
				Keys:          manyKeys,
			},
			contains:    []string{"12 secret entries", "first 10 keys", "a", "j"},
			notContains: []string{"[k", "l]"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := tc.err.DeletionBlockedMessage()
			for _, s := range tc.contains {
				assert.Contains(t, msg, s)
			}
			for _, s := range tc.notContains {
				assert.NotContains(t, msg, s)
			}
		})
	}
}
