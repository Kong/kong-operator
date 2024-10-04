package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/modules/manager"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongConsumerCredential_HMAC(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, context.Background())
	defer cancel()

	// Setup up the envtest environment.
	cfg, ns := Setup(t, ctx, scheme.Get())

	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clientWithWatch, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	consumerID := uuid.NewString()
	consumer := deploy.KongConsumerWithProgrammed(t, ctx, clientNamespaced, &configurationv1.KongConsumer{
		Username: "username1",
		Spec: configurationv1.KongConsumerSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
		},
	})
	consumer.Status.Konnect = &v1alpha1.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
		KonnectEntityStatus: v1alpha1.KonnectEntityStatus{
			ID:        consumerID,
			ServerURL: cp.GetKonnectStatus().GetServerURL(),
			OrgID:     cp.GetKonnectStatus().GetOrgID(),
		},
	}
	require.NoError(t, clientNamespaced.Status().Update(ctx, consumer))

	kongCredentialHMAC := deploy.KongCredentialHMAC(t, ctx, clientNamespaced, consumer.Name)
	hmacID := uuid.NewString()
	tags := []string{
		"k8s-generation:1",
		"k8s-group:configuration.konghq.com",
		"k8s-kind:KongCredentialHMAC",
		"k8s-name:" + kongCredentialHMAC.Name,
		"k8s-namespace:" + ns.Name,
		"k8s-uid:" + string(kongCredentialHMAC.GetUID()),
		"k8s-version:v1alpha1",
	}

	factory := ops.NewMockSDKFactory(t)
	factory.SDK.KongCredentialsHMACSDK.EXPECT().
		CreateHmacAuthWithConsumer(
			mock.Anything,
			sdkkonnectops.CreateHmacAuthWithConsumerRequest{
				ControlPlaneID:              cp.GetKonnectStatus().GetKonnectID(),
				ConsumerIDForNestedEntities: consumerID,
				HMACAuthWithoutParents: sdkkonnectcomp.HMACAuthWithoutParents{
					Username: lo.ToPtr("username"),
					Tags:     tags,
				},
			},
		).
		Return(
			&sdkkonnectops.CreateHmacAuthWithConsumerResponse{
				HMACAuth: &sdkkonnectcomp.HMACAuth{
					ID: lo.ToPtr(hmacID),
				},
			},
			nil,
		)
	factory.SDK.KongCredentialsHMACSDK.EXPECT().
		UpsertHmacAuthWithConsumer(mock.Anything, mock.Anything, mock.Anything).Maybe().
		Return(
			&sdkkonnectops.UpsertHmacAuthWithConsumerResponse{
				HMACAuth: &sdkkonnectcomp.HMACAuth{
					ID: lo.ToPtr(hmacID),
				},
			},
			nil,
		)

	require.NoError(t, manager.SetupCacheIndicesForKonnectTypes(ctx, mgr, false))
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialHMAC](konnectSyncTime),
		),
	}

	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KongCredentialsHMACSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	factory.SDK.KongCredentialsHMACSDK.EXPECT().
		DeleteHmacAuthWithConsumer(
			mock.Anything,
			sdkkonnectops.DeleteHmacAuthWithConsumerRequest{
				ControlPlaneID:              cp.GetKonnectStatus().GetKonnectID(),
				ConsumerIDForNestedEntities: consumerID,
				HMACAuthID:                  hmacID,
			},
		).
		Return(
			&sdkkonnectops.DeleteHmacAuthWithConsumerResponse{
				StatusCode: 200,
			},
			nil,
		)
	require.NoError(t, clientNamespaced.Delete(ctx, kongCredentialHMAC))

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KongCredentialsHMACSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	w := setupWatch[configurationv1alpha1.KongCredentialHMACList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))

	kongCredentialHMAC = deploy.KongCredentialHMAC(t, ctx, clientNamespaced, consumer.Name)
	t.Logf("redeployed %s KongCredentialHMAC resource", client.ObjectKeyFromObject(kongCredentialHMAC))
	t.Logf("checking if KongConsumer %s removal will delete the associated credentials %s",
		client.ObjectKeyFromObject(consumer),
		client.ObjectKeyFromObject(kongCredentialHMAC),
	)

	require.NoError(t, clientNamespaced.Delete(ctx, consumer))
	_ = watchFor(t, ctx, w, watch.Modified,
		func(c *configurationv1alpha1.KongCredentialHMAC) bool {
			return c.Name == kongCredentialHMAC.Name
		},
		"KongCredentialHMAC wasn't deleted but it should have been",
	)
}
