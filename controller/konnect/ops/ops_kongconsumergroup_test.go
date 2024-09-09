package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestCreateAndUpdateKongConsumerGroup_KubernetesMetadataConsistency(t *testing.T) {
	var (
		ctx = context.Background()
		cg  = &configurationv1beta1.KongConsumerGroup{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongConsumerGroup",
				APIVersion: "configuration.konghq.com/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cg-1",
				Namespace: "default",
			},
			Status: configurationv1beta1.KongConsumerGroupStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
					ControlPlaneID: uuid.NewString(),
				},
			},
		}
		sdk = &MockConsumerGroupSDK{}
	)

	t.Log("Triggering CreateConsumerGroup and capturing generated tags")
	sdk.EXPECT().
		CreateConsumerGroup(ctx, cg.GetControlPlaneID(), mock.Anything).
		Return(&sdkkonnectops.CreateConsumerGroupResponse{
			ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	err := createConsumerGroup(ctx, sdk, cg)
	require.NoError(t, err)
	require.Len(t, sdk.Calls, 1)
	call := sdk.Calls[0]
	require.Equal(t, "CreateConsumerGroup", call.Method)
	require.IsType(t, sdkkonnectcomp.ConsumerGroupInput{}, call.Arguments[2])
	capturedCreateTags := call.Arguments[2].(sdkkonnectcomp.ConsumerGroupInput).Tags

	t.Log("Triggering UpsertConsumerGroup and capturing generated tags")
	sdk.EXPECT().
		UpsertConsumerGroup(ctx, mock.Anything).
		Return(&sdkkonnectops.UpsertConsumerGroupResponse{
			ConsumerGroup: &sdkkonnectcomp.ConsumerGroup{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	err = updateConsumerGroup(ctx, sdk, cg)
	require.NoError(t, err)
	require.Len(t, sdk.Calls, 2)
	call = sdk.Calls[1]
	require.Equal(t, "UpsertConsumerGroup", call.Method)
	require.IsType(t, sdkkonnectops.UpsertConsumerGroupRequest{}, call.Arguments[1])
	capturedUpsertTags := call.Arguments[1].(sdkkonnectops.UpsertConsumerGroupRequest).ConsumerGroup.Tags

	require.NotEmpty(t, capturedCreateTags, "tags should be set on create")
	require.Equal(t, capturedCreateTags, capturedUpsertTags, "tags should be consistent between create and update")
}
