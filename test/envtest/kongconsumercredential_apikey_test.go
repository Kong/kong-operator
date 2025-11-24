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

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongConsumerCredential_APIKey(t *testing.T) {
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
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, mgr, clientNamespaced, apiAuth)

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
	consumer.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID: cp.GetKonnectStatus().GetKonnectID(),
		KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
			ID:        consumerID,
			ServerURL: cp.GetKonnectStatus().GetServerURL(),
			OrgID:     cp.GetKonnectStatus().GetOrgID(),
		},
	}
	require.NoError(t, clientNamespaced.Status().Update(ctx, consumer))

	kongCredentialAPIKey := deploy.KongCredentialAPIKey(t, ctx, clientNamespaced, consumer.Name)
	keyID := uuid.NewString()
	tags := []string{
		"k8s-generation:1",
		"k8s-group:configuration.konghq.com",
		"k8s-kind:KongCredentialAPIKey",
		"k8s-name:" + kongCredentialAPIKey.Name,
		"k8s-namespace:" + ns.Name,
		"k8s-uid:" + string(kongCredentialAPIKey.GetUID()),
		"k8s-version:v1alpha1",
	}

	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK.KongCredentialsAPIKeySDK

	sdk.EXPECT().
		CreateKeyAuthWithConsumer(
			mock.Anything,
			sdkkonnectops.CreateKeyAuthWithConsumerRequest{
				ControlPlaneID:              cp.GetKonnectStatus().GetKonnectID(),
				ConsumerIDForNestedEntities: consumerID,
				KeyAuthWithoutParents: &sdkkonnectcomp.KeyAuthWithoutParents{
					Key:  lo.ToPtr("key"),
					Tags: tags,
				},
			},
		).
		Return(
			&sdkkonnectops.CreateKeyAuthWithConsumerResponse{
				KeyAuth: &sdkkonnectcomp.KeyAuth{
					ID: lo.ToPtr(keyID),
				},
			},
			nil,
		)
	sdk.EXPECT().
		UpsertKeyAuthWithConsumer(mock.Anything, mock.Anything, mock.Anything).Maybe().
		Return(
			&sdkkonnectops.UpsertKeyAuthWithConsumerResponse{
				KeyAuth: &sdkkonnectcomp.KeyAuth{
					ID: lo.ToPtr(keyID),
				},
			},
			nil,
		)

	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCredentialAPIKey](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongCredentialAPIKey](&metricsmocks.MockRecorder{}),
		),
	}

	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	assert.EventuallyWithT(t,
		assertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kongCredentialAPIKey, keyID),
		waitTime, tickTime,
		"KongCredentialAPIKey wasn't created",
	)

	eventuallyAssertSDKExpectations(t, sdk, waitTime, tickTime)

	sdk.EXPECT().
		DeleteKeyAuthWithConsumer(
			mock.Anything,
			sdkkonnectops.DeleteKeyAuthWithConsumerRequest{
				ControlPlaneID:              cp.GetKonnectStatus().GetKonnectID(),
				ConsumerIDForNestedEntities: consumerID,
				KeyAuthID:                   keyID,
			},
		).
		Return(
			&sdkkonnectops.DeleteKeyAuthWithConsumerResponse{
				StatusCode: 200,
			},
			nil,
		)

	require.NoError(t, clientNamespaced.Delete(ctx, kongCredentialAPIKey))

	assert.EventuallyWithT(t,
		func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongCredentialAPIKey), kongCredentialAPIKey),
			))
		}, waitTime, tickTime,
		"KongCredentialAPIKey wasn't deleted but it should have been",
	)

	eventuallyAssertSDKExpectations(t, sdk, waitTime, tickTime)

	t.Run("conflict on creation should be handled successfully", func(t *testing.T) {
		t.Log("Setting up SDK expectations on creation with conflict")
		sdk.EXPECT().
			CreateKeyAuthWithConsumer(
				mock.Anything,
				mock.MatchedBy(func(r sdkkonnectops.CreateKeyAuthWithConsumerRequest) bool {
					return r.ControlPlaneID == cp.GetKonnectID() &&
						r.ConsumerIDForNestedEntities == consumerID &&
						r.KeyAuthWithoutParents.Tags != nil &&
						slices.ContainsFunc(
							r.KeyAuthWithoutParents.Tags,
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
			ListKeyAuth(
				mock.Anything,
				mock.MatchedBy(func(r sdkkonnectops.ListKeyAuthRequest) bool {
					return r.ControlPlaneID == cp.GetKonnectID() &&
						r.Tags != nil && strings.HasPrefix(*r.Tags, "k8s-uid")
				}),
			).
			Return(&sdkkonnectops.ListKeyAuthResponse{
				Object: &sdkkonnectops.ListKeyAuthResponseBody{
					Data: []sdkkonnectcomp.KeyAuth{
						{
							ID: lo.ToPtr(keyID),
						},
					},
				},
			}, nil)

		w := setupWatch[configurationv1alpha1.KongCredentialAPIKeyList](t, ctx, cl, client.InNamespace(ns.Name))
		created := deploy.KongCredentialAPIKey(t, ctx, clientNamespaced, consumer.Name)

		t.Log("Waiting for KongCredentialAPIKey to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(k *configurationv1alpha1.KongCredentialAPIKey) bool {
			return k.GetName() == created.GetName() && k8sutils.IsProgrammed(k)
		}, "KongCredentialAPIKey's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, sdk, waitTime, tickTime)
	})
}
