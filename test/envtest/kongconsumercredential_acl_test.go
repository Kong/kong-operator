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

func TestKongConsumerCredential_ACL(t *testing.T) {
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

	aclGroup := "acl-group1"
	KongCredentialACL := deployKongCredentialACL(t, ctx, clientNamespaced, consumer.Name, aclGroup)
	aclID := uuid.NewString()
	tags := []string{
		"k8s-generation:1",
		"k8s-group:configuration.konghq.com",
		"k8s-kind:KongCredentialACL",
		"k8s-name:" + KongCredentialACL.Name,
		"k8s-namespace:" + ns.Name,
		"k8s-uid:" + string(KongCredentialACL.GetUID()),
		"k8s-version:v1alpha1",
	}

	factory := ops.NewMockSDKFactory(t)
	factory.SDK.KongCredentialsACLSDK.EXPECT().
		CreateACLWithConsumer(
			mock.Anything,
			sdkkonnectops.CreateACLWithConsumerRequest{
				ControlPlaneID:              cp.GetKonnectStatus().GetKonnectID(),
				ConsumerIDForNestedEntities: consumerID,
				ACLWithoutParents: sdkkonnectcomp.ACLWithoutParents{
					Group: lo.ToPtr(aclGroup),
					Tags:  tags,
				},
			},
		).
		Return(
			&sdkkonnectops.CreateACLWithConsumerResponse{
				ACL: &sdkkonnectcomp.ACL{
					ID: lo.ToPtr(aclID),
				},
			},
			nil,
		)
	factory.SDK.KongCredentialsACLSDK.EXPECT().
		UpsertACLWithConsumer(mock.Anything, mock.Anything, mock.Anything).Maybe().
		Return(
			&sdkkonnectops.UpsertACLWithConsumerResponse{
				ACL: &sdkkonnectcomp.ACL{
					ID: lo.ToPtr(aclID),
				},
			},
			nil,
		)

	require.NoError(t, manager.SetupCacheIndicesForKonnectTypes(ctx, mgr, false))
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialACL](konnectSyncTime),
		),
	}

	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KongCredentialsACLSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	factory.SDK.KongCredentialsACLSDK.EXPECT().
		DeleteACLWithConsumer(
			mock.Anything,
			sdkkonnectops.DeleteACLWithConsumerRequest{
				ControlPlaneID:              cp.GetKonnectStatus().GetKonnectID(),
				ConsumerIDForNestedEntities: consumerID,
				ACLID:                       aclID,
			},
		).
		Return(
			&sdkkonnectops.DeleteACLWithConsumerResponse{
				StatusCode: 200,
			},
			nil,
		)
	require.NoError(t, clientNamespaced.Delete(ctx, KongCredentialACL))

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.KongCredentialsACLSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	w := setupWatch[configurationv1alpha1.KongCredentialACLList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))

	KongCredentialACL = deployKongCredentialACL(t, ctx, clientNamespaced, consumer.Name, aclGroup)
	t.Logf("redeployed %s KongCredentialACL resource", client.ObjectKeyFromObject(KongCredentialACL))
	t.Logf("checking if KongConsumer %s removal will delete the associated credentials %s",
		client.ObjectKeyFromObject(consumer),
		client.ObjectKeyFromObject(KongCredentialACL),
	)

	require.NoError(t, clientNamespaced.Delete(ctx, consumer))
	_ = watchFor(t, ctx, w, watch.Modified,
		func(c *configurationv1alpha1.KongCredentialACL) bool {
			return c.Name == KongCredentialACL.Name
		},
		"KongCredentialACL wasn't deleted but it should have been",
	)
}
