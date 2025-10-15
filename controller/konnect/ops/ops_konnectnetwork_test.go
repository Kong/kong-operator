package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
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

	_, err := Adopt(ctx, *sdk, 0, cl, metricRecorder, network, *network.Spec.Adopt)
	require.NoError(t, err)
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

	_, err := Adopt(ctx, *sdk, 0, cl, metricRecorder, network, *network.Spec.Adopt)
	require.Error(t, err)
	assert.Empty(t, network.GetKonnectID())
	assertProgrammedCondition(t, network.GetConditions(), metav1.ConditionFalse, string(kcfgkonnect.KonnectEntitiesFailedToAdoptReason))
}
