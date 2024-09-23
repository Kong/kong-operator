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

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongConsumerCredential_BasicAuth(t *testing.T) {
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

	apiAuth := deployKonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deployKonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	consumerID := uuid.NewString()
	consumer := deployKongConsumerWithProgrammed(t, ctx, clientNamespaced, &configurationv1.KongConsumer{
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

	password := "password"
	username := "username"
	KongCredentialBasicAuth := deployKongCredentialBasicAuth(t, ctx, clientNamespaced, consumer.Name, username, password)
	basicAuthID := uuid.NewString()
	tags := []string{
		"k8s-generation:1",
		"k8s-group:configuration.konghq.com",
		"k8s-kind:KongCredentialBasicAuth",
		"k8s-name:" + KongCredentialBasicAuth.Name,
		"k8s-uid:" + string(KongCredentialBasicAuth.GetUID()),
		"k8s-version:v1alpha1",
		"k8s-namespace:" + ns.Name,
	}

	factory := ops.NewMockSDKFactory(t)
	factory.SDK.KongCredentialsBasicAuthSDK.EXPECT().
		CreateBasicAuthWithConsumer(
			mock.Anything,
			sdkkonnectops.CreateBasicAuthWithConsumerRequest{
				ControlPlaneID:              cp.GetKonnectStatus().GetKonnectID(),
				ConsumerIDForNestedEntities: consumerID,
				BasicAuthWithoutParents: sdkkonnectcomp.BasicAuthWithoutParents{
					Password: lo.ToPtr(password),
					Username: lo.ToPtr(username),
					Tags:     tags,
				},
			},
		).
		Return(
			&sdkkonnectops.CreateBasicAuthWithConsumerResponse{
				BasicAuth: &sdkkonnectcomp.BasicAuth{
					ID: lo.ToPtr(basicAuthID),
				},
			},
			nil,
		)
	factory.SDK.KongCredentialsBasicAuthSDK.EXPECT().
		UpsertBasicAuthWithConsumer(mock.Anything, mock.Anything, mock.Anything).Maybe().
		Return(
			&sdkkonnectops.UpsertBasicAuthWithConsumerResponse{
				BasicAuth: &sdkkonnectcomp.BasicAuth{
					ID: lo.ToPtr(basicAuthID),
				},
			},
			nil,
		)

	require.NoError(t, manager.SetupCacheIndicesForKonnectTypes(ctx, mgr, false))
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialBasicAuth](konnectSyncTime),
		),
	}

	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KongCredentialsBasicAuthSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	factory.SDK.KongCredentialsBasicAuthSDK.EXPECT().
		DeleteBasicAuthWithConsumer(
			mock.Anything,
			sdkkonnectops.DeleteBasicAuthWithConsumerRequest{
				ControlPlaneID:              cp.GetKonnectStatus().GetKonnectID(),
				ConsumerIDForNestedEntities: consumerID,
				BasicAuthID:                 basicAuthID,
			},
		).
		Return(
			&sdkkonnectops.DeleteBasicAuthWithConsumerResponse{
				StatusCode: 200,
			},
			nil,
		)
	require.NoError(t, clientNamespaced.Delete(ctx, KongCredentialBasicAuth))

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KongCredentialsBasicAuthSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	w := setupWatch[configurationv1alpha1.KongCredentialBasicAuthList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))

	KongCredentialBasicAuth = deployKongCredentialBasicAuth(t, ctx, clientNamespaced, consumer.Name, username, password)
	t.Logf("redeployed %s KongCredentialBasicAuth resource", client.ObjectKeyFromObject(KongCredentialBasicAuth))
	t.Logf("checking if KongConsumer %s removal will delete the associated credentials %s",
		client.ObjectKeyFromObject(consumer),
		client.ObjectKeyFromObject(KongCredentialBasicAuth),
	)

	require.NoError(t, clientNamespaced.Delete(ctx, consumer))
	_ = watchFor(t, ctx, w, watch.Modified,
		func(c *configurationv1alpha1.KongCredentialBasicAuth) bool {
			return c.Name == KongCredentialBasicAuth.Name
		},
		"KongCredentialBasicAuth wasn't deleted but it should have been",
	)
}
