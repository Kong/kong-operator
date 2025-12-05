package ops

import (
	"context"
	"errors"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/metadata"
)

func TestKongConsumerToSDKConsumerInput_Tags(t *testing.T) {
	cg := &configurationv1.KongConsumer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongConsumer",
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
	}
	output := kongConsumerToSDKConsumerInput(cg)
	expectedTags := []string{
		"k8s-generation:2",
		"k8s-kind:KongConsumer",
		"k8s-name:cg-1",
		"k8s-uid:" + string(cg.GetUID()),
		"k8s-version:v1beta1",
		"k8s-group:configuration.konghq.com",
		"k8s-namespace:default",
		"tag1",
		"tag2",
	}
	require.ElementsMatch(t, expectedTags, output.Tags)
}

func TestAdoptKongConsumerOverride(t *testing.T) {
	ctx := context.Background()
	sdk := mocks.NewMockConsumersSDK(t)
	cgSDK := mocks.NewMockConsumerGroupsSDK(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	consumer := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer-override",
			Namespace: "default",
			UID:       k8stypes.UID(uuid.NewString()),
		},
		Status: configurationv1.KongConsumerStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}
	adoptOptions := commonv1alpha1.AdoptOptions{
		Mode: commonv1alpha1.AdoptModeOverride,
		Konnect: &commonv1alpha1.AdoptKonnectOptions{
			ID: "cons-1",
		},
	}

	sdk.EXPECT().
		GetConsumer(ctx, "cons-1", "cp-1").
		Return(&sdkkonnectops.GetConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{},
		}, nil)
	sdk.EXPECT().
		UpsertConsumer(ctx, mock.MatchedBy(func(req sdkkonnectops.UpsertConsumerRequest) bool {
			return req.ControlPlaneID == "cp-1" && req.ConsumerID == "cons-1"
		})).
		Return(&sdkkonnectops.UpsertConsumerResponse{}, nil)
	sdk.EXPECT().
		ListConsumerGroupsForConsumer(ctx, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
			ControlPlaneID: "cp-1",
			ConsumerID:     "cons-1",
		}).
		Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{
			Object: &sdkkonnectops.ListConsumerGroupsForConsumerResponseBody{},
		}, nil)

	err := adoptConsumer(ctx, sdk, cgSDK, cl, consumer, adoptOptions)
	require.NoError(t, err)
	assert.Equal(t, "cons-1", consumer.GetKonnectID())
}

func TestAdoptKongConsumerMatch(t *testing.T) {
	ctx := context.Background()
	sdk := mocks.NewMockConsumersSDK(t)
	cgSDK := mocks.NewMockConsumerGroupsSDK(t)

	cg1 := &configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group-a",
			Namespace: "default",
		},
		Status: configurationv1beta1.KongConsumerGroupStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
					ID: "cg-id-a",
				},
			},
		},
	}
	cg2 := cg1.DeepCopy()
	cg2.Name = "group-b"
	cg2.Status.Konnect.ID = "cg-id-b"

	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(cg1, cg2).
		Build()

	consumer := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer-match",
			Namespace: "default",
			UID:       k8stypes.UID(uuid.NewString()),
		},
		Username:       "user-1",
		CustomID:       "custom-1",
		ConsumerGroups: []string{"group-a", "group-b"},
		Status: configurationv1.KongConsumerStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}
	adoptOptions := commonv1alpha1.AdoptOptions{
		Mode: commonv1alpha1.AdoptModeMatch,
		Konnect: &commonv1alpha1.AdoptKonnectOptions{
			ID: "cons-2",
		},
	}

	sdk.EXPECT().
		GetConsumer(ctx, "cons-2", "cp-1").
		Return(&sdkkonnectops.GetConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				Username: lo.ToPtr("user-1"),
				CustomID: lo.ToPtr("custom-1"),
			},
		}, nil)
	sdk.EXPECT().
		ListConsumerGroupsForConsumer(ctx, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
			ControlPlaneID: "cp-1",
			ConsumerID:     "cons-2",
		}).
		Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{
			Object: &sdkkonnectops.ListConsumerGroupsForConsumerResponseBody{
				Data: []sdkkonnectcomp.ConsumerGroup{
					{ID: lo.ToPtr("cg-id-a")},
					{ID: lo.ToPtr("cg-id-b")},
				},
			},
		}, nil)

	err := adoptConsumer(ctx, sdk, cgSDK, cl, consumer, adoptOptions)
	require.NoError(t, err)
	assert.Equal(t, "cons-2", consumer.GetKonnectID())

	cond, found := lo.Find(consumer.Status.Conditions, func(c metav1.Condition) bool {
		return c.Type == string(konnectv1alpha1.KongConsumerGroupRefsValidConditionType)
	})
	require.True(t, found, "expected KongConsumerGroupRefsValid condition to be set")
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, konnectv1alpha1.KongConsumerGroupRefsReasonValid, cond.Reason)
}

func TestAdoptKongConsumerMatchNotMatching(t *testing.T) {
	ctx := context.Background()
	sdk := mocks.NewMockConsumersSDK(t)
	cgSDK := mocks.NewMockConsumerGroupsSDK(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	consumer := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer-mismatch",
			Namespace: "default",
			UID:       k8stypes.UID(uuid.NewString()),
		},
		Username: "desired",
		Status: configurationv1.KongConsumerStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}
	adoptOptions := commonv1alpha1.AdoptOptions{
		Mode: commonv1alpha1.AdoptModeMatch,
		Konnect: &commonv1alpha1.AdoptKonnectOptions{
			ID: "cons-3",
		},
	}

	sdk.EXPECT().
		GetConsumer(ctx, "cons-3", "cp-1").
		Return(&sdkkonnectops.GetConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				Username: lo.ToPtr("actual"),
			},
		}, nil)

	err := adoptConsumer(ctx, sdk, cgSDK, cl, consumer, adoptOptions)
	require.Error(t, err)
	var notMatch KonnectEntityAdoptionNotMatchError
	assert.True(t, errors.As(err, &notMatch))
}

func TestAdoptKongConsumerFetchError(t *testing.T) {
	ctx := context.Background()
	sdk := mocks.NewMockConsumersSDK(t)
	cgSDK := mocks.NewMockConsumerGroupsSDK(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	consumer := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer-fetch",
			Namespace: "default",
			UID:       k8stypes.UID(uuid.NewString()),
		},
		Status: configurationv1.KongConsumerStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}
	adoptOptions := commonv1alpha1.AdoptOptions{
		Mode: commonv1alpha1.AdoptModeOverride,
		Konnect: &commonv1alpha1.AdoptKonnectOptions{
			ID: "cons-err",
		},
	}

	sdk.EXPECT().
		GetConsumer(ctx, "cons-err", "cp-1").
		Return(nil, errors.New("boom"))

	err := adoptConsumer(ctx, sdk, cgSDK, cl, consumer, adoptOptions)
	require.Error(t, err)
	var fetchErr KonnectEntityAdoptionFetchError
	assert.True(t, errors.As(err, &fetchErr))
}
