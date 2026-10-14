package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func onPremEventGatewayParentRef(name string) *commonv1alpha1.GatewayRef {
	return &commonv1alpha1.GatewayRef{
		Group: "eventgateway.konghq.com",
		Kind:  "OnPremEventGateway",
		Name:  name,
	}
}

func newTestOnPremEventGatewayBackendCluster(namespace, name, gatewayName string) *configurationv1alpha1.EventGatewayBackendCluster { //nolint:unparam
	return &configurationv1alpha1.EventGatewayBackendCluster{
		Name:      name,
		Namespace: namespace,
		Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{
			ParentRef: onPremEventGatewayParentRef(gatewayName),
		},
	}
}

func newTestEventGatewayProducePolicy(namespace, name, virtualClusterName string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy { //nolint:unparam
	return &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
		Name:      name,
		Namespace: namespace,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
			EventGatewayVirtualClusterRef: testNamespacedObjectRef(virtualClusterName),
		},
	}
}

// TestSkipsKonnectReconciliation covers the walk from an entity up to its
// gateway, which decides whether the Konnect reconciler manages the entity.
func TestSkipsKonnectReconciliation(t *testing.T) {
	const ns = "default"

	onPremBackend := newTestOnPremEventGatewayBackendCluster(ns, "onprem-backend", "onprem-gw")
	konnectBackend := newTestEventGatewayBackendCluster(ns, "konnect-backend", "konnect-gw")
	konnectGateway := newTestEventGateway(ns, "konnect-gw", "api-auth")
	onPremVirtualCluster := newTestEventGatewayVirtualCluster(ns, "onprem-vc", onPremBackend.Name)
	konnectVirtualCluster := newTestEventGatewayVirtualCluster(ns, "konnect-vc", konnectBackend.Name)
	orphanVirtualCluster := newTestEventGatewayVirtualCluster(ns, "orphan-vc", "missing-backend")
	onPremListener := &configurationv1alpha1.EventGatewayListener{
		Name:      "onprem-listener",
		Namespace: ns,
		Spec: configurationv1alpha1.EventGatewayListenerSpec{
			ParentRef: onPremEventGatewayParentRef("onprem-gw"),
		},
	}
	onPremListenerPolicy := &configurationv1alpha1.EventGatewayListenerPolicy{
		Name:      "onprem-listener-policy",
		Namespace: ns,
		Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{
			EventGatewayListenerRef: testNamespacedObjectRef(onPremListener.Name),
		},
	}

	// A backend cluster in another namespace, attached to an OnPremEventGateway,
	// with the same name as a Konnect one in this namespace: the walk must
	// follow the namespace of the reference.
	otherNamespaceBackend := newTestOnPremEventGatewayBackendCluster("other", konnectBackend.Name, "onprem-gw")
	crossNamespaceVirtualCluster := newTestEventGatewayVirtualCluster(ns, "cross-namespace-vc", konnectBackend.Name)
	crossNamespaceVirtualCluster.Spec.EventGatewayBackendClusterRef.NamespacedRef.Namespace = new("other")

	objects := []client.Object{
		onPremBackend, konnectBackend, konnectGateway,
		onPremVirtualCluster, konnectVirtualCluster, orphanVirtualCluster,
		onPremListener, otherNamespaceBackend,
	}

	tests := []struct {
		name string
		obj  client.Object
		want bool
	}{
		{
			name: "root entity referencing an OnPremEventGateway",
			obj:  onPremBackend,
			want: true,
		},
		{
			name: "root entity referencing a KonnectEventGateway",
			obj:  konnectBackend,
		},
		{
			name: "virtual cluster under an on-prem backend cluster",
			obj:  onPremVirtualCluster,
			want: true,
		},
		{
			name: "virtual cluster under a Konnect backend cluster",
			obj:  konnectVirtualCluster,
		},
		{
			name: "policy two levels under an on-prem backend cluster",
			obj:  newTestEventGatewayProducePolicy(ns, "policy", onPremVirtualCluster.Name),
			want: true,
		},
		{
			name: "policy two levels under a Konnect backend cluster",
			obj:  newTestEventGatewayProducePolicy(ns, "policy", konnectVirtualCluster.Name),
		},
		{
			name: "listener policy under an on-prem listener",
			obj:  onPremListenerPolicy,
			want: true,
		},
		{
			name: "virtual cluster under an on-prem backend cluster in another namespace",
			obj:  crossNamespaceVirtualCluster,
			want: true,
		},
		{
			name: "missing ancestor",
			obj:  newTestEventGatewayProducePolicy(ns, "policy", orphanVirtualCluster.Name),
		},
		{
			name: "missing parent",
			obj:  newTestEventGatewayProducePolicy(ns, "policy", "missing-vc"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(objects...).
				Build()
			assert.Equal(t, tc.want, skipsKonnectReconciliation(t.Context(), cl, tc.obj))
		})
	}

	t.Run("an ancestor that cannot be read is left to the regular parent handling", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(objects...).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configurationv1alpha1.EventGatewayBackendCluster); ok {
						return assert.AnError
					}
					return cl.Get(ctx, key, obj, opts...)
				},
			}).
			Build()
		assert.False(t, skipsKonnectReconciliation(t.Context(), cl, onPremVirtualCluster))
	})
}

// TestHandleParentRef_OnPremEventGatewayAncestor verifies that an entity whose
// parent is Konnect-agnostic, but whose ancestor references an
// OnPremEventGateway, is not managed by the Konnect reconciler: it gets no
// parent-ref condition and no cleanup finalizer, even though its parent is not
// Programmed.
func TestHandleParentRef_OnPremEventGatewayAncestor(t *testing.T) {
	const ns = "default"

	backend := newTestOnPremEventGatewayBackendCluster(ns, "backend", "onprem-gw")
	virtualCluster := newTestEventGatewayVirtualCluster(ns, "virtual-cluster", backend.Name)
	policy := newTestEventGatewayProducePolicy(ns, "policy", virtualCluster.Name)
	policy.Finalizers = []string{KonnectCleanupFinalizer}

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithStatusSubresource(policy).
		WithObjects(backend, virtualCluster, policy).
		Build()

	handler := parentRefHandler[configurationv1alpha1.EventGatewayVirtualCluster, *configurationv1alpha1.EventGatewayVirtualCluster]{}
	res, err := handler.handleParentRef(t.Context(), cl, policy)
	require.True(t, res.IsZero())
	var notManagedErr ReferencedObjectNotKonnectManagedError
	require.ErrorAs(t, err, &notManagedErr)
	require.Equal(t, virtualCluster.Name, notManagedErr.Reference.Name)

	stop, res, err := handleRefResult(t.Context(), cl, policy, res, err)
	require.True(t, stop)
	require.True(t, res.IsZero())
	require.NoError(t, err)

	updated := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(policy), updated))
	require.Empty(t, updated.Status.Conditions, "no Konnect condition must be set")
	require.Empty(t, updated.Finalizers, "cleanup finalizer must be removed")
}

// TestSkipsKonnectReconciliation_OtherKinds covers the kinds outside the Event
// Gateway that go through the same parent handling: AI Gateway entities, which
// can be attached to an OnPremAIGateway, and Portal entities, which can't.
func TestSkipsKonnectReconciliation_OtherKinds(t *testing.T) {
	const ns = "default"

	onPremConsumer := newTestAIGatewayConsumer(ns, "onprem-consumer", "onprem-gw")
	onPremConsumer.Spec.AIGatewayRef.Group = aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem
	onPremConsumer.Spec.AIGatewayRef.Kind = aiconfigurationv1alpha1.AIGatewayRefKindOnPrem
	konnectConsumer := newTestAIGatewayConsumer(ns, "konnect-consumer", "konnect-ai-gw")
	konnectAIGateway := newTestKonnectAIGateway(ns, "konnect-ai-gw", "api-auth")
	portal := &konnectv1alpha1.Portal{Name: "portal", Namespace: ns}

	tests := []struct {
		name string
		obj  client.Object
		want bool
		// wantGets is the number of reads the walk makes.
		wantGets int
	}{
		{
			name:     "AI Gateway consumer attached to an OnPremAIGateway",
			obj:      onPremConsumer,
			want:     true,
			wantGets: 0,
		},
		{
			// The consumer's KonnectAIGateway is read, and has no parent itself.
			name:     "AI Gateway consumer attached to a KonnectAIGateway",
			obj:      konnectConsumer,
			wantGets: 1,
		},
		{
			// Portal has no parent: the walk stops without reading anything.
			name:     "Portal",
			obj:      portal,
			wantGets: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gets := 0
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(onPremConsumer, konnectConsumer, konnectAIGateway, portal).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						gets++
						return cl.Get(ctx, key, obj, opts...)
					},
				}).
				Build()
			assert.Equal(t, tc.want, skipsKonnectReconciliation(t.Context(), cl, tc.obj))
			assert.Equal(t, tc.wantGets, gets)
		})
	}
}

// TestHandleParentRef_OnPremEventGatewayParentInAnotherNamespace verifies that
// an entity whose parent, in another namespace, is attached to an
// OnPremEventGateway is skipped before the KongReferenceGrant check: the
// Konnect reconciler doesn't manage it, so it reports no missing grant.
func TestHandleParentRef_OnPremEventGatewayParentInAnotherNamespace(t *testing.T) {
	backend := newTestOnPremEventGatewayBackendCluster("other", "backend", "onprem-gw")
	virtualCluster := newTestEventGatewayVirtualCluster("default", "virtual-cluster", backend.Name)
	virtualCluster.Spec.EventGatewayBackendClusterRef.NamespacedRef.Namespace = new("other")

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithStatusSubresource(virtualCluster).
		WithObjects(backend, virtualCluster).
		Build()

	handler := parentRefHandler[configurationv1alpha1.EventGatewayBackendCluster, *configurationv1alpha1.EventGatewayBackendCluster]{}
	res, err := handler.handleParentRef(t.Context(), cl, virtualCluster)
	require.True(t, res.IsZero())
	require.ErrorAs(t, err, new(ReferencedObjectNotKonnectManagedError))

	updated := &configurationv1alpha1.EventGatewayVirtualCluster{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(virtualCluster), updated))
	require.Empty(t, updated.Status.Conditions, "no Konnect condition must be set")
}

// TestHandleParentRef_RemovesKonnectConditions verifies that the conditions the
// Konnect reconciler set on an entity before it turned out not to be
// Konnect-managed are removed, while a Programmed condition set by another
// controller is kept.
func TestHandleParentRef_RemovesKonnectConditions(t *testing.T) {
	const ns = "default"
	condition := func(conditionType, reason string) metav1.Condition {
		return metav1.Condition{
			Type: conditionType, Status: metav1.ConditionFalse, Reason: reason,
			Message: "set before the on-prem ancestor existed", LastTransitionTime: metav1.Now(),
		}
	}

	for _, tc := range []struct {
		name       string
		programmed metav1.Condition
		wantKept   []string
	}{
		{
			name:       "Programmed set by the Konnect reconciler is removed",
			programmed: condition("Programmed", konnectv1alpha1.KonnectEntityProgrammedReasonConditionWithStatusFalseExists),
		},
		{
			name:       "Programmed set by another controller is kept",
			programmed: condition("Programmed", "Pushed"),
			wantKept:   []string{"Programmed"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := newTestOnPremEventGatewayBackendCluster(ns, "backend", "onprem-gw")
			virtualCluster := newTestEventGatewayVirtualCluster(ns, "virtual-cluster", backend.Name)
			policy := newTestEventGatewayProducePolicy(ns, "policy", virtualCluster.Name)
			policy.Status.Conditions = []metav1.Condition{
				condition(policy.GetStatusConditionTypeParentRefValid(), policy.GetStatusConditionReasonParentRefNotProgrammed()),
				condition(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted),
				condition(configurationv1alpha1.KonnectReferencesResolvedConditionType, configurationv1alpha1.KonnectReferencesResolvedReasonNotProgrammed),
				tc.programmed,
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithStatusSubresource(policy).
				WithObjects(backend, virtualCluster, policy).
				Build()

			handler := parentRefHandler[configurationv1alpha1.EventGatewayVirtualCluster, *configurationv1alpha1.EventGatewayVirtualCluster]{}
			_, err := handler.handleParentRef(t.Context(), cl, policy)
			require.ErrorAs(t, err, new(ReferencedObjectNotKonnectManagedError))

			updated := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(policy), updated))
			var kept []string
			for _, c := range updated.Status.Conditions {
				kept = append(kept, c.Type)
			}
			require.Equal(t, tc.wantKept, kept)
		})
	}
}

// TestReconcile_SkipsEntityNotManagedByKonnect verifies that the Konnect
// reconciler leaves an entity referencing an OnPremEventGateway alone, however
// it was enqueued, e.g. by a reverse watch from an entity referencing it.
func TestReconcile_SkipsEntityNotManagedByKonnect(t *testing.T) {
	trustBundle := &configurationv1alpha1.EventGatewayTLSTrustBundle{
		Name:      "trust-bundle",
		Namespace: "default",
		Spec: configurationv1alpha1.EventGatewayTLSTrustBundleSpec{
			ParentRef: onPremEventGatewayParentRef("onprem-gw"),
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithStatusSubresource(trustBundle).
		WithObjects(trustBundle).
		Build()
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(trustBundle), trustBundle))
	resourceVersion := trustBundle.ResourceVersion

	// No SDK factory: any Konnect call would panic.
	reconciler := NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayTLSTrustBundle](nil, logging.DevelopmentMode, cl)
	res, err := reconciler.Reconcile(t.Context(), trustBundle)
	require.NoError(t, err)
	require.True(t, res.IsZero())

	updated := &configurationv1alpha1.EventGatewayTLSTrustBundle{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(trustBundle), updated))
	require.Equal(t, resourceVersion, updated.ResourceVersion, "the entity must not be modified")
	require.Empty(t, updated.Finalizers)
}
