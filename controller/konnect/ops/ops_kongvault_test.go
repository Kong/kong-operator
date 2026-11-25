package ops

import (
	"context"
	"net/http"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func mustConvertKongVaultToVaultInput(t *testing.T, vault *configurationv1alpha1.KongVault) sdkkonnectcomp.Vault {
	t.Helper()
	input, err := kongVaultToVaultInput(t.Context(), fake.NewClientBuilder().Build(), vault)
	require.NoError(t, err)
	return input
}

func TestCreateKongVault(t *testing.T) {
	testCases := []struct {
		name          string
		mockVaultPair func(*testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault)
		expectedErr   bool
		assertions    func(*testing.T, *configurationv1alpha1.KongVault)
	}{
		{
			name: "success",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-1",
					Spec: configurationv1alpha1.KongVaultSpec{
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{}`),
						},
						Backend: "aws",
						Prefix:  "aws-vault1",
					},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().CreateVault(mock.Anything, sdkkonnectops.CreateVaultRequest{ControlPlaneID: "123456789", Vault: mustConvertKongVaultToVaultInput(t, vault)}).
					Return(
						&sdkkonnectops.CreateVaultResponse{
							Vault: &sdkkonnectcomp.Vault{
								ID:     new("12345"),
								Name:   "aws",
								Prefix: "aws-vault1",
							},
						},
						nil,
					)
				return sdk, vault
			},
			assertions: func(t *testing.T, vault *configurationv1alpha1.KongVault) {
				assert.Equal(t, "12345", vault.GetKonnectStatus().GetKonnectID())
			},
		},
		{
			name: "failed - no control plane ID in Konnect status",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-no-cpid",
					Spec: configurationv1alpha1.KongVaultSpec{
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{}`),
						},
						Backend: "aws",
						Prefix:  "aws-vault1",
					},
					Status: configurationv1alpha1.KongVaultStatus{},
				}
				return mocks.NewMockVaultsSDK(t), vault
			},
			expectedErr: true,
			assertions: func(t *testing.T, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectStatus().GetKonnectID())
			},
		},
		{
			name: "fail - upstream returns non-OK response",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-1",
					Spec: configurationv1alpha1.KongVaultSpec{
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{}`),
						},
						Backend: "aws",
						Prefix:  "aws-vault1",
					},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().CreateVault(mock.Anything, sdkkonnectops.CreateVaultRequest{ControlPlaneID: "123456789", Vault: mustConvertKongVaultToVaultInput(t, vault)}).
					Return(
						&sdkkonnectops.CreateVaultResponse{
							Vault:      nil,
							StatusCode: http.StatusBadRequest,
						},
						&sdkkonnecterrs.BadRequestError{
							Title: "bad request",
						},
					)
				return sdk, vault
			},
			expectedErr: true,
			assertions: func(t *testing.T, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectStatus().GetKonnectID())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, vault := tc.mockVaultPair(t)
			err := createVault(t.Context(), fake.NewClientBuilder().Build(), sdk, vault)
			tc.assertions(t, vault)

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestUpdateKongVault(t *testing.T) {
	testCases := []struct {
		name          string
		mockVaultPair func(*testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault)
		expectedErr   bool
		assertions    func(*testing.T, *configurationv1alpha1.KongVault)
	}{
		{
			name: "success",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-1",
					Spec: configurationv1alpha1.KongVaultSpec{
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{}`),
						},
						Backend:     "aws",
						Prefix:      "aws-vault1",
						Description: "test vault",
					},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ID:             "12345",
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().UpsertVault(mock.Anything, sdkkonnectops.UpsertVaultRequest{
					VaultID:        "12345",
					ControlPlaneID: "123456789",
					Vault:          mustConvertKongVaultToVaultInput(t, vault),
				}).
					Return(
						&sdkkonnectops.UpsertVaultResponse{
							Vault: &sdkkonnectcomp.Vault{
								ID:          new("12345"),
								Name:        "aws",
								Prefix:      "aws-vault1",
								Description: new("test vault"),
							},
						},
						nil,
					)
				return sdk, vault
			},
			assertions: func(t *testing.T, vault *configurationv1alpha1.KongVault) {
				assert.Equal(t, "12345", vault.GetKonnectStatus().GetKonnectID())
			},
		},
		{
			name: "fail - upstream returns non-OK response",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-1",
					Spec: configurationv1alpha1.KongVaultSpec{
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{}`),
						},
						Backend:     "aws",
						Prefix:      "aws-vault1",
						Description: "test vault",
					},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ID:             "12345",
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().UpsertVault(mock.Anything, sdkkonnectops.UpsertVaultRequest{
					VaultID:        "12345",
					ControlPlaneID: "123456789",
					Vault:          mustConvertKongVaultToVaultInput(t, vault),
				}).Return(&sdkkonnectops.UpsertVaultResponse{
					StatusCode: http.StatusBadRequest,
				}, &sdkkonnecterrs.BadRequestError{
					Title: "bad request",
				})
				return sdk, vault
			},
			expectedErr: true,
			assertions: func(t *testing.T, vault *configurationv1alpha1.KongVault) {
				assert.Equal(t, "12345", vault.GetKonnectID())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, vault := tc.mockVaultPair(t)
			err := updateVault(t.Context(), fake.NewClientBuilder().Build(), sdk, nil, vault)
			tc.assertions(t, vault)

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestUpdateKongVaultSyncGuard(t *testing.T) {
	const (
		vaultID       = "vault-12345"
		configStoreID = "config-store-1"
	)

	tracker, clock := newTestConfigVersionTracker()
	version := newFakeConfigVersion(t)
	vaultsSDK := mocks.NewMockVaultsSDK(t)

	var (
		stored  sdkkonnectcomp.Vault
		gets    int
		upserts int
	)
	vaultsSDK.EXPECT().UpsertVault(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req sdkkonnectops.UpsertVaultRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpsertVaultResponse, error) {
			upserts++
			stored = req.Vault
			stored.ID = new(req.VaultID)
			stored.UpdatedAt = new(int64(upserts))
			copyStored := stored
			return &sdkkonnectops.UpsertVaultResponse{Vault: &copyStored}, nil
		},
	).Maybe()
	vaultsSDK.EXPECT().GetVault(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, _ sdkkonnectops.GetVaultRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.GetVaultResponse, error) {
			gets++
			copyStored := stored
			return &sdkkonnectops.GetVaultResponse{Vault: &copyStored}, nil
		},
	).Maybe()

	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	sdk.VaultSDK = vaultsSDK
	sdk.DPNodesSDK = version.sdk

	cs := programmedConfigStore("tls-cert-keys", "kong", configStoreID)
	cl := fake.NewClientBuilder().
		WithScheme(configStoreRefScheme(t)).
		WithObjects(cs).
		Build()

	vault := &configurationv1alpha1.KongVault{
		Name:       "vault-sync",
		UID:        k8stypes.UID("vault-uid"),
		Generation: 1,
		Spec: configurationv1alpha1.KongVaultSpec{
			Backend: "konnect",
			Prefix:  "certvault",
			ConfigStoreRef: &configurationv1alpha1.KonnectConfigStoreRef{
				Name:      cs.Name,
				Namespace: cs.Namespace,
			},
		},
		Status: configurationv1alpha1.KongVaultStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ID:             vaultID,
				ControlPlaneID: testCPID,
			},
			Conditions: []metav1.Condition{
				{
					Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status:             metav1.ConditionTrue,
					Reason:             konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed,
					ObservedGeneration: 1,
					LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
				},
			},
		},
	}

	// 1. First periodic update sends the PUT and persists sync state in status.
	_, syncResult, err := Update(t.Context(), sdk, time.Minute, cl, &metricsmocks.MockRecorder{}, vault, tracker)
	require.NoError(t, err)
	require.NotNil(t, syncResult)
	require.NotNil(t, vault.GetSyncStatus())
	assert.Equal(t, SyncHashVersion, vault.GetSyncStatus().HashVersion)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, vault.GetSyncStatus().PayloadHash)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, vault.GetSyncStatus().RepresentationHash)
	assert.Equal(t, 0, gets)
	assert.Equal(t, 1, upserts)
	syncResult.Commit()

	// Backdate Programmed so the next Update call is eligible as a periodic sync.
	vault.Status.Conditions[0].LastTransitionTime = metav1.NewTime(time.Now().Add(-time.Hour))

	// 2. Unchanged vault with unchanged config version skips both GET and PUT.
	clock.Advance(testVersionTTL)
	_, syncResult, err = Update(t.Context(), sdk, time.Minute, cl, &metricsmocks.MockRecorder{}, vault, tracker)
	require.NoError(t, err)
	assert.Nil(t, syncResult)
	assert.Equal(t, 0, gets)
	assert.Equal(t, 1, upserts)

	// 3. External config version bump triggers GET; matching representation skips PUT.
	version.version = "2"
	stored.UpdatedAt = new(int64(999)) // volatile field ignored by representationHash
	clock.Advance(testVersionTTL)
	_, syncResult, err = Update(t.Context(), sdk, time.Minute, cl, &metricsmocks.MockRecorder{}, vault, tracker)
	require.NoError(t, err)
	assert.Nil(t, syncResult)
	assert.Equal(t, 1, gets)
	assert.Equal(t, 1, upserts)

	// 4. Restarted tracker hydrates from persisted SyncStatus and GET-verifies without PUT.
	restarted, _ := newTestConfigVersionTracker()
	_, syncResult, err = Update(t.Context(), sdk, time.Minute, cl, &metricsmocks.MockRecorder{}, vault, restarted)
	require.NoError(t, err)
	assert.Nil(t, syncResult)
	assert.Equal(t, 2, gets)
	assert.Equal(t, 1, upserts)

	// 5. External drift in Konnect triggers GET followed by PUT.
	version.version = "3"
	stored.Description = new("drifted-externally")
	clock.Advance(testVersionTTL)
	_, syncResult, err = Update(t.Context(), sdk, time.Minute, cl, &metricsmocks.MockRecorder{}, vault, tracker)
	require.NoError(t, err)
	require.NotNil(t, syncResult)
	assert.Equal(t, 3, gets)
	assert.Equal(t, 2, upserts)
	syncResult.Commit()
	vault.Status.Conditions[0].LastTransitionTime = metav1.NewTime(time.Now().Add(-time.Hour))

	// 6. Referenced KonnectConfigStore ID change invalidates payloadHash and sends PUT
	// even without a control plane config version change or generation bump.
	cs.Status.ID = "config-store-2"
	require.NoError(t, cl.Update(t.Context(), cs))
	clock.Advance(testVersionTTL)
	_, syncResult, err = Update(t.Context(), sdk, time.Minute, cl, &metricsmocks.MockRecorder{}, vault, tracker)
	require.NoError(t, err)
	require.NotNil(t, syncResult)
	assert.Equal(t, 3, gets)
	assert.Equal(t, 3, upserts)
	assert.Equal(t, "config-store-2", stored.Config[configurationv1alpha1.KongVaultConfigStoreIDKey])
	syncResult.Commit()
}

func TestAdoptKongVault(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name          string
		mockVaultPair func(*testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault)
		assertions    func(*testing.T, *mocks.MockVaultsSDK, *configurationv1alpha1.KongVault)
		expectedError error
	}{
		{
			name: "override success",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-override",
					UID:  k8stypes.UID("uid-override"),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
						Prefix:  "aws-vault1",
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{"region":"us-east-1"}`),
						},
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "vault-123",
							},
						},
					},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().GetVault(mock.Anything, sdkkonnectops.GetVaultRequest{VaultID: "vault-123", ControlPlaneID: "123456789"}).
					Return(&sdkkonnectops.GetVaultResponse{
						Vault: &sdkkonnectcomp.Vault{
							ID:     new("vault-123"),
							Name:   "aws",
							Prefix: "aws-vault1",
							Config: map[string]any{"region": "us-east-1"},
						},
					}, nil)
				sdk.EXPECT().UpsertVault(mock.Anything, sdkkonnectops.UpsertVaultRequest{
					VaultID:        "vault-123",
					ControlPlaneID: "123456789",
					Vault:          mustConvertKongVaultToVaultInput(t, vault),
				}).Return(&sdkkonnectops.UpsertVaultResponse{}, nil)
				return sdk, vault
			},
			assertions: func(t *testing.T, _ *mocks.MockVaultsSDK, vault *configurationv1alpha1.KongVault) {
				assert.Equal(t, "vault-123", vault.GetKonnectID())
			},
		},
		{
			name: "match success",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-match",
					UID:  k8stypes.UID("uid-match"),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
						Prefix:  "aws-vault1",
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{"region":"us-east-1"}`),
						},
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "vault-456",
							},
						},
					},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().GetVault(mock.Anything, sdkkonnectops.GetVaultRequest{VaultID: "vault-456", ControlPlaneID: "123456789"}).
					Return(&sdkkonnectops.GetVaultResponse{
						Vault: &sdkkonnectcomp.Vault{
							ID:     new("vault-456"),
							Name:   "aws",
							Prefix: "aws-vault1",
							Config: map[string]any{"region": "us-east-1"},
							Tags:   []string{"k8s-uid:uid-match"},
						},
					}, nil)
				return sdk, vault
			},
			assertions: func(t *testing.T, sdk *mocks.MockVaultsSDK, vault *configurationv1alpha1.KongVault) {
				assert.Equal(t, "vault-456", vault.GetKonnectID())
				sdk.AssertNotCalled(t, "UpsertVault", mock.Anything, mock.Anything)
			},
		},
		{
			name: "match mismatch",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-mismatch",
					UID:  k8stypes.UID("uid-mismatch"),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
						Prefix:  "aws-vault1",
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{"region":"us-east-1"}`),
						},
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "vault-789",
							},
						},
					},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().GetVault(mock.Anything, sdkkonnectops.GetVaultRequest{VaultID: "vault-789", ControlPlaneID: "123456789"}).
					Return(&sdkkonnectops.GetVaultResponse{
						Vault: &sdkkonnectcomp.Vault{
							ID:     new("vault-789"),
							Name:   "aws",
							Prefix: "aws-vault1",
							Config: map[string]any{"region": "us-west-2"},
						},
					}, nil)
				return sdk, vault
			},
			assertions: func(t *testing.T, _ *mocks.MockVaultsSDK, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectID())
			},
			expectedError: KonnectEntityAdoptionNotMatchError{KonnectID: "vault-789"},
		},
		{
			name: "fetch failure",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-fetch-fail",
					UID:  k8stypes.UID("uid-fetch-fail"),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
						Prefix:  "aws-vault1",
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{"region":"us-east-1"}`),
						},
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "vault-321",
							},
						},
					},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().GetVault(mock.Anything, sdkkonnectops.GetVaultRequest{VaultID: "vault-321", ControlPlaneID: "123456789"}).
					Return(nil, &sdkkonnecterrs.SDKError{
						Message:    "bad request",
						StatusCode: http.StatusBadRequest,
					})
				return sdk, vault
			},
			assertions: func(t *testing.T, _ *mocks.MockVaultsSDK, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectID())
			},
			expectedError: KonnectEntityAdoptionFetchError{
				KonnectID: "vault-321",
			},
		},
		{
			name: "uid conflict",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-uid-conflict",
					UID:  k8stypes.UID("uid-conflict"),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
						Prefix:  "aws-vault1",
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{"region":"us-east-1"}`),
						},
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "vault-654",
							},
						},
					},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().GetVault(mock.Anything, sdkkonnectops.GetVaultRequest{VaultID: "vault-654", ControlPlaneID: "123456789"}).
					Return(&sdkkonnectops.GetVaultResponse{
						Vault: &sdkkonnectcomp.Vault{
							ID:     new("vault-654"),
							Name:   "aws",
							Prefix: "aws-vault1",
							Config: map[string]any{"region": "us-east-1"},
							Tags:   []string{"k8s-uid:other-uid"},
						},
					}, nil)
				return sdk, vault
			},
			assertions: func(t *testing.T, _ *mocks.MockVaultsSDK, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectID())
			},
			expectedError: KonnectEntityAdoptionUIDTagConflictError{
				KonnectID:    "vault-654",
				ActualUIDTag: "other-uid",
			},
		},
		{
			name: "missing control plane id",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-no-cp",
					UID:  k8stypes.UID("uid-no-cp"),
					Spec: configurationv1alpha1.KongVaultSpec{
						Backend: "aws",
						Prefix:  "aws-vault1",
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{"region":"us-east-1"}`),
						},
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "vault-no-cp",
							},
						},
					},
					Status: configurationv1alpha1.KongVaultStatus{},
				}
				return sdk, vault
			},
			assertions: func(t *testing.T, _ *mocks.MockVaultsSDK, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectID())
			},
			expectedError: KonnectEntityAdoptionMissingControlPlaneIDError{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, vault := tc.mockVaultPair(t)
			err := adoptVault(ctx, fake.NewClientBuilder().Build(), sdk, vault)
			require.ErrorIs(t, err, tc.expectedError)

			if tc.assertions != nil {
				tc.assertions(t, sdk, vault)
			}
		})
	}
}

func TestDeleteKongVault(t *testing.T) {
	testCases := []struct {
		name          string
		mockVaultPair func(*testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault)
		expectedErr   bool
	}{
		{
			name: "success",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-1",
					Spec: configurationv1alpha1.KongVaultSpec{},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ID:             "12345",
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().DeleteVault(mock.Anything, sdkkonnectops.DeleteVaultRequest{ControlPlaneID: "123456789", VaultID: "12345"}).Return(
					&sdkkonnectops.DeleteVaultResponse{}, nil,
				)
				return sdk, vault
			},
		},
		{
			name: "fail",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-1",
					Spec: configurationv1alpha1.KongVaultSpec{},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ID:             "12345",
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().DeleteVault(mock.Anything, sdkkonnectops.DeleteVaultRequest{ControlPlaneID: "123456789", VaultID: "12345"}).Return(
					nil, &sdkkonnecterrs.BadRequestError{
						Title: "bad request",
					},
				)
				return sdk, vault
			},
			expectedErr: true,
		},
		{
			name: "not found error treated as successful delete",
			mockVaultPair: func(t *testing.T) (*mocks.MockVaultsSDK, *configurationv1alpha1.KongVault) {
				sdk := mocks.NewMockVaultsSDK(t)
				vault := &configurationv1alpha1.KongVault{
					Name: "vault-1",
					Spec: configurationv1alpha1.KongVaultSpec{},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ID:             "12345",
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().DeleteVault(mock.Anything, sdkkonnectops.DeleteVaultRequest{ControlPlaneID: "123456789", VaultID: "12345"}).Return(
					nil, &sdkkonnecterrs.SDKError{
						Message:    "not found",
						StatusCode: http.StatusNotFound,
					},
				)
				return sdk, vault
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, vault := tc.mockVaultPair(t)
			err := deleteVault(t.Context(), sdk, vault)
			if tc.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
