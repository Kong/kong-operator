package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	kcfgkonnect "github.com/kong/kong-operator/v2/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/generate"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestAdoptMatchDataPlaneGroupConfigurationSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	controlPlaneID := generate.KonnectID(t)
	networkID := generate.KonnectID(t)
	maxRps := int64(200)

	cfg := &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cfg",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
			Version: "3.5.0.0",
			DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
				{
					Provider:   sdkkonnectcomp.ProviderNameAws,
					Region:     "us-east-1",
					NetworkRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeKonnectID, KonnectID: lo.ToPtr(networkID)},
					Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
						Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot,
						Autopilot: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleAutopilot{
							BaseRps: 100,
							MaxRps:  &maxRps,
						},
					},
					Environment: []konnectv1alpha1.ConfigurationDataPlaneGroupEnvironmentField{
						{Name: "KONG_CLUSTER_CERT", Value: "cert"},
					},
				},
			},
			ControlPlaneRef: commonv1alpha1.ControlPlaneRef{Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef, KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "cp"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "cfg-1"},
			},
		},
	}
	cfg.Status.ControlPlaneID = controlPlaneID

	sdk.CloudGatewaysSDK.
		EXPECT().
		GetConfiguration(mock.Anything, "cfg-1").
		Return(&sdkkonnectops.GetConfigurationResponse{
			ConfigurationManifest: &sdkkonnectcomp.ConfigurationManifest{
				ID:             "cfg-1",
				Version:        lo.ToPtr("3.5.0.0"),
				APIAccess:      lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
				ControlPlaneID: controlPlaneID,
				DataplaneGroups: []sdkkonnectcomp.ConfigurationDataPlaneGroup{
					{
						ID:                    "dpg-1",
						Provider:              sdkkonnectcomp.ProviderNameAws,
						Region:                "us-east-1",
						CloudGatewayNetworkID: lo.ToPtr(networkID),
						Autoscale: lo.ToPtr(sdkkonnectcomp.CreateConfigurationDataPlaneGroupAutoscaleConfigurationDataPlaneGroupAutoscaleAutopilot(
							sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleAutopilot{
								BaseRps: 100,
								MaxRps:  &maxRps,
							},
						)),
						Environment: []sdkkonnectcomp.ConfigurationDataPlaneGroupEnvironmentField{
							{Name: "KONG_CLUSTER_CERT", Value: "cert"},
						},
						State: sdkkonnectcomp.StateReady,
					},
				},
			},
		}, nil).
		Once()

	_, err := Adopt(ctx, *sdk, 0, cl, metricRecorder, cfg, *cfg.Spec.Adopt)
	require.NoError(t, err)
	assert.Equal(t, "cfg-1", cfg.GetKonnectID())
	assert.Len(t, cfg.Status.DataPlaneGroups, 1)
	assertProgrammedCondition(t, cfg.GetConditions(), metav1.ConditionTrue, string(konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed))
}

func TestAdoptMatchDataPlaneGroupConfigurationMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	controlPlaneID := generate.KonnectID(t)
	networkID := generate.KonnectID(t)
	maxRps := int64(200)

	cfg := &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cfg-mismatch",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
			Version: "3.5.0.0",
			DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
				{
					Provider:   sdkkonnectcomp.ProviderNameAws,
					Region:     "us-east-1",
					NetworkRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeKonnectID, KonnectID: lo.ToPtr(networkID)},
					Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
						Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot,
						Autopilot: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleAutopilot{
							BaseRps: 100,
							MaxRps:  &maxRps,
						},
					},
					Environment: []konnectv1alpha1.ConfigurationDataPlaneGroupEnvironmentField{
						{Name: "KONG_CLUSTER_CERT", Value: "cert"},
					},
				},
			},
			ControlPlaneRef: commonv1alpha1.ControlPlaneRef{Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef, KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "cp"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "cfg-2"},
			},
		},
	}
	cfg.Status.ControlPlaneID = controlPlaneID

	sdk.CloudGatewaysSDK.
		EXPECT().
		GetConfiguration(mock.Anything, "cfg-2").
		Return(&sdkkonnectops.GetConfigurationResponse{
			ConfigurationManifest: &sdkkonnectcomp.ConfigurationManifest{
				ID:             "cfg-2",
				Version:        lo.ToPtr("4.0.0.0"), // mismatch on purpose
				APIAccess:      lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
				ControlPlaneID: controlPlaneID,
				DataplaneGroups: []sdkkonnectcomp.ConfigurationDataPlaneGroup{
					{
						ID:                    "dpg-1",
						Provider:              sdkkonnectcomp.ProviderNameAws,
						Region:                "us-east-1",
						CloudGatewayNetworkID: lo.ToPtr(networkID),
						Autoscale: lo.ToPtr(sdkkonnectcomp.CreateConfigurationDataPlaneGroupAutoscaleConfigurationDataPlaneGroupAutoscaleAutopilot(
							sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleAutopilot{
								BaseRps: 100,
								MaxRps:  &maxRps,
							},
						)),
						Environment: []sdkkonnectcomp.ConfigurationDataPlaneGroupEnvironmentField{
							{Name: "KONG_CLUSTER_CERT", Value: "cert"},
						},
						State: sdkkonnectcomp.StateReady,
					},
				},
			},
		}, nil).
		Once()

	_, err := Adopt(ctx, *sdk, 0, cl, metricRecorder, cfg, *cfg.Spec.Adopt)
	require.Error(t, err)
	assert.Empty(t, cfg.GetKonnectID())
	assertProgrammedCondition(t, cfg.GetConditions(), metav1.ConditionFalse, string(kcfgkonnect.KonnectEntitiesFailedToAdoptReason))
}
