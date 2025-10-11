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

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	kcfgkonnect "github.com/kong/kong-operator/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestAdoptMatchNetworkSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	network := &konnectv1alpha1.KonnectCloudGatewayNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-network",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
			Name:                          "test-network",
			CloudGatewayProviderAccountID: "acct-1",
			Region:                        "us-east-1",
			AvailabilityZones:             []string{"us-east-1a", "us-east-1b"},
			CidrBlock:                     "10.0.0.0/16",
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{Name: "default"},
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Mode:    commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "net-1"},
			},
		},
	}

	sdk.CloudGatewaysSDK.
		EXPECT().
		GetNetwork(mock.Anything, "net-1").
		Return(&sdkkonnectops.GetNetworkResponse{
			Network: &sdkkonnectcomp.Network{
				ID:                            "net-1",
				Name:                          "test-network",
				CloudGatewayProviderAccountID: "acct-1",
				Region:                        "us-east-1",
				AvailabilityZones:             []string{"us-east-1b", "us-east-1a"},
				CidrBlock:                     "10.0.0.0/16",
				State:                         sdkkonnectcomp.NetworkStateReady,
			},
		}, nil).
		Once()

	require.NoError(t, AdoptMatch(ctx, *sdk, cl, network))
	assert.Equal(t, "net-1", network.GetKonnectID())
	assert.Equal(t, string(sdkkonnectcomp.NetworkStateReady), network.Status.State)
	assertProgrammedCondition(t, network.GetConditions(), metav1.ConditionTrue, string(konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed))
}

func TestAdoptMatchNetworkMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	network := &konnectv1alpha1.KonnectCloudGatewayNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-network",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
			Name:                          "test-network",
			CloudGatewayProviderAccountID: "acct-1",
			Region:                        "us-east-1",
			AvailabilityZones:             []string{"us-east-1a"},
			CidrBlock:                     "10.0.0.0/16",
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{Name: "default"},
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "net-1"},
			},
		},
	}

	sdk.CloudGatewaysSDK.
		EXPECT().
		GetNetwork(mock.Anything, "net-1").
		Return(&sdkkonnectops.GetNetworkResponse{
			Network: &sdkkonnectcomp.Network{
				ID:                            "net-1",
				Name:                          "test-network",
				CloudGatewayProviderAccountID: "acct-1",
				Region:                        "us-west-2",
				AvailabilityZones:             []string{"us-west-2b"},
				CidrBlock:                     "10.0.0.0/16",
				State:                         sdkkonnectcomp.NetworkStateReady,
			},
		}, nil).
		Once()

	err := AdoptMatch(ctx, *sdk, cl, network)
	require.Error(t, err)
	assert.Empty(t, network.GetKonnectID())
	assertProgrammedCondition(t, network.GetConditions(), metav1.ConditionFalse, string(kcfgkonnect.KonnectEntitiesFailedToAdoptReason))
}

func TestAdoptMatchDataPlaneGroupConfigurationSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	controlPlaneID := "cp-1"
	networkID := "net-1"
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
				Version:        "3.5.0.0",
				APIAccess:      lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
				ControlPlaneID: controlPlaneID,
				DataplaneGroups: []sdkkonnectcomp.ConfigurationDataPlaneGroup{
					{
						ID:                    "dpg-1",
						Provider:              sdkkonnectcomp.ProviderNameAws,
						Region:                "us-east-1",
						CloudGatewayNetworkID: networkID,
						Autoscale: sdkkonnectcomp.CreateConfigurationDataPlaneGroupAutoscaleConfigurationDataPlaneGroupAutoscaleAutopilot(
							sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleAutopilot{
								BaseRps: 100,
								MaxRps:  &maxRps,
							},
						),
						Environment: []sdkkonnectcomp.ConfigurationDataPlaneGroupEnvironmentField{
							{Name: "KONG_CLUSTER_CERT", Value: "cert"},
						},
						State: sdkkonnectcomp.StateReady,
					},
				},
			},
		}, nil).
		Once()

	require.NoError(t, AdoptMatch(ctx, *sdk, cl, cfg))
	assert.Equal(t, "cfg-1", cfg.GetKonnectID())
	assert.Len(t, cfg.Status.DataPlaneGroups, 1)
	assertProgrammedCondition(t, cfg.GetConditions(), metav1.ConditionTrue, string(konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed))
}

func TestAdoptMatchTransitGatewaySuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	networkID := "net-1"

	tg := &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tg",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
			NetworkRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "net"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "tg-1"},
			},
			KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
				Type: konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
				AWSTransitGateway: &konnectv1alpha1.AWSTransitGateway{
					Name:       "test-tg",
					CIDRBlocks: []string{"10.0.0.0/16"},
					DNSConfig: []konnectv1alpha1.TransitGatewayDNSConfig{
						{RemoteDNSServerIPAddresses: []string{"10.1.0.1"}, DomainProxyList: []string{"internal.example.com"}},
					},
					AttachmentConfig: konnectv1alpha1.AwsTransitGatewayAttachmentConfig{
						TransitGatewayID: "tgw-123",
						RAMShareArn:      "arn:aws:ram:us-east-1:123456789012:resource-share/tgw",
					},
				},
			},
		},
	}
	tg.Status.NetworkID = networkID

	awsResp := sdkkonnectcomp.CreateTransitGatewayResponseAwsTransitGatewayResponse(
		sdkkonnectcomp.AwsTransitGatewayResponse{
			ID:         "tg-1",
			Name:       "test-tg",
			CidrBlocks: []string{"10.0.0.0/16"},
			DNSConfig: []sdkkonnectcomp.TransitGatewayDNSConfig{
				{RemoteDNSServerIPAddresses: []string{"10.1.0.1"}, DomainProxyList: []string{"internal.example.com"}},
			},
			TransitGatewayAttachmentConfig: sdkkonnectcomp.AwsTransitGatewayAttachmentConfig{
				TransitGatewayID: "tgw-123",
				RAMShareArn:      "arn:aws:ram:us-east-1:123456789012:resource-share/tgw",
			},
			State: sdkkonnectcomp.TransitGatewayStateReady,
		},
	)

	sdk.CloudGatewaysSDK.
		EXPECT().
		GetTransitGateway(mock.Anything, networkID, "tg-1").
		Return(&sdkkonnectops.GetTransitGatewayResponse{TransitGatewayResponse: &awsResp}, nil).
		Once()

	require.NoError(t, AdoptMatch(ctx, *sdk, cl, tg))
	assert.Equal(t, "tg-1", tg.GetKonnectID())
	assert.Equal(t, sdkkonnectcomp.TransitGatewayStateReady, tg.Status.State)
	assertProgrammedCondition(t, tg.GetConditions(), metav1.ConditionTrue, string(konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed))
}

func assertProgrammedCondition(t *testing.T, conditions []metav1.Condition, expectedStatus metav1.ConditionStatus, expectedReason string) {
	t.Helper()
	cond, found := lo.Find(conditions, func(c metav1.Condition) bool {
		return c.Type == string(konnectv1alpha1.KonnectEntityProgrammedConditionType)
	})
	require.True(t, found, "expected Programmed condition to be set")
	assert.Equal(t, expectedStatus, cond.Status)
	assert.Equal(t, expectedReason, cond.Reason)
}

func TestAdoptMatchDataPlaneGroupConfigurationMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	controlPlaneID := "cp-1"
	networkID := "net-1"
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
				Version:        "4.0.0.0", // mismatch on purpose
				APIAccess:      lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
				ControlPlaneID: controlPlaneID,
				DataplaneGroups: []sdkkonnectcomp.ConfigurationDataPlaneGroup{
					{
						ID:                    "dpg-1",
						Provider:              sdkkonnectcomp.ProviderNameAws,
						Region:                "us-east-1",
						CloudGatewayNetworkID: networkID,
						Autoscale: sdkkonnectcomp.CreateConfigurationDataPlaneGroupAutoscaleConfigurationDataPlaneGroupAutoscaleAutopilot(
							sdkkonnectcomp.ConfigurationDataPlaneGroupAutoscaleAutopilot{
								BaseRps: 100,
								MaxRps:  &maxRps,
							},
						),
						Environment: []sdkkonnectcomp.ConfigurationDataPlaneGroupEnvironmentField{
							{Name: "KONG_CLUSTER_CERT", Value: "cert"},
						},
						State: sdkkonnectcomp.StateReady,
					},
				},
			},
		}, nil).
		Once()

	err := AdoptMatch(ctx, *sdk, cl, cfg)
	require.Error(t, err)
	assert.Empty(t, cfg.GetKonnectID())
	assertProgrammedCondition(t, cfg.GetConditions(), metav1.ConditionFalse, string(kcfgkonnect.KonnectEntitiesFailedToAdoptReason))
}

func TestAdoptMatchTransitGatewayMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	networkID := "net-1"

	tg := &konnectv1alpha1.KonnectCloudGatewayTransitGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tg-mismatch",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayTransitGatewaySpec{
			NetworkRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "net"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "tg-2"},
			},
			KonnectTransitGatewayAPISpec: konnectv1alpha1.KonnectTransitGatewayAPISpec{
				Type: konnectv1alpha1.TransitGatewayTypeAWSTransitGateway,
				AWSTransitGateway: &konnectv1alpha1.AWSTransitGateway{
					Name:       "test-tg",
					CIDRBlocks: []string{"10.0.0.0/16"},
					DNSConfig: []konnectv1alpha1.TransitGatewayDNSConfig{
						{RemoteDNSServerIPAddresses: []string{"10.1.0.1"}, DomainProxyList: []string{"internal.example.com"}},
					},
					AttachmentConfig: konnectv1alpha1.AwsTransitGatewayAttachmentConfig{
						TransitGatewayID: "tgw-123",
						RAMShareArn:      "arn:aws:ram:us-east-1:123456789012:resource-share/tgw",
					},
				},
			},
		},
	}
	tg.Status.NetworkID = networkID

	awsResp := sdkkonnectcomp.CreateTransitGatewayResponseAwsTransitGatewayResponse(
		sdkkonnectcomp.AwsTransitGatewayResponse{
			ID:         "tg-2",
			Name:       "test-tg",
			CidrBlocks: []string{"10.1.0.0/16"}, // mismatch on purpose
			DNSConfig: []sdkkonnectcomp.TransitGatewayDNSConfig{
				{RemoteDNSServerIPAddresses: []string{"10.1.0.1"}, DomainProxyList: []string{"internal.example.com"}},
			},
			TransitGatewayAttachmentConfig: sdkkonnectcomp.AwsTransitGatewayAttachmentConfig{
				TransitGatewayID: "tgw-123",
				RAMShareArn:      "arn:aws:ram:us-east-1:123456789012:resource-share/tgw",
			},
			State: sdkkonnectcomp.TransitGatewayStateReady,
		},
	)

	sdk.CloudGatewaysSDK.
		EXPECT().
		GetTransitGateway(mock.Anything, networkID, "tg-2").
		Return(&sdkkonnectops.GetTransitGatewayResponse{TransitGatewayResponse: &awsResp}, nil).
		Once()

	err := AdoptMatch(ctx, *sdk, cl, tg)
	require.Error(t, err)
	assert.Empty(t, tg.GetKonnectID())
	assertProgrammedCondition(t, tg.GetConditions(), metav1.ConditionFalse, string(kcfgkonnect.KonnectEntitiesFailedToAdoptReason))
}

func TestAdoptMatchUnsupportedMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	network := &konnectv1alpha1.KonnectCloudGatewayNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "net-mode", Namespace: "default"},
		Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
			Name:                          "net-mode",
			CloudGatewayProviderAccountID: "acct-1",
			Region:                        "us-east-1",
			AvailabilityZones:             []string{"us-east-1a"},
			CidrBlock:                     "10.0.0.0/16",
			KonnectConfiguration:          konnectv1alpha2.KonnectConfiguration{APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{Name: "default"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Mode:    "invalid-mode",
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "net-2"},
			},
		},
	}

	err := AdoptMatch(ctx, *sdk, cl, network)
	require.Error(t, err)
	assert.Empty(t, network.GetKonnectID())
	assertProgrammedCondition(t, network.GetConditions(), metav1.ConditionFalse, string(kcfgkonnect.KonnectEntitiesFailedToAdoptReason))
}

func TestAdoptMatchMissingKonnectID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	network := &konnectv1alpha1.KonnectCloudGatewayNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "net-missing-id", Namespace: "default"},
		Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
			Name:                          "net-missing-id",
			CloudGatewayProviderAccountID: "acct-1",
			Region:                        "us-east-1",
			AvailabilityZones:             []string{"us-east-1a"},
			CidrBlock:                     "10.0.0.0/16",
			KonnectConfiguration:          konnectv1alpha2.KonnectConfiguration{APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{Name: "default"}},
			Adopt: &commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Konnect: nil, // missing ID
			},
		},
	}

	err := AdoptMatch(ctx, *sdk, cl, network)
	require.Error(t, err)
	assert.Empty(t, network.GetKonnectID())
	assertProgrammedCondition(t, network.GetConditions(), metav1.ConditionFalse, string(kcfgkonnect.KonnectEntitiesFailedToAdoptReason))
}
