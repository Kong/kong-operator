package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/pkg/metadata"
)

func TestKongKeySetToKeySetInput(t *testing.T) {
	keySet := &configurationv1alpha1.KongKeySet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongKeySet",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "keySet-1",
			Namespace:  "default",
			Generation: 2,
			UID:        k8stypes.UID(uuid.NewString()),
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2,duplicate",
			},
		},
		Spec: configurationv1alpha1.KongKeySetSpec{
			KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
				Name: "name",
				Tags: []string{"tag3", "tag4", "duplicate"},
			},
		},
	}
	output := kongKeySetToKeySetInput(keySet)
	expectedTags := []string{
		"k8s-generation:2",
		"k8s-kind:KongKeySet",
		"k8s-name:keySet-1",
		"k8s-uid:" + string(keySet.GetUID()),
		"k8s-version:v1alpha1",
		"k8s-group:configuration.konghq.com",
		"k8s-namespace:default",
		"tag1",
		"tag2",
		"tag3",
		"tag4",
		"duplicate",
	}
	require.ElementsMatch(t, expectedTags, output.Tags)
	require.Equal(t, "name", *output.Name)
}

func TestAdoptKongKeySetOverride(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name          string
		mockPair      func(*testing.T) (*mocks.MockKeySetsSDK, *configurationv1alpha1.KongKeySet)
		assertions    func(*testing.T, *configurationv1alpha1.KongKeySet)
		expectedError error
	}{
		{
			name: "success",
			mockPair: func(t *testing.T) (*mocks.MockKeySetsSDK, *configurationv1alpha1.KongKeySet) {
				sdk := mocks.NewMockKeySetsSDK(t)
				sdk.EXPECT().GetKeySet(mock.Anything, "ks-1", "cp-1").Return(
					&sdkkonnectops.GetKeySetResponse{
						KeySet: &sdkkonnectcomp.KeySet{
							ID:   lo.ToPtr("ks-1"),
							Name: lo.ToPtr("name"),
							Tags: []string{"tag1"},
						},
					},
					nil,
				)
				sdk.EXPECT().UpsertKeySet(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertKeySetRequest) bool {
					return req.ControlPlaneID == "cp-1" && req.KeySetID == "ks-1"
				})).Return(&sdkkonnectops.UpsertKeySetResponse{}, nil)

				keySet := &configurationv1alpha1.KongKeySet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "keyset-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySetSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "ks-1",
							},
						},
						KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
							Name: "name",
							Tags: []string{"tag1"},
						},
					},
					Status: configurationv1alpha1.KongKeySetStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "cp-1",
						},
					},
				}
				return sdk, keySet
			},
			assertions: func(t *testing.T, keySet *configurationv1alpha1.KongKeySet) {
				assert.Equal(t, "ks-1", keySet.GetKonnectID())
			},
		},
		{
			name: "fetch failed",
			mockPair: func(t *testing.T) (*mocks.MockKeySetsSDK, *configurationv1alpha1.KongKeySet) {
				sdk := mocks.NewMockKeySetsSDK(t)
				sdk.EXPECT().GetKeySet(mock.Anything, "ks-1", "cp-1").Return(
					nil,
					&sdkkonnecterrs.NotFoundError{},
				)

				keySet := &configurationv1alpha1.KongKeySet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "keyset-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySetSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "ks-1",
							},
						},
						KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
							Name: "name",
						},
					},
					Status: configurationv1alpha1.KongKeySetStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "cp-1",
						},
					},
				}
				return sdk, keySet
			},
			assertions: func(t *testing.T, keySet *configurationv1alpha1.KongKeySet) {
				assert.Empty(t, keySet.GetKonnectID())
			},
			expectedError: KonnectEntityAdoptionFetchError{KonnectID: "ks-1"},
		},
		{
			name: "uid conflict",
			mockPair: func(t *testing.T) (*mocks.MockKeySetsSDK, *configurationv1alpha1.KongKeySet) {
				sdk := mocks.NewMockKeySetsSDK(t)
				sdk.EXPECT().GetKeySet(mock.Anything, "ks-1", "cp-1").Return(
					&sdkkonnectops.GetKeySetResponse{
						KeySet: &sdkkonnectcomp.KeySet{
							ID:   lo.ToPtr("ks-1"),
							Name: lo.ToPtr("name"),
							Tags: []string{"k8s-uid:different"},
						},
					},
					nil,
				)

				keySet := &configurationv1alpha1.KongKeySet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "keyset-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySetSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "ks-1",
							},
						},
						KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
							Name: "name",
						},
					},
					Status: configurationv1alpha1.KongKeySetStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "cp-1",
						},
					},
				}
				return sdk, keySet
			},
			assertions: func(t *testing.T, keySet *configurationv1alpha1.KongKeySet) {
				assert.Empty(t, keySet.GetKonnectID())
			},
			expectedError: KonnectEntityAdoptionUIDTagConflictError{
				KonnectID:    "ks-1",
				ActualUIDTag: "different",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, keySet := tc.mockPair(t)

			err := adoptKeySet(ctx, sdk, keySet)
			require.ErrorIs(t, err, tc.expectedError)

			if tc.assertions != nil {
				tc.assertions(t, keySet)
			}
		})
	}
}

func TestAdoptKongKeySetMatch(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name          string
		mockPair      func(*testing.T) (*mocks.MockKeySetsSDK, *configurationv1alpha1.KongKeySet)
		assertions    func(*testing.T, *configurationv1alpha1.KongKeySet)
		expectedError error
	}{
		{
			name: "success",
			mockPair: func(t *testing.T) (*mocks.MockKeySetsSDK, *configurationv1alpha1.KongKeySet) {
				sdk := mocks.NewMockKeySetsSDK(t)
				sdk.EXPECT().GetKeySet(mock.Anything, "ks-1", "cp-1").Return(
					&sdkkonnectops.GetKeySetResponse{
						KeySet: &sdkkonnectcomp.KeySet{
							ID:   lo.ToPtr("ks-1"),
							Name: lo.ToPtr("name"),
							Tags: []string{"tag1", "tag2"},
						},
					},
					nil,
				)

				keySet := &configurationv1alpha1.KongKeySet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "keyset-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySetSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "ks-1",
							},
						},
						KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
							Name: "name",
							Tags: []string{"tag1"},
						},
					},
					Status: configurationv1alpha1.KongKeySetStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "cp-1",
						},
					},
				}
				return sdk, keySet
			},
			assertions: func(t *testing.T, keySet *configurationv1alpha1.KongKeySet) {
				assert.Equal(t, "ks-1", keySet.GetKonnectID())
			},
		},
		{
			name: "mismatch",
			mockPair: func(t *testing.T) (*mocks.MockKeySetsSDK, *configurationv1alpha1.KongKeySet) {
				sdk := mocks.NewMockKeySetsSDK(t)
				sdk.EXPECT().GetKeySet(mock.Anything, "ks-1", "cp-1").Return(
					&sdkkonnectops.GetKeySetResponse{
						KeySet: &sdkkonnectcomp.KeySet{
							ID:   lo.ToPtr("ks-1"),
							Name: lo.ToPtr("different"),
							Tags: []string{"tag1"},
						},
					},
					nil,
				)

				keySet := &configurationv1alpha1.KongKeySet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "keyset-1",
						Namespace: "default",
						UID:       "abcd-0001",
					},
					Spec: configurationv1alpha1.KongKeySetSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "ks-1",
							},
						},
						KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
							Name: "name",
						},
					},
					Status: configurationv1alpha1.KongKeySetStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "cp-1",
						},
					},
				}
				return sdk, keySet
			},
			assertions: func(t *testing.T, keySet *configurationv1alpha1.KongKeySet) {
				assert.Empty(t, keySet.GetKonnectID())
			},
			expectedError: KonnectEntityAdoptionNotMatchError{KonnectID: "ks-1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, keySet := tc.mockPair(t)

			err := adoptKeySet(ctx, sdk, keySet)
			require.ErrorIs(t, err, tc.expectedError)

			if tc.assertions != nil {
				tc.assertions(t, keySet)
			}
		})
	}
}
