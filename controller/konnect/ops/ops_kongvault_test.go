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

	sdkmocks "github.com/kong/kong-operator/controller/konnect/ops/sdk/mocks"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
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
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
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
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
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
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
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
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
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
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
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
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
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
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
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
