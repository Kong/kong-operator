package ops

import (
	"context"
	"errors"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/pkg/metadata"
)

func TestKongConsumerGroupToSDKConsumerGroupInput_Tags(t *testing.T) {
	cg := &configurationv1beta1.KongConsumerGroup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongConsumerGroup",
			APIVersion: "configuration.konghq.com/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cg-1",
			Namespace:  "default",
			Generation: 2,
			UID:        k8stypes.UID(uuid.NewString()),
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2",
			},
		},
		Status: configurationv1beta1.KongConsumerGroupStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: uuid.NewString(),
			},
		},
	}
	expectedTags := []string{
		"k8s-kind:KongConsumerGroup",
		"k8s-name:cg-1",
		"k8s-namespace:default",
		"k8s-uid:" + string(cg.GetUID()),
		"k8s-version:v1beta1",
		"k8s-group:configuration.konghq.com",
		"k8s-generation:2",
		"tag1",
		"tag2",
	}
	output := kongConsumerGroupToSDKConsumerGroupInput(cg)
	require.ElementsMatch(t, expectedTags, output.Tags)
}

func TestAdoptKongConsumerGroupOverride(t *testing.T) {
	ctx := context.Background()
	sdk := mocks.NewMockConsumerGroupsSDK(t)

	group := &configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-override",
			Namespace: "default",
			UID:       k8stypes.UID(uuid.NewString()),
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
			Name: "group-override",
		},
		Status: configurationv1beta1.KongConsumerGroupStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}
	adoptOptions := commonv1alpha1.AdoptOptions{
		Mode: commonv1alpha1.AdoptModeOverride,
		Konnect: &commonv1alpha1.AdoptKonnectOptions{
			ID: "group-1",
		},
	}

	sdk.EXPECT().
		GetConsumerGroup(ctx, "group-1", "cp-1").
		Return(&sdkkonnectops.GetConsumerGroupResponse{
			ConsumerGroupInsideWrapper: &sdkkonnectcomp.ConsumerGroupInsideWrapper{
				ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{Name: "group-override"},
			},
		}, nil)
	sdk.EXPECT().
		UpsertConsumerGroup(ctx, mock.MatchedBy(func(req sdkkonnectops.UpsertConsumerGroupRequest) bool {
			return req.ControlPlaneID == "cp-1" && req.ConsumerGroupID == "group-1"
		})).
		Return(&sdkkonnectops.UpsertConsumerGroupResponse{}, nil)

	err := adoptConsumerGroup(ctx, sdk, group, adoptOptions)
	require.NoError(t, err)
	assert.Equal(t, "group-1", group.GetKonnectID())
}

func TestAdoptKongConsumerGroupMatch(t *testing.T) {
	ctx := context.Background()
	sdk := mocks.NewMockConsumerGroupsSDK(t)

	group := &configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-match",
			Namespace: "default",
			UID:       k8stypes.UID(uuid.NewString()),
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
			Name: "group-match",
		},
		Status: configurationv1beta1.KongConsumerGroupStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}
	adoptOptions := commonv1alpha1.AdoptOptions{
		Mode: commonv1alpha1.AdoptModeMatch,
		Konnect: &commonv1alpha1.AdoptKonnectOptions{
			ID: "group-2",
		},
	}

	sdk.EXPECT().
		GetConsumerGroup(ctx, "group-2", "cp-1").
		Return(&sdkkonnectops.GetConsumerGroupResponse{
			ConsumerGroupInsideWrapper: &sdkkonnectcomp.ConsumerGroupInsideWrapper{
				ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{Name: "group-match"},
			},
		}, nil)

	err := adoptConsumerGroup(ctx, sdk, group, adoptOptions)
	require.NoError(t, err)
	assert.Equal(t, "group-2", group.GetKonnectID())
}

func TestAdoptKongConsumerGroupMatchNotMatching(t *testing.T) {
	ctx := context.Background()
	sdk := mocks.NewMockConsumerGroupsSDK(t)

	group := &configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-mismatch",
			Namespace: "default",
			UID:       k8stypes.UID(uuid.NewString()),
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
			Name: "expected-name",
		},
		Status: configurationv1beta1.KongConsumerGroupStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}
	adoptOptions := commonv1alpha1.AdoptOptions{
		Mode: commonv1alpha1.AdoptModeMatch,
		Konnect: &commonv1alpha1.AdoptKonnectOptions{
			ID: "group-3",
		},
	}

	sdk.EXPECT().
		GetConsumerGroup(ctx, "group-3", "cp-1").
		Return(&sdkkonnectops.GetConsumerGroupResponse{
			ConsumerGroupInsideWrapper: &sdkkonnectcomp.ConsumerGroupInsideWrapper{
				ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{Name: "actual-name"},
			},
		}, nil)

	err := adoptConsumerGroup(ctx, sdk, group, adoptOptions)
	require.Error(t, err)
	var notMatch KonnectEntityAdoptionNotMatchError
	assert.True(t, errors.As(err, &notMatch))
}

func TestAdoptKongConsumerGroupUIDConflict(t *testing.T) {
	ctx := context.Background()
	sdk := mocks.NewMockConsumerGroupsSDK(t)

	group := &configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-conflict",
			Namespace: "default",
			UID:       k8stypes.UID("desired-uid"),
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
			Name: "group-conflict",
		},
		Status: configurationv1beta1.KongConsumerGroupStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}
	adoptOptions := commonv1alpha1.AdoptOptions{
		Mode: commonv1alpha1.AdoptModeOverride,
		Konnect: &commonv1alpha1.AdoptKonnectOptions{
			ID: "group-4",
		},
	}

	sdk.EXPECT().
		GetConsumerGroup(ctx, "group-4", "cp-1").
		Return(&sdkkonnectops.GetConsumerGroupResponse{
			ConsumerGroupInsideWrapper: &sdkkonnectcomp.ConsumerGroupInsideWrapper{
				ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
					Name: "group-conflict",
					Tags: []string{"k8s-uid:another-uid"},
				},
			},
		}, nil)

	err := adoptConsumerGroup(ctx, sdk, group, adoptOptions)
	require.Error(t, err)
	var uidConflict KonnectEntityAdoptionUIDTagConflictError
	assert.True(t, errors.As(err, &uidConflict))
}

func TestAdoptKongConsumerGroupFetchError(t *testing.T) {
	ctx := context.Background()
	sdk := mocks.NewMockConsumerGroupsSDK(t)

	group := &configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-fetch",
			Namespace: "default",
			UID:       k8stypes.UID(uuid.NewString()),
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
			Name: "group-fetch",
		},
		Status: configurationv1beta1.KongConsumerGroupStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}
	adoptOptions := commonv1alpha1.AdoptOptions{
		Mode: commonv1alpha1.AdoptModeOverride,
		Konnect: &commonv1alpha1.AdoptKonnectOptions{
			ID: "group-err",
		},
	}

	sdk.EXPECT().
		GetConsumerGroup(ctx, "group-err", "cp-1").
		Return(nil, errors.New("boom"))

	err := adoptConsumerGroup(ctx, sdk, group, adoptOptions)
	require.Error(t, err)
	var fetchErr KonnectEntityAdoptionFetchError
	assert.True(t, errors.As(err, &fetchErr))
}
