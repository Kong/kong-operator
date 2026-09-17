package ops

import (
	"errors"
	"fmt"
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
		require.Error(t, errNotEmpty.ListErr)
		assert.Contains(t, errNotEmpty.ListErr.Error(), "list boom")
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
			name: "listing the keys failed",
			err: KonnectConfigStoreNotEmptyError{
				ConfigStoreID: "store-id",
				ListErr:       errors.New("list boom"),
			},
			contains: []string{
				"deletion blocked",
				"still holds secret entries",
				"could not list their keys",
			},
		},
		{
			name: "no entries listed",
			err: KonnectConfigStoreNotEmptyError{
				ConfigStoreID: "store-id",
				Keys:          []string{},
			},
			contains:    []string{"deletion blocked", "no entries were listed", "retried automatically"},
			notContains: []string{"could not list"},
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
		{
			name: "truncated keys report a lower bound",
			err: KonnectConfigStoreNotEmptyError{
				ConfigStoreID: "store-id",
				Keys:          manyKeys,
				KeysTruncated: true,
			},
			contains:    []string{"at least 12 secret entries", "first 10 listed keys", "a", "j"},
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

func TestListConfigStoreSecretKeysPagination(t *testing.T) {
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

	newListRequest := func(pageAfter *string) sdkkonnectops.ListConfigStoreSecretsRequest {
		return sdkkonnectops.ListConfigStoreSecretsRequest{
			ControlPlaneID: parentID,
			ConfigStoreID:  storeID,
			PageSize:       new(int64(configStoreSecretsListPageSize)),
			PageAfter:      pageAfter,
		}
	}

	newListResponse := func(nextPageURI *string, keys ...string) *sdkkonnectops.ListConfigStoreSecretsResponse {
		data := make([]sdkkonnectcomp.ConfigStoreSecret, 0, len(keys))
		for _, k := range keys {
			data = append(data, sdkkonnectcomp.ConfigStoreSecret{Key: new(k)})
		}
		return &sdkkonnectops.ListConfigStoreSecretsResponse{
			ListConfigStoreSecretsResponse: &sdkkonnectcomp.ListConfigStoreSecretsResponse{
				Data: data,
				Meta: sdkkonnectcomp.CursorMeta{
					Page: sdkkonnectcomp.CursorMetaPage{Next: nextPageURI},
				},
			},
		}
	}

	t.Run("follows the page[after] cursor from the next page URI", func(t *testing.T) {
		t.Parallel()

		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		// The SDK models meta.page.next as a full URI; the page[after] item
		// cursor must be extracted from it.
		page1Next := "https://us.api.konghq.com/v2/control-planes/" + parentID +
			"/config-stores/" + storeID + "/secrets?page%5Bafter%5D=cursor-1&page%5Bsize%5D=100"
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, newListRequest(nil)).
			Return(newListResponse(&page1Next, "cert-b"), nil).
			Once()
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, newListRequest(new("cursor-1"))).
			Return(newListResponse(nil, "cert-a"), nil).
			Once()

		keys, truncated, err := listConfigStoreSecretKeys(t.Context(), secretsSDK, newObject())
		require.NoError(t, err)
		assert.Equal(t, []string{"cert-a", "cert-b"}, keys)
		assert.False(t, truncated)
	})

	t.Run("returns a non-nil empty slice when the listing succeeds with no entries", func(t *testing.T) {
		t.Parallel()

		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, newListRequest(nil)).
			Return(newListResponse(nil), nil).
			Once()

		keys, truncated, err := listConfigStoreSecretKeys(t.Context(), secretsSDK, newObject())
		require.NoError(t, err)
		assert.NotNil(t, keys)
		assert.Empty(t, keys)
		assert.False(t, truncated)
	})

	t.Run("stops and deduplicates when the cursor does not advance", func(t *testing.T) {
		t.Parallel()

		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		next := "https://us.api.konghq.com/v2/control-planes/" + parentID +
			"/config-stores/" + storeID + "/secrets?page%5Bafter%5D=cursor-1"
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, newListRequest(nil)).
			Return(newListResponse(&next, "cert-a"), nil).
			Once()
		// The second page returns the same cursor and the same entry: the
		// listing must stop instead of looping to the page cap, and the
		// duplicate key must not be reported twice.
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, newListRequest(new("cursor-1"))).
			Return(newListResponse(&next, "cert-a"), nil).
			Once()

		keys, truncated, err := listConfigStoreSecretKeys(t.Context(), secretsSDK, newObject())
		require.NoError(t, err)
		assert.Equal(t, []string{"cert-a"}, keys)
		assert.True(t, truncated)
	})

	t.Run("returns the keys collected so far when the next page URI is unparseable", func(t *testing.T) {
		t.Parallel()

		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		unparseable := "://not a url"
		secretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, newListRequest(nil)).
			Return(newListResponse(&unparseable, "cert-a"), nil).
			Once()

		keys, truncated, err := listConfigStoreSecretKeys(t.Context(), secretsSDK, newObject())
		require.NoError(t, err)
		assert.Equal(t, []string{"cert-a"}, keys)
		assert.True(t, truncated)
	})

	t.Run("marks the key list truncated when the page limit is reached", func(t *testing.T) {
		t.Parallel()

		secretsSDK := mocks.NewMockConfigStoreSecretsSDK(t)
		var pageAfter *string
		for page := range configStoreSecretsListMaxPages {
			nextCursor := fmt.Sprintf("cursor-%d", page+1)
			next := "https://us.api.konghq.com/v2/control-planes/" + parentID +
				"/config-stores/" + storeID + "/secrets?page%5Bafter%5D=" + nextCursor
			secretsSDK.EXPECT().
				ListConfigStoreSecrets(mock.Anything, newListRequest(pageAfter)).
				Return(newListResponse(&next, fmt.Sprintf("cert-%02d", page)), nil).
				Once()
			pageAfter = new(nextCursor)
		}

		keys, truncated, err := listConfigStoreSecretKeys(t.Context(), secretsSDK, newObject())
		require.NoError(t, err)
		assert.Len(t, keys, configStoreSecretsListMaxPages)
		assert.True(t, truncated)
	})
}

// TestClearInstanceFromErrorPreservesNotEmptyWrapper is a regression test:
// ClearInstanceFromError must keep the KonnectConfigStoreNotEmptyError wrapper
// intact (while still clearing the trace instance in place) so the reconciler
// can extract the blocking entry keys, regardless of which typed SDK error
// shape sits underneath.
func TestClearInstanceFromErrorPreservesNotEmptyWrapper(t *testing.T) {
	t.Parallel()

	t.Run("BadRequestError form", func(t *testing.T) {
		t.Parallel()

		badRequest := &sdkkonnecterrs.BadRequestError{
			Status:   400,
			Detail:   "can not delete config store with secrets",
			Instance: "trace-id",
		}
		err := KonnectConfigStoreNotEmptyError{
			ConfigStoreID: "store-id",
			Keys:          []string{"cert-a"},
			Err:           badRequest,
		}

		cleared := ClearInstanceFromError(err)
		errNotEmpty, ok := errors.AsType[KonnectConfigStoreNotEmptyError](cleared)
		require.True(t, ok, "wrapper must be preserved, got %T (%v)", cleared, cleared)
		assert.Equal(t, []string{"cert-a"}, errNotEmpty.Keys)
		assert.Empty(t, badRequest.Instance, "instance must be cleared in place")
	})

	t.Run("SDKError form", func(t *testing.T) {
		t.Parallel()

		sdkErr := &sdkkonnecterrs.SDKError{
			StatusCode: 400,
			Body:       `{"detail":"can not delete config store with secrets"}`,
		}
		err := KonnectConfigStoreNotEmptyError{
			ConfigStoreID: "store-id",
			Keys:          []string{"cert-a"},
			Err:           sdkErr,
		}

		cleared := ClearInstanceFromError(err)
		errNotEmpty, ok := errors.AsType[KonnectConfigStoreNotEmptyError](cleared)
		require.True(t, ok, "wrapper must be preserved, got %T (%v)", cleared, cleared)
		assert.Equal(t, []string{"cert-a"}, errNotEmpty.Keys)
	})
}
