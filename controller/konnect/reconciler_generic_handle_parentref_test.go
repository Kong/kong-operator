package konnect

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestPolicyChainParentNamespace(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"listener", "virtual cluster"} {
		for _, tc := range []struct {
			name      string
			namespace *string
			grant     bool
			stale     bool
		}{
			{name: "omitted"},
			{name: "empty", namespace: new("")},
			{name: "same", namespace: new("default")},
			{name: "different without grant", namespace: new("other")},
			{name: "different with grant", namespace: new("other"), grant: true},
			{name: "different with stale status", namespace: new("other"), grant: true, stale: true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				var chain objectWithParentRef
				var parent objectWithParentRef
				var handleParent func(context.Context, client.Client, objectWithParentRef) (ctrl.Result, error)
				var reconcile func(context.Context, client.Client) (ctrl.Result, error)
				if kind == "listener" {
					c := testListenerPolicyChain("chain")
					chain = c
					parent = &configurationv1alpha1.EventGatewayListener{Name: "listener", Namespace: "default"}
					handleParent = (parentRefHandler[configurationv1alpha1.EventGatewayListener, *configurationv1alpha1.EventGatewayListener]{}).handleParentRef
					reconcile = func(ctx context.Context, cl client.Client) (ctrl.Result, error) {
						return NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayListenerPolicyChain](nil, logging.DevelopmentMode, cl).Reconcile(ctx, c)
					}
				} else {
					c := testPolicyChain("chain")
					chain = c
					parent = &configurationv1alpha1.EventGatewayVirtualCluster{Name: "vc", Namespace: "default"}
					handleParent = (parentRefHandler[configurationv1alpha1.EventGatewayVirtualCluster, *configurationv1alpha1.EventGatewayVirtualCluster]{}).handleParentRef
					reconcile = func(ctx context.Context, cl client.Client) (ctrl.Result, error) {
						return NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](nil, logging.DevelopmentMode, cl).Reconcile(ctx, c)
					}
				}
				crossNamespace := tc.namespace != nil && *tc.namespace == "other"
				chain.GetParentRef().NamespacedRef.Namespace = tc.namespace
				chain.GetObjectKind().SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(chain.GetParentGVK().Kind + "PolicyChain"))
				chain.SetFinalizers([]string{KonnectCleanupFinalizer})
				if crossNamespace {
					parent.SetNamespace("other")
				}
				konnectEntityStatusOf(parent).ID = "parent-id"
				parent.(interface{ SetGatewayID(string) }).SetGatewayID("gateway-id")
				parent.SetConditions([]metav1.Condition{{
					Type: string(konnectv1alpha1.KonnectEntityProgrammedConditionType), Status: metav1.ConditionTrue, Reason: "Programmed",
				}})
				if tc.stale {
					chain.SetParentID("previous-parent-id")
					chain.(objectAcceptingAncestorIDs).SetAncestorID("KonnectEventGateway", "previous-gateway-id")
					chain.SetConditions([]metav1.Condition{
						{Type: chain.GetStatusConditionTypeParentRefValid(), Status: metav1.ConditionTrue, Reason: chain.GetStatusConditionReasonParentRefValid()},
						{Type: configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, Status: metav1.ConditionTrue, Reason: string(configurationv1alpha1.KongReferenceGrantReasonResolvedRefs)},
					})
				}
				objects := []client.Object{chain, parent}
				if tc.grant {
					objects = append(objects, &configurationv1alpha1.KongReferenceGrant{
						Name: "allow-chain", Namespace: parent.GetNamespace(),
						Spec: configurationv1alpha1.KongReferenceGrantSpec{
							From: []configurationv1alpha1.ReferenceGrantFrom{{
								Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
								Kind:  configurationv1alpha1.Kind(chain.GetObjectKind().GroupVersionKind().Kind), Namespace: "default",
							}},
							To: []configurationv1alpha1.ReferenceGrantTo{{
								Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
								Kind:  configurationv1alpha1.Kind(chain.GetParentGVK().Kind),
							}},
						},
					})
				}
				forbidReads := crossNamespace
				cl := newFakeClientBuilder(scheme.Get()).
					WithObjects(objects...).WithStatusSubresource(chain).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if forbidReads {
								t.Errorf("invalid chain must stop before looking up %T %s", obj, key)
							}
							return cl.Get(ctx, key, obj, opts...)
						},
						List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
							if forbidReads {
								t.Errorf("invalid chain must stop before checking grants: %T", list)
							}
							return cl.List(ctx, list, opts...)
						},
					}).Build()
				if crossNamespace {
					res, err := reconcile(t.Context(), cl)
					require.NoError(t, err)
					require.True(t, res.IsZero())
					require.Equal(t, konnectv1alpha2.KonnectEntityStatus{}, *konnectEntityStatusOf(chain))
					require.True(t, meta.IsStatusConditionFalse(chain.GetConditions(), string(konnectv1alpha1.KonnectEntityProgrammedConditionType)))
					require.Contains(t, chain.GetFinalizers(), KonnectCleanupFinalizer)
				} else {
					res, err := handleParent(t.Context(), cl, chain)
					require.NoError(t, err)
					require.True(t, res.IsZero())
					require.Equal(t, "gateway-id", chain.(objectExposingAncestorIDs).GetAncestorIDs()["KonnectEventGateway"])
				}
				condition := meta.FindStatusCondition(chain.GetConditions(), chain.GetStatusConditionTypeParentRefValid())
				require.NotNil(t, condition)
				if crossNamespace {
					require.Equal(t, metav1.ConditionFalse, condition.Status)
					require.Equal(t, chain.GetStatusConditionReasonParentRefInvalid(), condition.Reason)
					require.Contains(t, condition.Message, "same namespace")
				} else {
					require.Equal(t, metav1.ConditionTrue, condition.Status)
					require.Equal(t, chain.GetStatusConditionReasonParentRefValid(), condition.Reason)
				}
				require.Nil(t, meta.FindStatusCondition(chain.GetConditions(), configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs))
				require.Empty(t, parent.GetAnnotations(), "invalid parents must never acquire chain ownership")
				forbidReads = false
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(chain), chain))
				persistedCondition := meta.FindStatusCondition(chain.GetConditions(), chain.GetStatusConditionTypeParentRefValid())
				require.Equal(t, condition, persistedCondition)
				require.Nil(t, meta.FindStatusCondition(chain.GetConditions(), configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs))
				// Deletion must still remove the finalizer without auth or parent lookup.
				require.NoError(t, cl.Delete(t.Context(), chain))
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(chain), chain))
				forbidReads = crossNamespace
				res, err := reconcile(t.Context(), cl)
				require.NoError(t, err)
				require.True(t, res.IsZero())
				forbidReads = false
				require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKeyFromObject(chain), chain)))
			})
		}
	}
}

func TestListenerPolicyCrossNamespaceParentGrant(t *testing.T) {
	t.Parallel()
	policy := &configurationv1alpha1.EventGatewayListenerPolicy{
		Name: "policy", Namespace: "default",
		Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{
			EventGatewayListenerRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "listener", Namespace: new("other"),
				},
			},
		},
	}
	policy.SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("EventGatewayListenerPolicy"))
	parent := &configurationv1alpha1.EventGatewayListener{Name: "listener", Namespace: "other"}
	parent.SetKonnectID("listener-id")
	parent.SetGatewayID("gateway-id")
	parent.SetConditions([]metav1.Condition{{
		Type: string(konnectv1alpha1.KonnectEntityProgrammedConditionType), Status: metav1.ConditionTrue, Reason: "Programmed",
	}})
	grant := &configurationv1alpha1.KongReferenceGrant{
		Name: "allow-policy", Namespace: "other",
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{
				Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:  "EventGatewayListenerPolicy", Namespace: "default",
			}},
			To: []configurationv1alpha1.ReferenceGrantTo{{
				Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group), Kind: "EventGatewayListener",
			}},
		},
	}
	cl := newFakeClientBuilder(scheme.Get()).WithObjects(policy, parent, grant).WithStatusSubresource(policy).Build()
	handler := parentRefHandler[configurationv1alpha1.EventGatewayListener, *configurationv1alpha1.EventGatewayListener]{}
	res, err := handler.handleParentRef(t.Context(), cl, policy)
	require.NoError(t, err)
	require.True(t, res.IsZero())
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(policy), policy))
	require.Equal(t, "listener-id", policy.GetEventGatewayListenerID())
	require.Equal(t, "gateway-id", policy.GetGatewayID())
	require.True(t, meta.IsStatusConditionTrue(policy.GetConditions(), policy.GetStatusConditionTypeParentRefValid()))
	require.True(t, meta.IsStatusConditionTrue(policy.GetConditions(), configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs))
}

// childEntry describes a child entity in parentRefHandler tests.
type childEntry struct {
	name        string
	obj         objectWithParentRef
	getParentID func(client.Object) string
}

// parentRefScenario describes a scenario for parentRefHandler tests.
type parentRefScenario struct {
	name string
	// setupParent is called to mutate the parent before adding it to the fake client.
	// When nil, the parent is not added to the fake client (simulates not-found).
	setupParent      func(client.Object)
	wantCondStatus   metav1.ConditionStatus
	wantCondReason   string
	wantResultIsZero bool
	wantRequeue      bool
	// wantParentID is checked only when non-empty (happy path).
	wantParentID string
}

func gatewayRef(name string) commonv1alpha1.ObjectRef { //nolint:unparam
	return commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{
			Name: name,
		},
	}
}

func portalRef(name string) commonv1alpha1.ObjectRef { //nolint:unparam
	return commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{
			Name: name,
		},
	}
}

func TestHandleParentRef_EventGatewayChildren(t *testing.T) {
	const (
		parentName = "event-gateway"
		parentNS   = "default"
		parentID   = "gateway-konnect-id"
		childNS    = "default"
	)

	newProgrammedGateway := func() *konnectv1alpha1.KonnectEventGateway {
		return &konnectv1alpha1.KonnectEventGateway{
			Name: parentName, Namespace: parentNS,
			Status: konnectv1alpha1.KonnectEventGatewayStatus{
				Conditions: []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.Now(),
				}},
				KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{ID: parentID},
			},
		}
	}

	scenarios := []parentRefScenario{
		{
			name: "parent programmed with Konnect ID",
			setupParent: func(o client.Object) {
				// use programmed gateway as-is
			},
			wantCondStatus:   metav1.ConditionTrue,
			wantCondReason:   configurationv1alpha1.EventGatewayRefReasonValid,
			wantResultIsZero: true,
			wantParentID:     parentID,
		},
		{
			name:             "parent not found",
			setupParent:      nil,
			wantCondStatus:   metav1.ConditionFalse,
			wantCondReason:   configurationv1alpha1.EventGatewayRefReasonInvalid,
			wantResultIsZero: true,
		},
		{
			name: "parent being deleted",
			setupParent: func(o client.Object) {
				now := metav1.NewTime(time.Now())
				o.SetDeletionTimestamp(&now)
				o.SetFinalizers([]string{"test-finalizer"})
			},
			wantCondStatus:   metav1.ConditionFalse,
			wantCondReason:   configurationv1alpha1.EventGatewayRefReasonInvalid,
			wantResultIsZero: true,
		},
		{
			name: "parent not programmed",
			setupParent: func(o client.Object) {
				gw := o.(*konnectv1alpha1.KonnectEventGateway)
				gw.Status.Conditions = []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionFalse,
					Reason:             "Pending",
					LastTransitionTime: metav1.Now(),
				}}
				gw.Status.KonnectEntityStatus = konnectv1alpha1.KonnectEntityStatus{ID: parentID}
			},
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: configurationv1alpha1.EventGatewayRefReasonNotProgrammed,
			wantRequeue:    true,
		},
		{
			name: "parent has no Konnect ID",
			setupParent: func(o client.Object) {
				gw := o.(*konnectv1alpha1.KonnectEventGateway)
				gw.Status.Conditions = []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.Now(),
				}}
				gw.Status.KonnectEntityStatus = konnectv1alpha1.KonnectEntityStatus{} // ID empty
			},
			wantCondStatus:   metav1.ConditionFalse,
			wantCondReason:   configurationv1alpha1.EventGatewayRefReasonInvalid,
			wantResultIsZero: true,
		},
	}

	// The entities directly under a gateway reference it through the
	// deprecated spec.gatewayRef or through spec.parentRef.
	parentRef := &commonv1alpha1.GatewayRef{
		Group: konnectv1alpha1.GroupVersion.Group,
		Kind:  "KonnectEventGateway",
		Name:  parentName,
	}
	getGatewayID := func(o client.Object) string {
		return o.(interface{ GetGatewayID() string }).GetGatewayID()
	}
	children := []childEntry{
		{
			name: "EventGatewayListener",
			obj: &configurationv1alpha1.EventGatewayListener{
				Name: "child", Namespace: childNS,
				Spec: configurationv1alpha1.EventGatewayListenerSpec{GatewayRef: gatewayRef(parentName)},
			},
			getParentID: getGatewayID,
		},
		{
			name: "EventGatewayDataPlaneCertificate",
			obj: &configurationv1alpha1.EventGatewayDataPlaneCertificate{
				Name: "child", Namespace: childNS,
				Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{GatewayRef: gatewayRef(parentName)},
			},
			getParentID: getGatewayID,
		},
		{
			name: "EventGatewayBackendCluster",
			obj: &configurationv1alpha1.EventGatewayBackendCluster{
				Name: "child", Namespace: childNS,
				Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{GatewayRef: gatewayRef(parentName)},
			},
			getParentID: getGatewayID,
		},
		{
			name: "EventGatewayListener with parentRef",
			obj: &configurationv1alpha1.EventGatewayListener{
				Name: "child", Namespace: childNS,
				Spec: configurationv1alpha1.EventGatewayListenerSpec{ParentRef: parentRef},
			},
			getParentID: getGatewayID,
		},
		{
			name: "EventGatewayDataPlaneCertificate with parentRef",
			obj: &configurationv1alpha1.EventGatewayDataPlaneCertificate{
				Name: "child", Namespace: childNS,
				Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{ParentRef: parentRef},
			},
			getParentID: getGatewayID,
		},
		{
			name: "EventGatewayBackendCluster with parentRef",
			obj: &configurationv1alpha1.EventGatewayBackendCluster{
				Name: "child", Namespace: childNS,
				Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{ParentRef: parentRef},
			},
			getParentID: getGatewayID,
		},
		{
			name: "EventGatewaySchemaRegistry with parentRef",
			obj: &configurationv1alpha1.EventGatewaySchemaRegistry{
				Name: "child", Namespace: childNS,
				Spec: configurationv1alpha1.EventGatewaySchemaRegistrySpec{ParentRef: parentRef},
			},
			getParentID: getGatewayID,
		},
		{
			name: "EventGatewayTLSTrustBundle with parentRef",
			obj: &configurationv1alpha1.EventGatewayTLSTrustBundle{
				Name: "child", Namespace: childNS,
				Spec: configurationv1alpha1.EventGatewayTLSTrustBundleSpec{ParentRef: parentRef},
			},
			getParentID: getGatewayID,
		},
		{
			name: "EventGatewayStaticKey with parentRef",
			obj: &configurationv1alpha1.EventGatewayStaticKey{
				Name: "child", Namespace: childNS,
				Spec: configurationv1alpha1.EventGatewayStaticKeySpec{ParentRef: parentRef},
			},
			getParentID: getGatewayID,
		},
	}

	handler := parentRefHandler[konnectv1alpha1.KonnectEventGateway, *konnectv1alpha1.KonnectEventGateway]{}

	for _, sc := range scenarios {
		for _, tc := range children {
			t.Run(sc.name+"/"+tc.name, func(t *testing.T) {
				// Each sub-test needs a fresh child object to avoid cross-test mutation.
				child := tc.obj.DeepCopyObject().(objectWithParentRef)

				builder := newFakeClientBuilder(scheme.Get()).
					WithStatusSubresource(child)

				if sc.setupParent != nil {
					gw := newProgrammedGateway()
					sc.setupParent(gw)
					builder = builder.WithObjects(gw)
				}

				builder = builder.WithObjects(child)
				cl := builder.Build()

				res, err := handler.handleParentRef(t.Context(), cl, child)

				if sc.wantResultIsZero {
					assert.True(t, res.IsZero(), "expected zero result")
				}
				if sc.wantRequeue {
					assert.Greater(t, res.RequeueAfter, time.Duration(0), "expected non-zero requeue")
				}

				// Re-fetch child to read persisted status.
				updated := tc.obj.DeepCopyObject().(client.Object)
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(child), updated))

				condType := child.GetStatusConditionTypeParentRefValid()
				cond, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(condType), updated.(k8sutils.ConditionsAwareObject))
				require.True(t, ok, "condition %q not found", condType)
				assert.Equal(t, sc.wantCondStatus, cond.Status)
				assert.Equal(t, sc.wantCondReason, cond.Reason)

				if sc.wantParentID != "" {
					require.NoError(t, err)
					assert.Equal(t, sc.wantParentID, tc.getParentID(updated))
				}
			})
		}
	}
}

func TestHandleParentRef_EventGatewayVirtualCluster(t *testing.T) {
	const (
		parentName = "backend-cluster"
		parentNS   = "default"
		parentID   = "backend-cluster-konnect-id"
		gatewayID  = "gateway-konnect-id"
		childNS    = "default"
	)

	newProgrammedBackendCluster := func() *configurationv1alpha1.EventGatewayBackendCluster {
		return &configurationv1alpha1.EventGatewayBackendCluster{
			Name: parentName, Namespace: parentNS,
			Status: configurationv1alpha1.EventGatewayBackendClusterStatus{
				Conditions: []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.Now(),
				}},
				KonnectEntityStatus: configurationv1alpha1.KonnectEntityStatus{ID: parentID},
				GatewayID:           &configurationv1alpha1.KonnectEntityRef{ID: gatewayID},
			},
		}
	}

	scenarios := []parentRefScenario{
		{
			name:             "parent programmed with Konnect ID and gateway ancestor ID set",
			setupParent:      func(o client.Object) {},
			wantCondStatus:   metav1.ConditionTrue,
			wantCondReason:   configurationv1alpha1.EventGatewayBackendClusterRefReasonValid,
			wantResultIsZero: true,
			wantParentID:     parentID,
		},
		{
			name:             "parent not found",
			setupParent:      nil,
			wantCondStatus:   metav1.ConditionFalse,
			wantCondReason:   configurationv1alpha1.EventGatewayBackendClusterRefReasonInvalid,
			wantResultIsZero: true,
		},
		{
			name: "parent being deleted",
			setupParent: func(o client.Object) {
				now := metav1.NewTime(time.Now())
				o.SetDeletionTimestamp(&now)
				o.SetFinalizers([]string{"test-finalizer"})
			},
			wantCondStatus:   metav1.ConditionFalse,
			wantCondReason:   configurationv1alpha1.EventGatewayBackendClusterRefReasonInvalid,
			wantResultIsZero: true,
		},
		{
			name: "parent not programmed",
			setupParent: func(o client.Object) {
				bc := o.(*configurationv1alpha1.EventGatewayBackendCluster)
				bc.Status.Conditions = []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionFalse,
					Reason:             "Pending",
					LastTransitionTime: metav1.Now(),
				}}
				bc.Status.KonnectEntityStatus = configurationv1alpha1.KonnectEntityStatus{ID: parentID}
			},
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: configurationv1alpha1.EventGatewayBackendClusterRefReasonNotProgrammed,
			wantRequeue:    true,
		},
		{
			name: "parent has no Konnect ID",
			setupParent: func(o client.Object) {
				bc := o.(*configurationv1alpha1.EventGatewayBackendCluster)
				bc.Status.Conditions = []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.Now(),
				}}
				bc.Status.KonnectEntityStatus = configurationv1alpha1.KonnectEntityStatus{}
				bc.Status.GatewayID = &configurationv1alpha1.KonnectEntityRef{ID: gatewayID}
			},
			wantCondStatus:   metav1.ConditionFalse,
			wantCondReason:   configurationv1alpha1.EventGatewayBackendClusterRefReasonInvalid,
			wantResultIsZero: true,
		},
	}

	child := &configurationv1alpha1.EventGatewayVirtualCluster{
		Name: "vc", Namespace: childNS,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterSpec{
			EventGatewayBackendClusterRef: gatewayRef(parentName),
		},
	}

	handler := parentRefHandler[configurationv1alpha1.EventGatewayBackendCluster, *configurationv1alpha1.EventGatewayBackendCluster]{}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			freshChild := child.DeepCopy()

			builder := newFakeClientBuilder(scheme.Get()).
				WithStatusSubresource(freshChild)

			if sc.setupParent != nil {
				bc := newProgrammedBackendCluster()
				sc.setupParent(bc)
				builder = builder.WithObjects(bc)
			}

			builder = builder.WithObjects(freshChild)
			cl := builder.Build()

			res, err := handler.handleParentRef(t.Context(), cl, freshChild)

			if sc.wantResultIsZero {
				assert.True(t, res.IsZero(), "expected zero result")
			}
			if sc.wantRequeue {
				assert.Greater(t, res.RequeueAfter, time.Duration(0), "expected non-zero requeue")
			}

			updated := &configurationv1alpha1.EventGatewayVirtualCluster{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(freshChild), updated))

			condType := freshChild.GetStatusConditionTypeParentRefValid()
			cond, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(condType), updated)
			require.True(t, ok, "condition %q not found", condType)
			assert.Equal(t, sc.wantCondStatus, cond.Status)
			assert.Equal(t, sc.wantCondReason, cond.Reason)

			if sc.wantParentID != "" {
				require.NoError(t, err)
				assert.Equal(t, sc.wantParentID, updated.GetEventGatewayBackendClusterID())
				assert.Equal(t, gatewayID, updated.GetGatewayID(), "ancestor gateway ID must be propagated")
			}
		})
	}
}

func TestHandleParentRef_PortalChildren(t *testing.T) {
	const (
		parentName = "portal"
		parentNS   = "default"
		parentID   = "portal-konnect-id"
		childNS    = "default"
	)

	newProgrammedPortal := func() *konnectv1alpha1.Portal {
		return &konnectv1alpha1.Portal{
			Name: parentName, Namespace: parentNS,
			Status: konnectv1alpha1.PortalStatus{
				Conditions: []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.Now(),
				}},
				KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{ID: parentID},
			},
		}
	}

	scenarios := []parentRefScenario{
		{
			name: "parent programmed with Konnect ID",
			setupParent: func(o client.Object) {
				// use programmed portal as-is
			},
			wantCondStatus:   metav1.ConditionTrue,
			wantCondReason:   konnectv1alpha1.PortalRefReasonValid,
			wantResultIsZero: true,
			wantParentID:     parentID,
		},
		{
			name:             "parent not found",
			setupParent:      nil,
			wantCondStatus:   metav1.ConditionFalse,
			wantCondReason:   konnectv1alpha1.PortalRefReasonInvalid,
			wantResultIsZero: true,
		},
		{
			name: "parent being deleted",
			setupParent: func(o client.Object) {
				now := metav1.NewTime(time.Now())
				o.SetDeletionTimestamp(&now)
				o.SetFinalizers([]string{"test-finalizer"})
			},
			wantCondStatus:   metav1.ConditionFalse,
			wantCondReason:   konnectv1alpha1.PortalRefReasonInvalid,
			wantResultIsZero: true,
		},
		{
			name: "parent not programmed",
			setupParent: func(o client.Object) {
				p := o.(*konnectv1alpha1.Portal)
				p.Status.Conditions = []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionFalse,
					Reason:             "Pending",
					LastTransitionTime: metav1.Now(),
				}}
				p.Status.KonnectEntityStatus = konnectv1alpha1.KonnectEntityStatus{ID: parentID}
			},
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: konnectv1alpha1.PortalRefReasonNotProgrammed,
			wantRequeue:    true,
		},
		{
			name: "parent has no Konnect ID",
			setupParent: func(o client.Object) {
				p := o.(*konnectv1alpha1.Portal)
				p.Status.Conditions = []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.Now(),
				}}
				p.Status.KonnectEntityStatus = konnectv1alpha1.KonnectEntityStatus{} // ID empty
			},
			wantCondStatus:   metav1.ConditionFalse,
			wantCondReason:   konnectv1alpha1.PortalRefReasonInvalid,
			wantResultIsZero: true,
		},
	}

	children := []childEntry{
		{
			name: "PortalTeam",
			obj: &konnectv1alpha1.PortalTeam{
				Name: "child", Namespace: childNS,
				Spec: konnectv1alpha1.PortalTeamSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalTeam).GetPortalID()
			},
		},
		{
			name: "PortalPage",
			obj: &konnectv1alpha1.PortalPage{
				Name: "child", Namespace: childNS,
				Spec: konnectv1alpha1.PortalPageSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalPage).GetPortalID()
			},
		},
		{
			name: "PortalEmailConfig",
			obj: &konnectv1alpha1.PortalEmailConfig{
				Name: "child", Namespace: childNS,
				Spec: konnectv1alpha1.PortalEmailConfigSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalEmailConfig).GetPortalID()
			},
		},
		{
			name: "PortalCustomDomain",
			obj: &konnectv1alpha1.PortalCustomDomain{
				Name: "child", Namespace: childNS,
				Spec: konnectv1alpha1.PortalCustomDomainSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalCustomDomain).GetPortalID()
			},
		},
		{
			name: "PortalIPAllowList",
			obj: &konnectv1alpha1.PortalIPAllowList{
				Name: "child", Namespace: childNS,
				Spec: konnectv1alpha1.PortalIPAllowListSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalIPAllowList).GetPortalID()
			},
		},
		{
			name: "IdentityProviderRequest",
			obj: &konnectv1alpha1.PortalIdentityProviderRequest{
				Name: "child", Namespace: childNS,
				Spec: konnectv1alpha1.PortalIdentityProviderRequestSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalIdentityProviderRequest).GetPortalID()
			},
		},
	}

	handler := parentRefHandler[konnectv1alpha1.Portal, *konnectv1alpha1.Portal]{}

	for _, sc := range scenarios {
		for _, tc := range children {
			t.Run(sc.name+"/"+tc.name, func(t *testing.T) {
				child := tc.obj.DeepCopyObject().(objectWithParentRef)

				builder := newFakeClientBuilder(scheme.Get()).
					WithStatusSubresource(child)

				if sc.setupParent != nil {
					portal := newProgrammedPortal()
					sc.setupParent(portal)
					builder = builder.WithObjects(portal)
				}

				builder = builder.WithObjects(child)
				cl := builder.Build()

				res, err := handler.handleParentRef(t.Context(), cl, child)

				if sc.wantResultIsZero {
					assert.True(t, res.IsZero(), "expected zero result")
				}
				if sc.wantRequeue {
					assert.Greater(t, res.RequeueAfter, time.Duration(0), "expected non-zero requeue")
				}

				updated := tc.obj.DeepCopyObject().(client.Object)
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(child), updated))

				condType := child.GetStatusConditionTypeParentRefValid()
				cond, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(condType), updated.(k8sutils.ConditionsAwareObject))
				require.True(t, ok, "condition %q not found", condType)
				assert.Equal(t, sc.wantCondStatus, cond.Status)
				assert.Equal(t, sc.wantCondReason, cond.Reason)

				if sc.wantParentID != "" {
					require.NoError(t, err)
					assert.Equal(t, sc.wantParentID, tc.getParentID(updated))
				}
			})
		}
	}
}

// TestHandleParentRef_NotKonnectManagedParent covers the skip for parents outside the
// Konnect reconciler's scope: an AIGatewayConsumer targeting an OnPremAIGateway never
// gets a Konnect ID, so a credential referencing it must stop reconciliation without
// erroring (and without keeping a stale parent-ref condition), instead of failing with
// "does not have a Konnect ID yet" forever.
func TestHandleParentRef_NotKonnectManagedParent(t *testing.T) {
	const parentName = "ai-gw-consumer"

	onPremConsumer := func() *aiconfigurationv1alpha1.AIGatewayConsumer {
		return &aiconfigurationv1alpha1.AIGatewayConsumer{
			Name: parentName, Namespace: "default",
			Spec: aiconfigurationv1alpha1.AIGatewayConsumerSpec{
				AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
					Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
					Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "onprem-gw"},
				},
			},
		}
	}

	newCredential := func(mutators ...func(*aiconfigurationv1alpha1.AIGatewayConsumerCredential)) *aiconfigurationv1alpha1.AIGatewayConsumerCredential {
		cred := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
			Name: "cred", Namespace: "default",
			Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
				AIGatewayConsumerRef: gatewayRef(parentName),
			},
		}
		for _, m := range mutators {
			m(cred)
		}
		return cred
	}

	handler := parentRefHandler[aiconfigurationv1alpha1.AIGatewayConsumer, *aiconfigurationv1alpha1.AIGatewayConsumer]{}

	t.Run("on-prem parent skips reconciliation, clears stale condition and finalizer", func(t *testing.T) {
		cred := newCredential(func(c *aiconfigurationv1alpha1.AIGatewayConsumerCredential) {
			c.Finalizers = []string{KonnectCleanupFinalizer}
			c.Status.Conditions = []metav1.Condition{{
				Type:               c.GetStatusConditionTypeParentRefValid(),
				Status:             metav1.ConditionFalse,
				Reason:             "ParentRefInvalid",
				Message:            "stale condition from before the parent became on-prem",
				LastTransitionTime: metav1.Now(),
			}}
		})
		cl := newFakeClientBuilder(scheme.Get()).
			WithStatusSubresource(cred).
			WithObjects(onPremConsumer(), cred).
			Build()

		res, err := handler.handleParentRef(t.Context(), cl, cred)
		require.Zero(t, res.RequeueAfter)
		require.True(t, res.IsZero())

		var notManagedErr ReferencedObjectNotKonnectManagedError
		require.ErrorAs(t, err, &notManagedErr)
		require.Equal(t, parentName, notManagedErr.Reference.Name)

		// Mirror the real reconcile flow: handleRefResult consumes the sentinel and
		// removes the cleanup finalizer.
		stop, res, err := handleRefResult(t.Context(), cl, cred, res, err)
		require.True(t, stop)
		require.True(t, res.IsZero())
		require.NoError(t, err)

		updated := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cred), updated))
		_, ok := k8sutils.GetCondition(
			kcfgconsts.ConditionType(updated.GetStatusConditionTypeParentRefValid()),
			updated,
		)
		require.False(t, ok, "stale parent-ref condition must be cleared")
		require.Empty(t, updated.Finalizers, "cleanup finalizer must be removed")
	})

	t.Run("konnect parent without Konnect ID still reports invalid", func(t *testing.T) {
		// Programmed-but-ID-less consumer: pins that the skip branch sits before the
		// Konnect-ID check only for on-prem parents, not for Konnect ones.
		consumer := &aiconfigurationv1alpha1.AIGatewayConsumer{
			Name: parentName, Namespace: "default",
			Status: aiconfigurationv1alpha1.AIGatewayConsumerStatus{
				Conditions: []metav1.Condition{{
					Type:               string(konnectv1alpha1.KonnectEntityProgrammedConditionType),
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					LastTransitionTime: metav1.Now(),
				}},
			},
		}
		cred := newCredential()
		cl := newFakeClientBuilder(scheme.Get()).
			WithStatusSubresource(cred).
			WithObjects(consumer, cred).
			Build()

		_, err := handler.handleParentRef(t.Context(), cl, cred)
		require.ErrorAs(t, err, new(ReferencedObjectIsInvalidError))

		updated := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cred), updated))
		cond, ok := k8sutils.GetCondition(
			kcfgconsts.ConditionType(updated.GetStatusConditionTypeParentRefValid()),
			updated,
		)
		require.True(t, ok)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, cred.GetStatusConditionReasonParentRefInvalid(), cond.Reason)
	})
}

func TestEnsureKongReferenceGrantForParentRef_NotGranted(t *testing.T) {
	newListener := func() *configurationv1alpha1.EventGatewayListener {
		listener := &configurationv1alpha1.EventGatewayListener{
			Name: "listener", Namespace: "listeners",
			Finalizers: []string{KonnectCleanupFinalizer},
		}
		listener.GetObjectKind().SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("EventGatewayListener"))
		return listener
	}
	ref := commonv1alpha1.ObjectRef{
		Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gateway", Namespace: new("gateways")},
	}
	parentGVK := konnectv1alpha1.GroupVersion.WithKind("KonnectEventGateway")

	t.Run("an entity not being deleted waits for a grant, and reports it", func(t *testing.T) {
		listener := newListener()
		cl := newFakeClientBuilder(scheme.Get()).WithObjects(listener).WithStatusSubresource(listener).Build()
		res, err := ensureKongReferenceGrantForParentRef(t.Context(), cl, listener, ref, parentGVK)
		require.NoError(t, err)
		require.False(t, res.IsZero())
		require.True(t, meta.IsStatusConditionFalse(listener.Status.Conditions, string(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs)))
	})

	t.Run("an entity being deleted in Konnect isn't held back by a revoked grant", func(t *testing.T) {
		listener := newListener()
		listener.DeletionTimestamp = new(metav1.Now())
		listener.SetKonnectID("listener-id")
		cl := newFakeClientBuilder(scheme.Get()).WithObjects(listener).WithStatusSubresource(listener).Build()
		res, err := ensureKongReferenceGrantForParentRef(t.Context(), cl, listener, ref, parentGVK)
		require.NoError(t, err)
		require.True(t, res.IsZero())
	})

	t.Run("an entity being deleted not in Konnect stops, without reading its parent, and drops its finalizer", func(t *testing.T) {
		listener := newListener()
		listener.DeletionTimestamp = new(metav1.Now())
		cl := newFakeClientBuilder(scheme.Get()).WithObjects(listener).WithStatusSubresource(listener).Build()
		res, err := ensureKongReferenceGrantForParentRef(t.Context(), cl, listener, ref, parentGVK)
		_, ok := errors.AsType[ReferencedObjectNotGrantedOnDeletionError](err)
		require.True(t, ok, "expected ReferencedObjectNotGrantedOnDeletionError, got %v", err)

		stop, res, err := handleRefResult(t.Context(), cl, listener, res, err)
		require.NoError(t, err)
		require.True(t, stop)
		require.True(t, res.IsZero())
		require.Empty(t, listener.Finalizers)
		require.Empty(t, listener.GetGatewayID())
	})
}

func TestHandleParentRef_EntityBeingDeletedWithParentNotProgrammed(t *testing.T) {
	for _, tc := range []struct {
		name         string
		parentID     string
		wantDeferred bool
	}{
		{name: "the entity goes on, with the Konnect ID of its parent", parentID: "listener-id"},
		{name: "without a Konnect ID on its parent, it waits", wantDeferred: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := &configurationv1alpha1.EventGatewayListener{Name: "listener", Namespace: "listeners"}
			parent.SetKonnectID(tc.parentID)
			parent.SetGatewayID("gateway-id")
			parent.SetConditions([]metav1.Condition{{Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: metav1.ConditionFalse}})
			policy := &configurationv1alpha1.EventGatewayListenerPolicy{
				Name: "policy", Namespace: "listeners",
				Finalizers:        []string{KonnectCleanupFinalizer},
				DeletionTimestamp: new(metav1.Now()),
				Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{
					EventGatewayListenerRef: commonv1alpha1.ObjectRef{
						Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "listener"},
					},
				},
			}
			policy.GetObjectKind().SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("EventGatewayListenerPolicy"))
			cl := newFakeClientBuilder(scheme.Get()).WithObjects(parent, policy).WithStatusSubresource(parent, policy).Build()

			handler := parentRefHandler[configurationv1alpha1.EventGatewayListener, *configurationv1alpha1.EventGatewayListener]{}
			res, err := handler.handleParentRef(t.Context(), cl, policy)
			if tc.wantDeferred {
				require.NoError(t, err)
				require.False(t, res.IsZero())
				return
			}
			require.NoError(t, err)
			require.True(t, res.IsZero())
			require.Equal(t, "listener-id", policy.GetEventGatewayListenerID())
		})
	}
}
