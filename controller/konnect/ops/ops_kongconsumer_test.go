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

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestCreateAndUpdateKongConsumer_KubernetesMetadataConsistency(t *testing.T) {
	var (
		ctx = context.Background()
		cg  = &configurationv1.KongConsumer{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongConsumer",
				APIVersion: "configuration.konghq.com/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cg-1",
				Namespace: "default",
			},
			Status: configurationv1.KongConsumerStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
					ControlPlaneID: uuid.NewString(),
				},
			},
		}
		sdk = &MockConsumersSDK{}
	)

	t.Log("Triggering CreateConsumer and capturing generated tags")
	sdk.EXPECT().
		CreateConsumer(ctx, cg.GetControlPlaneID(), mock.Anything).
		Return(&sdkkonnectops.CreateConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	err := createConsumer(ctx, sdk, cg)
	require.NoError(t, err)
	require.Len(t, sdk.Calls, 1)
	call := sdk.Calls[0]
	require.Equal(t, "CreateConsumer", call.Method)
	require.IsType(t, sdkkonnectcomp.ConsumerInput{}, call.Arguments[2])
	capturedCreateTags := call.Arguments[2].(sdkkonnectcomp.ConsumerInput).Tags

	t.Log("Triggering UpsertConsumer and capturing generated tags")
	sdk.EXPECT().
		UpsertConsumer(ctx, mock.Anything).
		Return(&sdkkonnectops.UpsertConsumerResponse{
			Consumer: &sdkkonnectcomp.Consumer{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	err = updateConsumer(ctx, sdk, cg)
	require.NoError(t, err)
	require.Len(t, sdk.Calls, 2)
	call = sdk.Calls[1]
	require.Equal(t, "UpsertConsumer", call.Method)
	require.IsType(t, sdkkonnectops.UpsertConsumerRequest{}, call.Arguments[1])
	capturedUpsertTags := call.Arguments[1].(sdkkonnectops.UpsertConsumerRequest).Consumer.Tags

	require.NotEmpty(t, capturedCreateTags, "tags should be set on create")
	require.Equal(t, capturedCreateTags, capturedUpsertTags, "tags should be consistent between create and update")
}
