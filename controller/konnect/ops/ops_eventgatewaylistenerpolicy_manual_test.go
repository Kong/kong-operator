package ops

import (
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestCreateEventGatewayListenerPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayListenerPoliciesSDK(t)
	cl := testEventGatewayListenerPolicyClient()
	policy := testEventGatewayListenerPolicy()

	expectedRequest, err := policy.ToCreateEventGatewayListenerPolicyRequest(ctx, cl)
	require.NoError(t, err)
	expectedRequest.GatewayID = "gateway-1"
	expectedRequest.ListenerID = "listener-1"
	createVariant := expectedRequest.EventGatewayListenerPolicyCreate.EventGatewayTLSListenerPolicy
	createVariant.Labels = WithKubernetesMetadataLabels(policy, createVariant.Labels)
	require.NotNil(t, createVariant.Config.ClientAuthentication)
	assert.Equal(t,
		[]sdkkonnectcomp.TLSTrustBundleReference{
			sdkkonnectcomp.CreateTLSTrustBundleReferenceTLSTrustBundleReferenceByID(
				sdkkonnectcomp.TLSTrustBundleReferenceByID{ID: "trust-bundle-by-id"},
			),
			sdkkonnectcomp.CreateTLSTrustBundleReferenceTLSTrustBundleReferenceByID(
				sdkkonnectcomp.TLSTrustBundleReferenceByID{ID: "trust-bundle-1"},
			),
			sdkkonnectcomp.CreateTLSTrustBundleReferenceTLSTrustBundleReferenceByName(
				sdkkonnectcomp.TLSTrustBundleReferenceByName{Name: "trust-bundle-by-name"},
			),
		},
		createVariant.Config.ClientAuthentication.TLSTrustBundles,
		"Konnect IDs and names should be kept as-is and the EventGatewayTLSTrustBundle reference resolved to its Konnect ID, in order",
	)

	sdk.EXPECT().
		CreateEventGatewayListenerPolicy(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.CreateEventGatewayListenerPolicyResponse{
			EventGatewayListenerPolicy: &sdkkonnectcomp.EventGatewayListenerPolicy{
				ID: "listener-policy-1",
			},
		}, nil).
		Once()

	require.NoError(t, createEventGatewayListenerPolicy(ctx, cl, sdk, policy))
	assert.Equal(t, "listener-policy-1", policy.GetKonnectID())
}

func TestEventGatewayListenerPolicyKonnectTLSTrustBundleReferences(t *testing.T) {
	t.Parallel()

	// Trust bundles referenced by Konnect ID or name only (the form accepted
	// before EventGatewayTLSTrustBundle existed) need no in-cluster objects and
	// are sent as-is.
	ctx := t.Context()
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).Build()
	policy := testEventGatewayListenerPolicy()
	policy.Spec.APISpec.EventGatewayTLSListen.Config.ClientAuthentication.TLSTrustBundles = []configurationv1alpha1.TLSTrustBundleReference{
		{ID: new("trust-bundle-by-id")},
		{Name: new(configurationv1alpha1.TLSTrustBundleName("trust-bundle-by-name"))},
	}

	req, err := policy.ToCreateEventGatewayListenerPolicyRequest(ctx, cl)
	require.NoError(t, err)
	createVariant := req.EventGatewayListenerPolicyCreate.EventGatewayTLSListenerPolicy
	require.NotNil(t, createVariant)
	require.NotNil(t, createVariant.Config.ClientAuthentication)
	assert.Equal(t,
		[]sdkkonnectcomp.TLSTrustBundleReference{
			sdkkonnectcomp.CreateTLSTrustBundleReferenceTLSTrustBundleReferenceByID(
				sdkkonnectcomp.TLSTrustBundleReferenceByID{ID: "trust-bundle-by-id"},
			),
			sdkkonnectcomp.CreateTLSTrustBundleReferenceTLSTrustBundleReferenceByName(
				sdkkonnectcomp.TLSTrustBundleReferenceByName{Name: "trust-bundle-by-name"},
			),
		},
		createVariant.Config.ClientAuthentication.TLSTrustBundles,
	)
}

func TestEventGatewayListenerPolicyTLSTrustBundleReferenceResolutionErrors(t *testing.T) {
	t.Parallel()

	trustBundle := func(mutate func(*configurationv1alpha1.EventGatewayTLSTrustBundle)) *configurationv1alpha1.EventGatewayTLSTrustBundle {
		tb := &configurationv1alpha1.EventGatewayTLSTrustBundle{
			Name: "trust-bundle", Namespace: "default",
			Status: configurationv1alpha1.EventGatewayTLSTrustBundleStatus{
				GatewayID: &configurationv1alpha1.KonnectEntityRef{ID: "gateway-1"},
			},
		}
		tb.SetKonnectID("trust-bundle-1")
		if mutate != nil {
			mutate(tb)
		}
		return tb
	}
	withRef := func(ref configurationv1alpha1.EventGatewayTLSTrustBundleRef) *configurationv1alpha1.EventGatewayListenerPolicy {
		policy := testEventGatewayListenerPolicy()
		policy.Spec.APISpec.EventGatewayTLSListen.Config.ClientAuthentication.TLSTrustBundles = []configurationv1alpha1.TLSTrustBundleReference{
			{NamespacedRef: &ref},
		}
		return policy
	}

	testCases := []struct {
		name      string
		objects   []client.Object
		policy    *configurationv1alpha1.EventGatewayListenerPolicy
		assertErr func(t *testing.T, err error)
	}{
		{
			name:   "referenced trust bundle not found",
			policy: withRef(configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "trust-bundle"}),
			assertErr: func(t *testing.T, err error) {
				var target configurationv1alpha1.ReferenceNotFoundError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "referenced trust bundle not programmed",
			objects: []client.Object{trustBundle(func(tb *configurationv1alpha1.EventGatewayTLSTrustBundle) {
				tb.SetKonnectID("")
			})},
			policy: withRef(configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "trust-bundle"}),
			assertErr: func(t *testing.T, err error) {
				var target configurationv1alpha1.ReferenceNotProgrammedError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "referenced trust bundle on a different gateway",
			objects: []client.Object{trustBundle(func(tb *configurationv1alpha1.EventGatewayTLSTrustBundle) {
				tb.Status.GatewayID = &configurationv1alpha1.KonnectEntityRef{ID: "gateway-2"}
			})},
			policy: withRef(configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "trust-bundle"}),
			assertErr: func(t *testing.T, err error) {
				var target configurationv1alpha1.ReferenceDifferentGatewayError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "referenced trust bundle being deleted, policy not created in Konnect yet",
			objects: []client.Object{trustBundle(func(tb *configurationv1alpha1.EventGatewayTLSTrustBundle) {
				tb.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				tb.Finalizers = []string{"test/finalizer"}
			})},
			policy: withRef(configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "trust-bundle"}),
			assertErr: func(t *testing.T, err error) {
				var target configurationv1alpha1.ReferenceBeingDeletedError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name:    "referenced trust bundle in another namespace",
			objects: []client.Object{trustBundle(nil)},
			policy:  withRef(configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "trust-bundle", Namespace: "other"}),
			assertErr: func(t *testing.T, err error) {
				var target configurationv1alpha1.ReferenceCrossNamespaceError
				assert.ErrorAs(t, err, &target)
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(tc.objects...).Build()
			_, err := tc.policy.ToCreateEventGatewayListenerPolicyRequest(t.Context(), cl)
			require.Error(t, err)
			tc.assertErr(t, err)
			_, err = tc.policy.ToUpdateEventGatewayListenerPolicyRequest(t.Context(), cl)
			require.Error(t, err)
			tc.assertErr(t, err)
			err = tc.policy.ResolveKonnectReferences(t.Context(), cl)
			require.Error(t, err)
			tc.assertErr(t, err)
		})
	}
}

func TestEventGatewayListenerPolicyKeepsResolvingTLSTrustBundleBeingDeleted(t *testing.T) {
	t.Parallel()

	// A policy already created in Konnect keeps syncing while the trust bundle
	// it references is being deleted (its deletion waits for the policy to
	// drop the reference); only policies not created in Konnect yet are
	// refused a trust bundle being deleted (see the resolution errors test).
	trustBundle := &configurationv1alpha1.EventGatewayTLSTrustBundle{
		Name:              "trust-bundle",
		Namespace:         "default",
		DeletionTimestamp: &metav1.Time{Time: time.Now()},
		Finalizers:        []string{"test/finalizer"},
		Status: configurationv1alpha1.EventGatewayTLSTrustBundleStatus{
			GatewayID: &configurationv1alpha1.KonnectEntityRef{ID: "gateway-1"},
		},
	}
	trustBundle.SetKonnectID("trust-bundle-1")
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(trustBundle).Build()
	policy := testEventGatewayListenerPolicy()
	policy.Spec.APISpec.EventGatewayTLSListen.Config.ClientAuthentication.TLSTrustBundles = []configurationv1alpha1.TLSTrustBundleReference{
		{NamespacedRef: &configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "trust-bundle"}},
	}
	policy.SetKonnectID("listener-policy-1")

	req, err := policy.ToUpdateEventGatewayListenerPolicyRequest(t.Context(), cl)
	require.NoError(t, err)
	updateVariant := req.EventGatewayListenerPolicyUpdate.EventGatewayTLSListenerSensitiveDataAwarePolicy
	require.NotNil(t, updateVariant)
	require.NotNil(t, updateVariant.Config.ClientAuthentication)
	assert.Equal(t,
		[]sdkkonnectcomp.TLSTrustBundleReference{
			sdkkonnectcomp.CreateTLSTrustBundleReferenceTLSTrustBundleReferenceByID(
				sdkkonnectcomp.TLSTrustBundleReferenceByID{ID: "trust-bundle-1"},
			),
		},
		updateVariant.Config.ClientAuthentication.TLSTrustBundles,
	)
}

func TestEventGatewayListenerPolicyRefsToEventGatewayTLSTrustBundle(t *testing.T) {
	t.Parallel()

	// The EventGatewayTLSTrustBundle controller watches listener policies
	// (reverseWatch) and re-reconciles the trust bundles they reference.
	policy := testEventGatewayListenerPolicy()
	policy.Spec.APISpec.EventGatewayTLSListen.Config.ClientAuthentication.TLSTrustBundles = []configurationv1alpha1.TLSTrustBundleReference{
		{ID: new("trust-bundle-by-id")},
		{NamespacedRef: &configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "trust-bundle"}},
		{Name: new(configurationv1alpha1.TLSTrustBundleName("trust-bundle-by-name"))},
	}
	assert.Equal(t,
		[]client.ObjectKey{{Namespace: "default", Name: "trust-bundle"}},
		configurationv1alpha1.EventGatewayListenerPolicyRefsToEventGatewayTLSTrustBundle(policy),
	)
}

func TestUpdateEventGatewayListenerPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayListenerPoliciesSDK(t)
	policy := testEventGatewayListenerPolicy()
	cl := testEventGatewayListenerPolicyClient()
	policy.SetKonnectID("listener-policy-1")

	expectedRequest, err := policy.ToUpdateEventGatewayListenerPolicyRequest(ctx, cl)
	require.NoError(t, err)
	expectedRequest.GatewayID = "gateway-1"
	expectedRequest.ListenerID = "listener-1"
	expectedRequest.PolicyID = "listener-policy-1"
	updateVariant := expectedRequest.EventGatewayListenerPolicyUpdate.EventGatewayTLSListenerSensitiveDataAwarePolicy
	updateVariant.Labels = WithKubernetesMetadataLabels(policy, updateVariant.Labels)
	require.NotNil(t, updateVariant.Config.ClientAuthentication)
	assert.Equal(t,
		[]sdkkonnectcomp.TLSTrustBundleReference{
			sdkkonnectcomp.CreateTLSTrustBundleReferenceTLSTrustBundleReferenceByID(
				sdkkonnectcomp.TLSTrustBundleReferenceByID{ID: "trust-bundle-by-id"},
			),
			sdkkonnectcomp.CreateTLSTrustBundleReferenceTLSTrustBundleReferenceByID(
				sdkkonnectcomp.TLSTrustBundleReferenceByID{ID: "trust-bundle-1"},
			),
			sdkkonnectcomp.CreateTLSTrustBundleReferenceTLSTrustBundleReferenceByName(
				sdkkonnectcomp.TLSTrustBundleReferenceByName{Name: "trust-bundle-by-name"},
			),
		},
		updateVariant.Config.ClientAuthentication.TLSTrustBundles,
		"Konnect IDs and names should be kept as-is and the EventGatewayTLSTrustBundle reference resolved to its Konnect ID, in order",
	)

	sdk.EXPECT().
		UpdateEventGatewayListenerPolicy(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.UpdateEventGatewayListenerPolicyResponse{}, nil).
		Once()

	require.NoError(t, updateEventGatewayListenerPolicy(ctx, cl, sdk, policy))
}

func TestDeleteEventGatewayListenerPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayListenerPoliciesSDK(t)
	policy := testEventGatewayListenerPolicy()
	policy.SetKonnectID("listener-policy-1")

	sdk.EXPECT().
		DeleteEventGatewayListenerPolicy(mock.Anything, sdkkonnectops.DeleteEventGatewayListenerPolicyRequest{
			GatewayID:  "gateway-1",
			ListenerID: "listener-1",
			PolicyID:   "listener-policy-1",
		}).
		Return(&sdkkonnectops.DeleteEventGatewayListenerPolicyResponse{}, nil).
		Once()

	require.NoError(t, deleteEventGatewayListenerPolicy(ctx, sdk, policy))
}

func TestGetEventGatewayListenerPolicyForUID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayListenerPoliciesSDK(t)
	policy := testEventGatewayListenerPolicy()

	sdk.EXPECT().
		ListEventGatewayListenerPolicies(mock.Anything, sdkkonnectops.ListEventGatewayListenerPoliciesRequest{
			GatewayID:  "gateway-1",
			ListenerID: "listener-1",
		}).
		Return(&sdkkonnectops.ListEventGatewayListenerPoliciesResponse{
			ListEventGatewayListenerPoliciesResponse: []sdkkonnectcomp.EventGatewayListenerPolicy{
				{
					// Same type and name, but created for another object.
					ID:     "other-policy",
					Type:   "tls_server",
					Name:   new("tls-policy"),
					Labels: map[string]string{KubernetesUIDLabelKey: "other-uid"},
				},
				{
					ID:     "listener-policy-1",
					Type:   "tls_server",
					Name:   new("tls-policy"),
					Labels: map[string]string{KubernetesUIDLabelKey: string(policy.GetUID())},
				},
			},
		}, nil).
		Once()

	id, err := getEventGatewayListenerPolicyForUID(ctx, sdk, policy)
	require.NoError(t, err)
	assert.Equal(t, "listener-policy-1", id)
}

func testEventGatewayListenerPolicy() *configurationv1alpha1.EventGatewayListenerPolicy {
	return &configurationv1alpha1.EventGatewayListenerPolicy{
		APIVersion: configurationv1alpha1.GroupVersion.String(),
		Kind:       "EventGatewayListenerPolicy",
		Name:       "listener-policy",
		Namespace:  "default",
		UID:        "listener-policy-uid",
		Generation: 2,
		Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{
			EventGatewayListenerRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-gateway-listener",
				},
			},
			APISpec: configurationv1alpha1.EventGatewayListenerPolicyAPISpec{
				EventGatewayListenerPolicyConfig: &configurationv1alpha1.EventGatewayListenerPolicyConfig{
					Type: configurationv1alpha1.EventGatewayListenerPolicyConfigTypeEventGatewayTLSListen,
					EventGatewayTLSListen: &configurationv1alpha1.EventGatewayTLSListenerPolicy{
						Name:        "tls-policy",
						Description: "listener tls policy",
						Config: configurationv1alpha1.EventGatewayTLSListenerPolicyConfig{
							Certificates: []configurationv1alpha1.TLSCertificate{
								{
									Certificate: configurationv1alpha1.SensitiveDataSource{Type: configurationv1alpha1.SensitiveDataSourceTypeInline, Value: new("-----BEGIN CERTIFICATE-----test-----END CERTIFICATE-----")},
									Key:         configurationv1alpha1.SensitiveDataSource{Type: configurationv1alpha1.SensitiveDataSourceTypeInline, Value: new("-----BEGIN PRIVATE KEY-----test-----END PRIVATE KEY-----")},
								},
							},
							ClientAuthentication: configurationv1alpha1.EventGatewayTLSListenerPolicyConfigClientAuthentication{
								Mode: "requested",
								TLSTrustBundles: []configurationv1alpha1.TLSTrustBundleReference{
									{
										ID: new("trust-bundle-by-id"),
									},
									{
										NamespacedRef: &configurationv1alpha1.EventGatewayTLSTrustBundleRef{
											Name: "trust-bundle",
										},
									},
									{
										Name: new(configurationv1alpha1.TLSTrustBundleName("trust-bundle-by-name")),
									},
								},
							},
						},
					},
				},
			},
		},
		Status: configurationv1alpha1.EventGatewayListenerPolicyStatus{
			GatewayID: &configurationv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
			EventGatewayListenerID: &configurationv1alpha1.KonnectEntityRef{
				ID: "listener-1",
			},
		},
	}
}

// testEventGatewayListenerPolicyClient returns a fake client holding the
// programmed EventGatewayTLSTrustBundle referenced by testEventGatewayListenerPolicy.
func testEventGatewayListenerPolicyClient() client.Client {
	trustBundle := &configurationv1alpha1.EventGatewayTLSTrustBundle{
		Name:      "trust-bundle",
		Namespace: "default",
		Spec: configurationv1alpha1.EventGatewayTLSTrustBundleSpec{
			APISpec: configurationv1alpha1.EventGatewayTLSTrustBundleAPISpec{
				Name: "konnect-trust-bundle",
			},
		},
		Status: configurationv1alpha1.EventGatewayTLSTrustBundleStatus{
			GatewayID: &configurationv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
		},
	}
	trustBundle.SetKonnectID("trust-bundle-1")
	return fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(trustBundle).Build()
}
