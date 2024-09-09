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
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestCreateAndUpdateKongPlugin_KubernetesMetadataConsistency(t *testing.T) {
	var (
		ctx           = context.Background()
		pluginBinding = &configurationv1alpha1.KongPluginBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongPluginBinding",
				APIVersion: "configuration.konghq.com/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "plugin-binding-1",
				Namespace: "default",
				UID:       k8stypes.UID(uuid.NewString()),
			},
			Spec: configurationv1alpha1.KongPluginBindingSpec{
				PluginReference: configurationv1alpha1.PluginRef{
					Name: "plugin-1",
					Kind: lo.ToPtr("KongPlugin"),
				},
				Targets: configurationv1alpha1.KongPluginBindingTargets{
					ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
						Name: "service-1",
						Kind: "KongService",
					},
				},
			},
			Status: configurationv1alpha1.KongPluginBindingStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
					ControlPlaneID: uuid.NewString(),
				},
			},
		}
		sdk = &MockPluginSDK{}
		cl  = fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(
			&configurationv1.KongPlugin{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongPlugin",
					APIVersion: "configuration.konghq.com/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "plugin-1",
					Namespace: "default",
				},
				PluginName: "basic-auth",
			},
			&configurationv1alpha1.KongService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongService",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-1",
					Namespace: "default",
				},
				Status: configurationv1alpha1.KongServiceStatus{
					Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				},
			},
		).Build()
	)

	t.Log("Triggering CreatePlugin and capturing generated tags")
	sdk.EXPECT().
		CreatePlugin(ctx, pluginBinding.GetControlPlaneID(), mock.Anything).
		Return(&sdkkonnectops.CreatePluginResponse{
			Plugin: &sdkkonnectcomp.Plugin{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	err := createPlugin(ctx, cl, sdk, pluginBinding)
	require.NoError(t, err)
	require.Len(t, sdk.Calls, 1)
	call := sdk.Calls[0]
	require.Equal(t, "CreatePlugin", call.Method)
	require.IsType(t, sdkkonnectcomp.PluginInput{}, call.Arguments[2])
	capturedCreateTags := call.Arguments[2].(sdkkonnectcomp.PluginInput).Tags

	t.Log("Triggering UpsertPlugin and capturing generated tags")
	sdk.EXPECT().
		UpsertPlugin(ctx, mock.Anything).
		Return(&sdkkonnectops.UpsertPluginResponse{
			Plugin: &sdkkonnectcomp.Plugin{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	err = updatePlugin(ctx, sdk, cl, pluginBinding)
	require.NoError(t, err)
	require.Len(t, sdk.Calls, 2)
	call = sdk.Calls[1]
	require.Equal(t, "UpsertPlugin", call.Method)
	require.IsType(t, sdkkonnectops.UpsertPluginRequest{}, call.Arguments[1])
	capturedUpsertTags := call.Arguments[1].(sdkkonnectops.UpsertPluginRequest).Plugin.Tags

	require.NotEmpty(t, capturedCreateTags, "tags should be set on create")
	require.Equal(t, capturedCreateTags, capturedUpsertTags, "tags should be consistent between create and update")
}
