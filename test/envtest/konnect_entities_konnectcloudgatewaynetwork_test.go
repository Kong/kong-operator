package envtest

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestKonnectCloudGatewayNetwork(t *testing.T) {
	t.Parallel()

	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.KonnectCloudGatewayNetwork](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.KonnectCloudGatewayNetwork](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Run("Creating, updating and deleting Konnect cloud gateway network", func(t *testing.T) {
		t.Log("Setting up a watch for KonnectCloudGatewayNetwork events")
		w := setupWatch[konnectv1alpha1.KonnectCloudGatewayNetworkList](t, ctx, cl, client.InNamespace(ns.Name))
		t.Log("Setting up SDK expectations on creation")

		var (
			networkID   = "kcgn-" + uuid.NewString()
			networkName = "cloud-gateway-network-test-" + uuid.NewString()[:8]
		)

		t.Log("Setting up SDK expectations on creation")
		sdk.CloudGatewaysSDK.EXPECT().CreateNetwork(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectcomp.CreateNetworkRequest) bool {
				return req.Name == networkName
			}),
		).Return(&sdkkonnectops.CreateNetworkResponse{
			Network: &sdkkonnectcomp.Network{
				ID:    networkID,
				Name:  networkName,
				State: sdkkonnectcomp.NetworkStateInitializing,
			},
		}, nil)

		t.Log("Creating KonnectAPIAuthConfiguration")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)

		t.Log("Creating a KonnectCloudGatewayNetwork")
		network := deploy.KonnectCloudGatewayNetwork(t, ctx, clientNamespaced, apiAuth, func(o client.Object) {
			n, ok := o.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
			if !ok {
				return
			}
			n.Spec.Name = networkName
		})

		t.Log("Waiting for KonnectCloudGatewayNetwork to be Programmed and get a Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(n *konnectv1alpha1.KonnectCloudGatewayNetwork) bool {
			return n.GetKonnectID() == networkID && conditionsContainProgrammed(n.GetConditions(), metav1.ConditionTrue)
		}, "Did not see KonnectCloudGatewayNetwork get Programmed and Konnect ID set.")

		t.Log("Setting up SDK expectations on deletion")
		sdk.CloudGatewaysSDK.EXPECT().DeleteNetwork(mock.Anything, networkID, mock.Anything).Return(&sdkkonnectops.DeleteNetworkResponse{}, nil)

		t.Log("Deleting the network")
		require.NoError(t, clientNamespaced.Delete(ctx, network))
		eventually.WaitForObjectToNotExist(t, ctx, cl, network, waitTime, tickTime)

		t.Log("Waiting for object to be deleted in the SDK")
		eventuallyAssertSDKExpectations(t, factory.SDK.CloudGatewaysSDK, waitTime, tickTime)
	})

	t.Run("Creating network when SDK returns ForbiddenError", func(t *testing.T) {
		t.Log("Setting up a watch for KonnectCloudGatewayNetwork events")
		w := setupWatch[konnectv1alpha1.KonnectCloudGatewayNetworkList](t, ctx, cl, client.InNamespace(ns.Name))
		t.Log("Setting up SDK expectations on creation")

		networkName := "cloud-gateway-network-test-" + uuid.NewString()[:8]

		t.Log("Setting up SDK expectations on creation")
		sdk.CloudGatewaysSDK.EXPECT().CreateNetwork(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectcomp.CreateNetworkRequest) bool {
				return req.Name == networkName
			}),
		).Return(nil, &sdkkonnecterrs.ForbiddenError{
			Status:   403,
			Title:    "Quota Exceeded",
			Instance: "kong:trace:0000000000000000000",
			Detail:   "Maximum number of Active Networks exceeded. Max allowed: 0",
		})

		t.Log("Creating KonnectAPIAuthConfiguration")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)

		t.Log("Creating a KonnectCloudGatewayNetwork")
		deploy.KonnectCloudGatewayNetwork(t, ctx, clientNamespaced, apiAuth, func(o client.Object) {
			n, ok := o.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
			if !ok {
				return
			}
			n.Spec.Name = networkName
		})

		t.Log("Waiting for KonnectCloudGatewayNetwork to be Programmed and get a Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(n *konnectv1alpha1.KonnectCloudGatewayNetwork) bool {
			return conditionsContainProgrammedFalse(n.GetConditions())
		}, "Did not see KonnectCloudGatewayNetwork get Programmed condition set to false.")

		t.Log("Waiting for the expected calls called in SDK")
		eventuallyAssertSDKExpectations(t, factory.SDK.CloudGatewaysSDK, waitTime, tickTime)
	})

	t.Run("Adopting a network with match mode", func(t *testing.T) {
		t.Log("Setting up a watch for KonnectCloudGatewayNetwork events")
		w := setupWatch[konnectv1alpha1.KonnectCloudGatewayNetworkList](t, ctx, cl, client.InNamespace(ns.Name))

		networkName := "cloud-gateway-network-test-adopt-" + uuid.NewString()[:8]
		networkID := uuid.NewString()

		t.Log("Setting up SDK expectation on getting")
		sdk.CloudGatewaysSDK.EXPECT().GetNetwork(mock.Anything, networkID).Return(
			&sdkkonnectops.GetNetworkResponse{
				Network: &sdkkonnectcomp.Network{
					ID:                            networkID,
					Name:                          networkName,
					CloudGatewayProviderAccountID: "aws:1234",
					Region:                        "us-east-1",
					AvailabilityZones: []string{
						"us-east-1",
					},
					CidrBlock: "10.0.0.0/24",
					State:     sdkkonnectcomp.NetworkStateInitializing,
					ProviderMetadata: sdkkonnectcomp.NetworkProviderMetadata{
						VpcID: lo.ToPtr("vpc-1234"),
					},
				},
			}, nil,
		)

		t.Log("Creating KonnectAPIAuthConfiguration")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)

		t.Log("Creating a KonnectCloudGatewayNetwork to adopt the existing network")
		createdNetwork := deploy.KonnectCloudGatewayNetwork(t, ctx, clientNamespaced, apiAuth, func(o client.Object) {
			n, ok := o.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
			if !ok {
				return
			}

			n.Spec.Adopt = &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: networkID,
				},
			}
			n.Spec.Name = networkName
			n.Spec.CloudGatewayProviderAccountID = "aws:1234"
			n.Spec.Region = "us-east-1"
			n.Spec.AvailabilityZones = []string{"us-east-1"}
			n.Spec.CidrBlock = "10.0.0.0/24"
		})

		t.Log("Waiting for KonnectCloudGatewayNetwork to be marked as programmed and get Konnect ID")
		watchFor(t, t.Context(), w, apiwatch.Modified, func(n *konnectv1alpha1.KonnectCloudGatewayNetwork) bool {
			return n.Name == createdNetwork.Name &&
				conditionsContainProgrammedTrue(n.GetConditions()) &&
				n.GetKonnectID() == networkID
		},
			"Did not see KonnectCloudGatewayNetwork turn Programmed and set Konnect ID",
		)
	})

	t.Run("Adopting a network with match mode but not matching the network in Konnect", func(t *testing.T) {
		t.Log("Setting up a watch for KonnectCloudGatewayNetwork events")
		w := setupWatch[konnectv1alpha1.KonnectCloudGatewayNetworkList](t, ctx, cl, client.InNamespace(ns.Name))

		networkName := "cloud-gateway-network-test-adopt-" + uuid.NewString()[:8]
		networkID := uuid.NewString()

		t.Log("Setting up SDK expectation on getting")
		sdk.CloudGatewaysSDK.EXPECT().GetNetwork(mock.Anything, networkID).Return(
			&sdkkonnectops.GetNetworkResponse{
				Network: &sdkkonnectcomp.Network{
					ID:                            networkID,
					Name:                          networkName,
					CloudGatewayProviderAccountID: "aws:1234",
					Region:                        "us-east-1",
					AvailabilityZones: []string{
						"us-east-1",
					},
					CidrBlock: "10.0.0.0/24",
					State:     sdkkonnectcomp.NetworkStateInitializing,
					ProviderMetadata: sdkkonnectcomp.NetworkProviderMetadata{
						VpcID: lo.ToPtr("vpc-1234"),
					},
				},
			}, nil,
		)

		t.Log("Creating KonnectAPIAuthConfiguration")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)

		t.Log("Creating a KonnectCloudGatewayNetwork to adopt the existing network but not matching the configuration")
		createdNetwork := deploy.KonnectCloudGatewayNetwork(t, ctx, clientNamespaced, apiAuth, func(o client.Object) {
			n, ok := o.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
			if !ok {
				return
			}

			n.Spec.Adopt = &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: networkID,
				},
			}
			n.Spec.Name = networkName
			n.Spec.CloudGatewayProviderAccountID = "aws:1234"
			n.Spec.Region = "us-east-1"
			n.Spec.AvailabilityZones = []string{"us-east-1"}
			n.Spec.CidrBlock = "10.1.0.0/24" // different CIDRs with the existing network
		})

		t.Log("Waiting for KonnectCloudGatewayNetwork to be marked as not programmed")
		watchFor(t, t.Context(), w, apiwatch.Modified, func(n *konnectv1alpha1.KonnectCloudGatewayNetwork) bool {
			return n.Name == createdNetwork.Name &&
				conditionsContainProgrammedFalse(n.GetConditions()) && lo.ContainsBy(
				n.GetConditions(), func(c metav1.Condition) bool {
					return c.Type == konnectv1alpha1.KonnectEntityAdoptedConditionType &&
						c.Status == metav1.ConditionFalse &&
						c.Reason == konnectv1alpha1.KonnectEntityAdoptedReasonNotMatch
				},
			)
		},
			"Did not see KonnectCloudGatewayNetwork marked as not Programmed and not Adopted",
		)
	})
}
