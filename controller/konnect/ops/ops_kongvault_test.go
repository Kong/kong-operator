package ops

import (
	"net/http"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func mustConvertKongVaultToVaultInput(t *testing.T, vault *configurationv1alpha1.KongVault) sdkkonnectcomp.Vault {
	t.Helper()
	input, err := kongVaultToVaultInput(vault)
	require.NoError(t, err)
	return input
}

func TestCreateKongVault(t *testing.T) {
	testCases := []struct {
		name          string
		mockVaultPair func(*testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault)
		expectedErr   bool
		assertions    func(*testing.T, *configurationv1alpha1.KongVault)
	}{
		{
			name: "success",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-1",
					},
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
				sdk.EXPECT().CreateVault(mock.Anything, "123456789", mustConvertKongVaultToVaultInput(t, vault)).
					Return(
						&sdkkonnectops.CreateVaultResponse{
							Vault: &sdkkonnectcomp.Vault{
								ID:     lo.ToPtr("12345"),
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
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-no-cpid",
					},
					Spec: configurationv1alpha1.KongVaultSpec{
						Config: apiextensionsv1.JSON{
							Raw: []byte(`{}`),
						},
						Backend: "aws",
						Prefix:  "aws-vault1",
					},
					Status: configurationv1alpha1.KongVaultStatus{},
				}
				return sdkmocks.NewMockVaultSDK(t), vault
			},
			expectedErr: true,
			assertions: func(t *testing.T, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectStatus().GetKonnectID())
			},
		},
		{
			name: "fail - upstream returns non-OK response",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-1",
					},
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
				sdk.EXPECT().CreateVault(mock.Anything, "123456789", mustConvertKongVaultToVaultInput(t, vault)).
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
			err := createVault(t.Context(), sdk, vault)
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
		mockVaultPair func(*testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault)
		expectedErr   bool
		assertions    func(*testing.T, *configurationv1alpha1.KongVault)
	}{
		{
			name: "success",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-1",
					},
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
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "12345",
							},
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
								ID:          lo.ToPtr("12345"),
								Name:        "aws",
								Prefix:      "aws-vault1",
								Description: lo.ToPtr("test vault"),
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
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-1",
					},
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
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "12345",
							},
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
			err := updateVault(t.Context(), sdk, vault)
			tc.assertions(t, vault)

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestAdoptKongVault(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name                string
		mockVaultPair       func(*testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault)
		assertions          func(*testing.T, *sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault)
		expectedErrContains string
		expectedErrType     error
	}{
		{
			name: "override success",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-override",
						UID:  k8stypes.UID("uid-override"),
					},
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
				sdk.EXPECT().GetVault(mock.Anything, "vault-123", "123456789").
					Return(&sdkkonnectops.GetVaultResponse{
						Vault: &sdkkonnectcomp.Vault{
							ID:     lo.ToPtr("vault-123"),
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
			assertions: func(t *testing.T, _ *sdkmocks.MockVaultSDK, vault *configurationv1alpha1.KongVault) {
				assert.Equal(t, "vault-123", vault.GetKonnectID())
			},
		},
		{
			name: "match success",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-match",
						UID:  k8stypes.UID("uid-match"),
					},
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
				sdk.EXPECT().GetVault(mock.Anything, "vault-456", "123456789").
					Return(&sdkkonnectops.GetVaultResponse{
						Vault: &sdkkonnectcomp.Vault{
							ID:     lo.ToPtr("vault-456"),
							Name:   "aws",
							Prefix: "aws-vault1",
							Config: map[string]any{"region": "us-east-1"},
							Tags:   []string{"k8s-uid:uid-match"},
						},
					}, nil)
				return sdk, vault
			},
			assertions: func(t *testing.T, sdk *sdkmocks.MockVaultSDK, vault *configurationv1alpha1.KongVault) {
				assert.Equal(t, "vault-456", vault.GetKonnectID())
				sdk.AssertNotCalled(t, "UpsertVault", mock.Anything, mock.Anything)
			},
		},
		{
			name: "match mismatch",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-mismatch",
						UID:  k8stypes.UID("uid-mismatch"),
					},
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
				sdk.EXPECT().GetVault(mock.Anything, "vault-789", "123456789").
					Return(&sdkkonnectops.GetVaultResponse{
						Vault: &sdkkonnectcomp.Vault{
							ID:     lo.ToPtr("vault-789"),
							Name:   "aws",
							Prefix: "aws-vault1",
							Config: map[string]any{"region": "us-west-2"},
						},
					}, nil)
				return sdk, vault
			},
			assertions: func(t *testing.T, _ *sdkmocks.MockVaultSDK, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectID())
			},
			expectedErrContains: "Konnect entity (ID: vault-789) does not match the spec of the object when adopting in match mode",
			expectedErrType:     &KonnectEntityAdoptionNotMatchError{},
		},
		{
			name: "fetch failure",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-fetch-fail",
						UID:  k8stypes.UID("uid-fetch-fail"),
					},
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
				sdk.EXPECT().GetVault(mock.Anything, "vault-321", "123456789").
					Return(nil, &sdkkonnecterrs.SDKError{
						Message:    "bad request",
						StatusCode: http.StatusBadRequest,
					})
				return sdk, vault
			},
			assertions: func(t *testing.T, _ *sdkmocks.MockVaultSDK, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectID())
			},
			expectedErrContains: "failed to fetch Konnect entity (ID: vault-321) for adoption",
			expectedErrType:     KonnectEntityAdoptionFetchError{},
		},
		{
			name: "uid conflict",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-uid-conflict",
						UID:  k8stypes.UID("uid-conflict"),
					},
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
				sdk.EXPECT().GetVault(mock.Anything, "vault-654", "123456789").
					Return(&sdkkonnectops.GetVaultResponse{
						Vault: &sdkkonnectcomp.Vault{
							ID:     lo.ToPtr("vault-654"),
							Name:   "aws",
							Prefix: "aws-vault1",
							Config: map[string]any{"region": "us-east-1"},
							Tags:   []string{"k8s-uid:other-uid"},
						},
					}, nil)
				return sdk, vault
			},
			assertions: func(t *testing.T, _ *sdkmocks.MockVaultSDK, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectID())
			},
			expectedErrContains: "Konnect entity (ID: vault-654) is managed by another k8s object with UID other-uid",
			expectedErrType:     KonnectEntityAdoptionUIDTagConflictError{},
		},
		{
			name: "missing control plane id",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-no-cp",
						UID:  k8stypes.UID("uid-no-cp"),
					},
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
			assertions: func(t *testing.T, _ *sdkmocks.MockVaultSDK, vault *configurationv1alpha1.KongVault) {
				assert.Empty(t, vault.GetKonnectID())
			},
			expectedErrContains: "No Control Plane ID",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, vault := tc.mockVaultPair(t)
			err := adoptVault(ctx, sdk, vault)

			if tc.assertions != nil {
				tc.assertions(t, sdk, vault)
			}

			if tc.expectedErrContains != "" {
				assert.ErrorContains(t, err, tc.expectedErrContains)
				if tc.expectedErrType != nil {
					require.ErrorAs(t, err, &tc.expectedErrType)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestDeleteKongVault(t *testing.T) {
	testCases := []struct {
		name          string
		mockVaultPair func(*testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault)
		expectedErr   bool
	}{
		{
			name: "success",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-1",
					},
					Spec: configurationv1alpha1.KongVaultSpec{},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "12345",
							},
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().DeleteVault(mock.Anything, "123456789", "12345").Return(
					&sdkkonnectops.DeleteVaultResponse{}, nil,
				)
				return sdk, vault
			},
		},
		{
			name: "fail",
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-1",
					},
					Spec: configurationv1alpha1.KongVaultSpec{},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "12345",
							},
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().DeleteVault(mock.Anything, "123456789", "12345").Return(
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
			mockVaultPair: func(t *testing.T) (*sdkmocks.MockVaultSDK, *configurationv1alpha1.KongVault) {
				sdk := sdkmocks.NewMockVaultSDK(t)
				vault := &configurationv1alpha1.KongVault{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vault-1",
					},
					Spec: configurationv1alpha1.KongVaultSpec{},
					Status: configurationv1alpha1.KongVaultStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "12345",
							},
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().DeleteVault(mock.Anything, "123456789", "12345").Return(
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
