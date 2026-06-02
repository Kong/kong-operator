package konnect

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

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
			ObjectMeta: metav1.ObjectMeta{Name: parentName, Namespace: parentNS},
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

	children := []childEntry{
		{
			name: "EventGatewayListener",
			obj: &configurationv1alpha1.EventGatewayListener{
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: childNS},
				Spec:       configurationv1alpha1.EventGatewayListenerSpec{GatewayRef: gatewayRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*configurationv1alpha1.EventGatewayListener).GetGatewayID()
			},
		},
		{
			name: "EventGatewayDataPlaneCertificate",
			obj: &configurationv1alpha1.EventGatewayDataPlaneCertificate{
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: childNS},
				Spec:       configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{GatewayRef: gatewayRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*configurationv1alpha1.EventGatewayDataPlaneCertificate).GetGatewayID()
			},
		},
		{
			name: "EventGatewayBackendCluster",
			obj: &configurationv1alpha1.EventGatewayBackendCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: childNS},
				Spec:       configurationv1alpha1.EventGatewayBackendClusterSpec{GatewayRef: gatewayRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*configurationv1alpha1.EventGatewayBackendCluster).GetGatewayID()
			},
		},
	}

	handler := parentRefHandler[konnectv1alpha1.KonnectEventGateway, *konnectv1alpha1.KonnectEventGateway]{}

	for _, sc := range scenarios {
		for _, tc := range children {
			t.Run(sc.name+"/"+tc.name, func(t *testing.T) {
				// Each sub-test needs a fresh child object to avoid cross-test mutation.
				child := tc.obj.DeepCopyObject().(objectWithParentRef)

				builder := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
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
			ObjectMeta: metav1.ObjectMeta{Name: parentName, Namespace: parentNS},
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
		ObjectMeta: metav1.ObjectMeta{Name: "vc", Namespace: childNS},
		Spec: configurationv1alpha1.EventGatewayVirtualClusterSpec{
			EventGatewayBackendClusterRef: gatewayRef(parentName),
		},
	}

	handler := parentRefHandler[configurationv1alpha1.EventGatewayBackendCluster, *configurationv1alpha1.EventGatewayBackendCluster]{}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			freshChild := child.DeepCopy()

			builder := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
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
			ObjectMeta: metav1.ObjectMeta{Name: parentName, Namespace: parentNS},
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
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: childNS},
				Spec:       konnectv1alpha1.PortalTeamSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalTeam).GetPortalID()
			},
		},
		{
			name: "PortalPage",
			obj: &konnectv1alpha1.PortalPage{
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: childNS},
				Spec:       konnectv1alpha1.PortalPageSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalPage).GetPortalID()
			},
		},
		{
			name: "PortalEmailConfig",
			obj: &konnectv1alpha1.PortalEmailConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: childNS},
				Spec:       konnectv1alpha1.PortalEmailConfigSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalEmailConfig).GetPortalID()
			},
		},
		{
			name: "PortalCustomDomain",
			obj: &konnectv1alpha1.PortalCustomDomain{
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: childNS},
				Spec:       konnectv1alpha1.PortalCustomDomainSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalCustomDomain).GetPortalID()
			},
		},
		{
			name: "PortalIPAllowList",
			obj: &konnectv1alpha1.PortalIPAllowList{
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: childNS},
				Spec:       konnectv1alpha1.PortalIPAllowListSpec{PortalRef: portalRef(parentName)},
			},
			getParentID: func(o client.Object) string {
				return o.(*konnectv1alpha1.PortalIPAllowList).GetPortalID()
			},
		},
		{
			name: "IdentityProviderRequest",
			obj: &konnectv1alpha1.PortalIdentityProviderRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: childNS},
				Spec:       konnectv1alpha1.PortalIdentityProviderRequestSpec{PortalRef: portalRef(parentName)},
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

				builder := fake.NewClientBuilder().
					WithScheme(scheme.Get()).
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
