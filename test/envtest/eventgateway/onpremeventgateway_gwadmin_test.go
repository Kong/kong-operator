package eventgateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	egonprem "github.com/kong/kong-operator/v2/controller/eventgateway/onprem"
	"github.com/kong/kong-operator/v2/internal/gwadmin/published"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref/gwadminreftest"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestOnPremEventGatewayGWAdmin checks that an OnPremEventGateway reports
// what its GWAdmin is missing until it has gw-admin pods, that a change of
// the GWAdmin, of its pods or of a KongReferenceGrant updates the gateways
// that reference it, and that a GWAdmin in another namespace is used only
// with a KongReferenceGrant. The GWAdmin controller doesn't run: the test
// publishes the API of the GWAdmin, and the EndpointSlice of its API Service,
// itself.
func TestOnPremEventGatewayGWAdmin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	envtest.StartReconcilers(ctx, t, mgr, logs, &egonprem.Reconciler{Client: mgr.GetClient()})
	cl := mgr.GetClient()

	const (
		gwAdminName    = "gw-admin"
		apiServiceName = "gw-admin-api"
	)
	// reports checks the GWAdminResolved condition of the gateway, and that
	// it is not Programmed: Pending once its GWAdmin is resolved, waiting for
	// it otherwise.
	reports := func(t *testing.T, gateway *eventgatewayv1alpha1.OnPremEventGateway, reason kcfgconsts.ConditionReason, message string) {
		t.Helper()
		resolved := reason == eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedReason
		wantStatus, wantProgrammedReason := metav1.ConditionFalse, eventgatewayv1alpha1.DependenciesNotReadyReason
		if resolved {
			wantStatus, wantProgrammedReason = metav1.ConditionTrue, eventgatewayv1alpha1.OnPremEventGatewayPendingReason
		}
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var got eventgatewayv1alpha1.OnPremEventGateway
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gateway), &got)) {
				return
			}
			cond := apimeta.FindStatusCondition(got.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedType))
			if assert.NotNil(ct, cond) {
				assert.Equal(ct, wantStatus, cond.Status)
				assert.Equal(ct, string(reason), cond.Reason)
				assert.Equal(ct, message, cond.Message)
			}
			programmed := apimeta.FindStatusCondition(got.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
			if assert.NotNil(ct, programmed) {
				assert.Equal(ct, metav1.ConditionFalse, programmed.Status)
				assert.Equal(ct, string(wantProgrammedReason), programmed.Reason)
			}
		}, waitTime, tickTime)
	}
	notFound := eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotFoundReason
	notPermitted := eventgatewayv1alpha1.OnPremEventGatewayGWAdminRefNotPermittedReason
	notReady := eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotReadyReason
	resolved := eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedReason
	servedBy := func(gwAdminNamespace string) string {
		return "The GWAdmin " + gwAdminNamespace + "/gw-admin serves the gateway"
	}

	gateway := &eventgatewayv1alpha1.OnPremEventGateway{
		Name: "gateway", Namespace: ns.Name,
		Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{GWAdminRef: eventgatewayv1alpha1.GWAdminRef{Name: gwAdminName}},
	}
	require.NoError(t, cl.Create(ctx, gateway))
	reports(t, gateway, notFound, "The GWAdmin "+ns.Name+"/gw-admin does not exist")

	gwAdmin := &operatorv1alpha1.GWAdmin{Name: gwAdminName, Namespace: ns.Name}
	require.NoError(t, cl.Create(ctx, gwAdmin))
	reports(t, gateway, notReady, "The GWAdmin "+ns.Name+"/gw-admin hasn't published its API yet")

	gwAdmin.Status.API = &operatorv1alpha1.GWAdminAPIStatus{ServiceName: apiServiceName, PortName: "api", Port: 8443}
	require.NoError(t, cl.Status().Update(ctx, gwAdmin))
	reports(t, gateway, notReady, "The GWAdmin "+ns.Name+"/gw-admin has no gw-admin pod")

	// Labelled like the EndpointSlice controller labels the EndpointSlices of
	// the API Service.
	labels := published.APIServiceLabels(gwAdminName)
	labels[discoveryv1.LabelServiceName] = apiServiceName
	labels[discoveryv1.LabelManagedBy] = "endpointslice-controller.k8s.io"
	slice := &discoveryv1.EndpointSlice{
		Name: apiServiceName + "-abcde", Namespace: ns.Name,
		Labels:      labels,
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Name: new("api"), Port: new(int32(8443))}},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses:  []string{"10.0.0.1"},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "gw-admin-0"},
			Conditions: discoveryv1.EndpointConditions{Terminating: new(false)},
		}},
	}
	require.NoError(t, cl.Create(ctx, slice))
	reports(t, gateway, resolved, servedBy(ns.Name))

	t.Run("a gateway of another namespace needs a KongReferenceGrant", func(t *testing.T) {
		other := envtest.CreateNamespace(ctx, t, cl)
		crossNamespace := &eventgatewayv1alpha1.OnPremEventGateway{
			Name: "gateway", Namespace: other.Name,
			Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{GWAdminRef: eventgatewayv1alpha1.GWAdminRef{Name: gwAdminName, Namespace: new(ns.Name)}},
		}
		require.NoError(t, cl.Create(ctx, crossNamespace))
		reports(t, crossNamespace, notPermitted, "The gateway may not use its GWAdmin: no KongReferenceGrant in namespace "+ns.Name+
			" allows the OnPremEventGateways of namespace "+other.Name+" to reference the GWAdmin "+ns.Name+"/gw-admin")

		grant := gwadminreftest.Grant(ns.Name, "gateways-of-"+other.Name, other.Name, "")
		require.NoError(t, cl.Create(ctx, grant))
		reports(t, crossNamespace, resolved, servedBy(ns.Name))

		require.NoError(t, cl.Delete(ctx, grant))
		reports(t, crossNamespace, notPermitted, "The gateway may not use its GWAdmin: no KongReferenceGrant in namespace "+ns.Name+
			" allows the OnPremEventGateways of namespace "+other.Name+" to reference the GWAdmin "+ns.Name+"/gw-admin")
	})

	t.Run("a pod that goes away is noticed", func(t *testing.T) {
		slice.Endpoints[0].Conditions.Terminating = new(true)
		require.NoError(t, cl.Update(ctx, slice))
		reports(t, gateway, notReady, "The GWAdmin "+ns.Name+"/gw-admin has no gw-admin pod")
	})

	t.Run("a GWAdmin that goes away is noticed", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, gwAdmin))
		reports(t, gateway, notFound, "The GWAdmin "+ns.Name+"/gw-admin does not exist")
	})
}
