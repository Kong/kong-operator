package ops

import (
	"sort"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/pkg/metadata"
)

func TestKongKeyToKeyInput(t *testing.T) {
	testCases := []struct {
		name           string
		key            *configurationv1alpha1.KongKey
		expectedOutput sdkkonnectcomp.Key
	}{
		{
			name: "kong key with all fields set without key set",
			key: &configurationv1alpha1.KongKey{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongKey",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "key-1",
					Namespace:  "default",
					Generation: 2,
					UID:        k8stypes.UID("key-uid"),
					Annotations: map[string]string{
						metadata.AnnotationKeyTags: "tag1,tag2,duplicate",
					},
				},
				Spec: configurationv1alpha1.KongKeySpec{
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID:  "kid",
						Name: lo.ToPtr("name"),
						JWK:  lo.ToPtr("jwk"),
						PEM: &configurationv1alpha1.PEMKeyPair{
							PublicKey:  "public",
							PrivateKey: "private",
						},
						Tags: []string{"tag3", "tag4", "duplicate"},
					},
				},
			},
			expectedOutput: sdkkonnectcomp.Key{
				Kid:  "kid",
				Name: lo.ToPtr("name"),
				Jwk:  lo.ToPtr("jwk"),
				Pem: &sdkkonnectcomp.Pem{
					PublicKey:  lo.ToPtr("public"),
					PrivateKey: lo.ToPtr("private"),
				},
				Tags: []string{
					"duplicate",
					"k8s-generation:2",
					"k8s-group:configuration.konghq.com",
					"k8s-kind:KongKey",
					"k8s-name:key-1",
					"k8s-namespace:default",
					"k8s-uid:key-uid",
					"k8s-version:v1alpha1",
					"tag1",
					"tag2",
					"tag3",
					"tag4",
				},
			},
		},
		{
			name: "kong key with all fields set with key set",
			key: &configurationv1alpha1.KongKey{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongKey",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "key-1",
					Namespace:  "default",
					Generation: 2,
					UID:        k8stypes.UID("key-uid"),
					Annotations: map[string]string{
						metadata.AnnotationKeyTags: "tag1,tag2,duplicate",
					},
				},
				Spec: configurationv1alpha1.KongKeySpec{
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID:  "kid",
						Name: lo.ToPtr("name"),
						JWK:  lo.ToPtr("jwk"),
						PEM: &configurationv1alpha1.PEMKeyPair{
							PublicKey:  "public",
							PrivateKey: "private",
						},
						Tags: []string{"tag3", "tag4", "duplicate"},
					},
				},
				Status: configurationv1alpha1.KongKeyStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndKeySetRef{
						KeySetID: "key-set-id",
					},
				},
			},
			expectedOutput: sdkkonnectcomp.Key{
				Kid:  "kid",
				Name: lo.ToPtr("name"),
				Jwk:  lo.ToPtr("jwk"),
				Pem: &sdkkonnectcomp.Pem{
					PublicKey:  lo.ToPtr("public"),
					PrivateKey: lo.ToPtr("private"),
				},
				Set: &sdkkonnectcomp.Set{
					ID: lo.ToPtr("key-set-id"),
				},
				Tags: []string{
					"duplicate",
					"k8s-generation:2",
					"k8s-group:configuration.konghq.com",
					"k8s-kind:KongKey",
					"k8s-name:key-1",
					"k8s-namespace:default",
					"k8s-uid:key-uid",
					"k8s-version:v1alpha1",
					"tag1",
					"tag2",
					"tag3",
					"tag4",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output := kongKeyToKeyInput(tc.key)

			// Tags order is not guaranteed, so we need to sort them before comparing.
			sort.Strings(output.Tags)
			require.Equal(t, tc.expectedOutput, output)
		})
	}
}

func TestAdoptKongKeyOverride(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name                string
		mockPair            func(*testing.T) (*mocks.MockKeysSDK, *configurationv1alpha1.KongKey)
		assertions          func(*testing.T, *configurationv1alpha1.KongKey)
		expectedErrContains string
		expectedErrType     error
	}{
		{
			name: "success",
			mockPair: func(t *testing.T) (*mocks.MockKeysSDK, *configurationv1alpha1.KongKey) {
				sdk := mocks.NewMockKeysSDK(t)
				sdk.EXPECT().GetKey(mock.Anything, "konnect-key-id", "cp-1").Return(
					&sdkkonnectops.GetKeyResponse{
						Key: &sdkkonnectcomp.Key{
							ID:  lo.ToPtr("konnect-key-id"),
							Kid: "kid-1",
							Jwk: lo.ToPtr("jwk-data"),
						},
					},
					nil,
				)
				sdk.EXPECT().UpsertKey(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertKeyRequest) bool {
					return req.ControlPlaneID == "cp-1" && req.KeyID == "konnect-key-id"
				})).Return(&sdkkonnectops.UpsertKeyResponse{}, nil)

				key := &configurationv1alpha1.KongKey{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "key-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "konnect-key-id",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "kid-1",
							JWK: lo.ToPtr("jwk-data"),
						},
					},
					Status: configurationv1alpha1.KongKeyStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndKeySetRef{
							ControlPlaneID: "cp-1",
						},
					},
				}
				return sdk, key
			},
			assertions: func(t *testing.T, key *configurationv1alpha1.KongKey) {
				assert.Equal(t, "konnect-key-id", key.GetKonnectID())
			},
		},
		{
			name: "failed to fetch",
			mockPair: func(t *testing.T) (*mocks.MockKeysSDK, *configurationv1alpha1.KongKey) {
				sdk := mocks.NewMockKeysSDK(t)
				sdk.EXPECT().GetKey(mock.Anything, "konnect-key-id", "cp-1").Return(
					nil,
					&sdkkonnecterrs.NotFoundError{},
				)

				key := &configurationv1alpha1.KongKey{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "key-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "konnect-key-id",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "kid-1",
							JWK: lo.ToPtr("jwk-data"),
						},
					},
					Status: configurationv1alpha1.KongKeyStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndKeySetRef{
							ControlPlaneID: "cp-1",
						},
					},
				}
				return sdk, key
			},
			assertions: func(t *testing.T, key *configurationv1alpha1.KongKey) {
				assert.Empty(t, key.GetKonnectID())
			},
			expectedErrContains: "failed to fetch Konnect entity",
			expectedErrType:     KonnectEntityAdoptionFetchError{},
		},
		{
			name: "uid conflict",
			mockPair: func(t *testing.T) (*mocks.MockKeysSDK, *configurationv1alpha1.KongKey) {
				sdk := mocks.NewMockKeysSDK(t)
				sdk.EXPECT().GetKey(mock.Anything, "konnect-key-id", "cp-1").Return(
					&sdkkonnectops.GetKeyResponse{
						Key: &sdkkonnectcomp.Key{
							Kid:  "kid-1",
							Jwk:  lo.ToPtr("jwk-data"),
							Tags: []string{"k8s-uid:different"},
						},
					},
					nil,
				)

				key := &configurationv1alpha1.KongKey{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "key-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "konnect-key-id",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "kid-1",
							JWK: lo.ToPtr("jwk-data"),
						},
					},
					Status: configurationv1alpha1.KongKeyStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndKeySetRef{
							ControlPlaneID: "cp-1",
						},
					},
				}
				return sdk, key
			},
			assertions: func(t *testing.T, key *configurationv1alpha1.KongKey) {
				assert.Empty(t, key.GetKonnectID())
			},
			expectedErrContains: "Konnect entity (ID: konnect-key-id) is managed by another k8s object",
			expectedErrType:     KonnectEntityAdoptionUIDTagConflictError{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, key := tc.mockPair(t)

			err := adoptKey(ctx, sdk, key)

			if tc.assertions != nil {
				tc.assertions(t, key)
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

func TestAdoptKongKeyMatch(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name                string
		mockPair            func(*testing.T) (*mocks.MockKeysSDK, *configurationv1alpha1.KongKey)
		assertions          func(*testing.T, *configurationv1alpha1.KongKey)
		expectedErrContains string
		expectedErrType     error
	}{
		{
			name: "success",
			mockPair: func(t *testing.T) (*mocks.MockKeysSDK, *configurationv1alpha1.KongKey) {
				sdk := mocks.NewMockKeysSDK(t)
				sdk.EXPECT().GetKey(mock.Anything, "konnect-key-id", "cp-1").Return(
					&sdkkonnectops.GetKeyResponse{
						Key: &sdkkonnectcomp.Key{
							ID:   lo.ToPtr("konnect-key-id"),
							Kid:  "kid-1",
							Name: lo.ToPtr("my-key"),
							Jwk:  lo.ToPtr("jwk-data"),
							Set: &sdkkonnectcomp.Set{
								ID: lo.ToPtr("ks-1"),
							},
						},
					},
					nil,
				)

				key := &configurationv1alpha1.KongKey{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "key-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "konnect-key-id",
							},
						},
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{
								Name: "keyset-1",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID:  "kid-1",
							Name: lo.ToPtr("my-key"),
							JWK:  lo.ToPtr("jwk-data"),
						},
					},
					Status: configurationv1alpha1.KongKeyStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndKeySetRef{
							ControlPlaneID: "cp-1",
							KeySetID:       "ks-1",
						},
					},
				}
				return sdk, key
			},
			assertions: func(t *testing.T, key *configurationv1alpha1.KongKey) {
				assert.Equal(t, "konnect-key-id", key.GetKonnectID())
				assert.Equal(t, "ks-1", key.Status.Konnect.GetKeySetID())
			},
		},
		{
			name: "mismatch",
			mockPair: func(t *testing.T) (*mocks.MockKeysSDK, *configurationv1alpha1.KongKey) {
				sdk := mocks.NewMockKeysSDK(t)
				sdk.EXPECT().GetKey(mock.Anything, "konnect-key-id", "cp-1").Return(
					&sdkkonnectops.GetKeyResponse{
						Key: &sdkkonnectcomp.Key{
							ID:  lo.ToPtr("konnect-key-id"),
							Kid: "kid-1",
							Jwk: lo.ToPtr("different"),
						},
					},
					nil,
				)

				key := &configurationv1alpha1.KongKey{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "key-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "konnect-key-id",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "kid-1",
							JWK: lo.ToPtr("jwk-data"),
						},
					},
					Status: configurationv1alpha1.KongKeyStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndKeySetRef{
							ControlPlaneID: "cp-1",
						},
					},
				}
				return sdk, key
			},
			assertions: func(t *testing.T, key *configurationv1alpha1.KongKey) {
				assert.Empty(t, key.GetKonnectID())
			},
			expectedErrContains: "does not match",
			expectedErrType:     KonnectEntityAdoptionNotMatchError{},
		},
		{
			name: "key set mismatch",
			mockPair: func(t *testing.T) (*mocks.MockKeysSDK, *configurationv1alpha1.KongKey) {
				sdk := mocks.NewMockKeysSDK(t)
				sdk.EXPECT().GetKey(mock.Anything, "konnect-key-id", "cp-1").Return(
					&sdkkonnectops.GetKeyResponse{
						Key: &sdkkonnectcomp.Key{
							ID:  lo.ToPtr("konnect-key-id"),
							Kid: "kid-1",
							Jwk: lo.ToPtr("jwk-data"),
							Set: &sdkkonnectcomp.Set{
								ID: lo.ToPtr("ks-different"),
							},
						},
					},
					nil,
				)

				key := &configurationv1alpha1.KongKey{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "key-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "konnect-key-id",
							},
						},
						KeySetRef: &configurationv1alpha1.KeySetRef{
							Type: configurationv1alpha1.KeySetRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{
								Name: "keyset-1",
							},
						},
						KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
							KID: "kid-1",
							JWK: lo.ToPtr("jwk-data"),
						},
					},
					Status: configurationv1alpha1.KongKeyStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndKeySetRef{
							ControlPlaneID: "cp-1",
							KeySetID:       "ks-1",
						},
					},
				}
				return sdk, key
			},
			assertions: func(t *testing.T, key *configurationv1alpha1.KongKey) {
				assert.Empty(t, key.GetKonnectID())
			},
			expectedErrContains: "does not match",
			expectedErrType:     KonnectEntityAdoptionNotMatchError{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, key := tc.mockPair(t)

			err := adoptKey(ctx, sdk, key)

			if tc.assertions != nil {
				tc.assertions(t, key)
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
