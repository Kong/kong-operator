package envtest

import (
	"slices"
	"strings"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"

	"github.com/kong/kong-operator/controller/konnect"
	sdkmocks "github.com/kong/kong-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
)

func TestKongConsumerCredential_ACL(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()

	// Setup up the envtest environment.
	cfg, ns := Setup(t, ctx, scheme.Get())

	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
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
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
		},
	})
	consumer.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
		KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
			ID:        consumerID,
			ServerURL: cp.GetKonnectStatus().GetServerURL(),
			OrgID:     cp.GetKonnectStatus().GetOrgID(),
		},
	}
	require.NoError(t, clientNamespaced.Status().Update(ctx, consumer))

	aclGroup := "acl-group1"
	kongCredentialACL := deploy.KongCredentialACL(t, ctx, clientNamespaced, consumer.Name, aclGroup)
	aclID := uuid.NewString()
	tags := []string{
		"k8s-generation:1",
		"k8s-group:configuration.konghq.com",
		"k8s-kind:KongCredentialACL",
		"k8s-name:" + kongCredentialACL.Name,
		"k8s-namespace:" + ns.Name,
		"k8s-uid:" + string(kongCredentialACL.GetUID()),
		"k8s-version:v1alpha1",
	}

	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK.KongCredentialsACLSDK
	sdk.EXPECT().
		CreateACLWithConsumer(
			mock.Anything,
			sdkkonnectops.CreateACLWithConsumerRequest{
				ControlPlaneID:              cp.GetKonnectStatus().GetKonnectID(),
				ConsumerIDForNestedEntities: consumerID,
				ACLWithoutParents: sdkkonnectcomp.ACLWithoutParents{
					Group: aclGroup,
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
	sdk.EXPECT().
		UpsertACLWithConsumer(mock.Anything, mock.Anything, mock.Anything).Maybe().
		Return(
			&sdkkonnectops.UpsertACLWithConsumerResponse{
				ACL: &sdkkonnectcomp.ACL{
					ID: lo.ToPtr(aclID),
				},
			},
			nil,
		)

	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialACL](konnectInfiniteSyncTime),
		),
	}

	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	assert.EventuallyWithT(t,
		assertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kongCredentialACL, aclID),
		waitTime, tickTime,
		"KongCredentialACL wasn't created",
	)

	eventuallyAssertSDKExpectations(t, sdk, waitTime, tickTime)

	sdk.EXPECT().
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
	require.NoError(t, clientNamespaced.Delete(ctx, kongCredentialACL))

	assert.EventuallyWithT(t,
		func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongCredentialACL), kongCredentialACL),
			))
		}, waitTime, tickTime,
		"KongCredentialACL wasn't deleted but it should have been",
	)

	eventuallyAssertSDKExpectations(t, sdk, waitTime, tickTime)

	t.Run("conflict on creation should be handled successfully", func(t *testing.T) {
		t.Log("Setting up SDK expectations on creation with conflict")
		sdk.EXPECT().
			CreateACLWithConsumer(
				mock.Anything,
				mock.MatchedBy(func(r sdkkonnectops.CreateACLWithConsumerRequest) bool {
					return r.ControlPlaneID == cp.GetKonnectID() &&
						r.ConsumerIDForNestedEntities == consumerID &&
						r.ACLWithoutParents.Group == aclGroup &&
						r.ACLWithoutParents.Tags != nil &&
						slices.ContainsFunc(
							r.ACLWithoutParents.Tags,
							func(t string) bool {
								return strings.HasPrefix(t, "k8s-uid:")
							},
						)
				},
				),
			).
			Return(
				nil,
				&sdkkonnecterrs.SDKError{
					StatusCode: 400,
					Body:       ErrBodyDataConstraintError,
				},
			)

		sdk.EXPECT().
			ListACL(
				mock.Anything,
				mock.MatchedBy(func(r sdkkonnectops.ListACLRequest) bool {
					return r.ControlPlaneID == cp.GetKonnectID() &&
						r.Tags != nil && strings.HasPrefix(*r.Tags, "k8s-uid")
				}),
			).
			Return(&sdkkonnectops.ListACLResponse{
				Object: &sdkkonnectops.ListACLResponseBody{
					Data: []sdkkonnectcomp.ACL{
						{
							ID: lo.ToPtr(aclID),
						},
					},
				},
			}, nil)

		w := setupWatch[configurationv1alpha1.KongCredentialACLList](t, ctx, cl, client.InNamespace(ns.Name))
		created := deploy.KongCredentialACL(t, ctx, clientNamespaced, consumer.Name, aclGroup)

		t.Log("Waiting for KongCredentialACL to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(k *configurationv1alpha1.KongCredentialACL) bool {
			return k.GetName() == created.GetName() && k8sutils.IsProgrammed(k)
		}, "KongCredentialACL's Programmed condition should be true eventually")

		t.Log("Checking SDK KongCredentialACL operations")
		eventuallyAssertSDKExpectations(t, sdk, waitTime, tickTime)
	})
}
