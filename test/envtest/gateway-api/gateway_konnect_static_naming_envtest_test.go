package gatewayapi

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kcfggateway "github.com/kong/kong-operator/v2/api/gateway-operator/gateway"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	kogateway "github.com/kong/kong-operator/v2/controller/gateway"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	gatewayutils "github.com/kong/kong-operator/v2/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/pkg/vars"
	"github.com/kong/kong-operator/v2/test/envtest"
	envtestconsts "github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
)

// TestGatewayKonnectControlPlaneStaticNaming covers the naming of the KonnectGatewayControlPlane
// created for a Gateway that opts into static naming via the
// gateway-operator.konghq.com/static-naming annotation.
//
// The Kubernetes name and the Konnect name have different uniqueness scopes:
// (namespace, kind, name) versus (org_id, name). Under static naming the Kubernetes name must
// stay the unqualified Gateway name, while the Konnect name must be qualified with the namespace
// so that same-named Gateways in different namespaces do not collide in Konnect with an HTTP 409
// (#4079).
func TestGatewayKonnectControlPlaneStaticNaming(t *testing.T) {
	t.Parallel()

	const gatewayName = "edge-gw"

	scheme := managerscheme.Get()
	ctx := t.Context()

	cfg, gwNs := envtest.Setup(t, ctx, scheme, envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme)

	r := &kogateway.Reconciler{
		Client:                mgr.GetClient(),
		Scheme:                scheme,
		Namespace:             gwNs.Name,
		DefaultDataPlaneImage: "kong:latest",
	}
	envtest.StartReconcilers(ctx, t, mgr, logs, r)

	c := mgr.GetClient()

	gwConfig := deploy.GatewayConfiguration(t, ctx, c,
		func(obj client.Object) { obj.SetName("static-naming-gwconfig"); obj.SetNamespace(gwNs.Name) },
		deploy.WithGatewayConfigKonnectAuthRef("my-auth", gwNs.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gwConfig) })

	gc := deploy.GatewayClass(t, ctx, c,
		func(obj client.Object) { obj.SetName("static-naming-gc") },
		deploy.WithGatewayClassControllerName(vars.ControllerName()),
		deploy.WithGatewayClassParametersRef("gateway-operator.konghq.com", "GatewayConfiguration", gwConfig.Name, gwNs.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gc) })

	t.Log("patching GatewayClass status to Accepted=True")
	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), envtestconsts.WaitTime, envtestconsts.TickTime)

	gw := deploy.Gateway(t, ctx, c,
		func(obj client.Object) {
			obj.SetName(gatewayName)
			obj.SetNamespace(gwNs.Name)
			obj.SetAnnotations(map[string]string{consts.GatewayStaticNamingAnnotation: "true"})
		},
		deploy.WithGatewayClassName(gc.Name),
		deploy.WithGatewayListeners(gatewayv1.Listener{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80}),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gw) })

	t.Log("waiting for the KonnectGatewayControlPlane to be created")
	var kgcp konnectv1alpha2.KonnectGatewayControlPlane
	require.Eventually(t, func() bool {
		var l konnectv1alpha2.KonnectGatewayControlPlaneList
		if err := c.List(ctx, &l, client.InNamespace(gwNs.Name)); err != nil {
			t.Logf("error listing KonnectGatewayControlPlanes: %v", err)
			return false
		}
		if len(l.Items) != 1 {
			return false
		}
		kgcp = l.Items[0]
		return kgcp.Spec.CreateControlPlaneRequest != nil
	}, envtestconsts.WaitTime, envtestconsts.TickTime, "exactly one KonnectGatewayControlPlane should be created")

	// The Kubernetes name stays unqualified: that is the point of the annotation.
	assert.Equal(t, gatewayName, kgcp.Name,
		"the KonnectGatewayControlPlane Kubernetes name must match the Gateway name under static naming")
	// The Konnect name is qualified with the namespace to keep it unique within the organization.
	assert.Equal(t, gwNs.Name+"-"+gatewayName, kgcp.Spec.CreateControlPlaneRequest.Name,
		"the Konnect Control Plane name must be qualified with the Gateway namespace")
}

// TestGatewayKonnectControlPlaneStaticNamingBackwardCompatibility asserts that a
// KonnectGatewayControlPlane created before the #4079 fix -- whose
// spec.createControlPlaneRequest.name is the old unqualified Gateway name -- is left alone on
// upgrade. Renaming it would rename the Control Plane in Konnect, and creating a second one
// would provision a duplicate Control Plane.
func TestGatewayKonnectControlPlaneStaticNamingBackwardCompatibility(t *testing.T) {
	t.Parallel()

	const (
		gatewayName = "edge-gw"
		// legacyKonnectName is what the operator wrote before the fix: the unqualified name.
		legacyKonnectName = gatewayName
	)

	scheme := managerscheme.Get()
	ctx := t.Context()

	cfg, gwNs := envtest.Setup(t, ctx, scheme, envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme)

	r := &kogateway.Reconciler{
		Client:                mgr.GetClient(),
		Scheme:                scheme,
		Namespace:             gwNs.Name,
		DefaultDataPlaneImage: "kong:latest",
	}
	envtest.StartReconcilers(ctx, t, mgr, logs, r)

	c := mgr.GetClient()

	gwConfig := deploy.GatewayConfiguration(t, ctx, c,
		func(obj client.Object) { obj.SetName("static-naming-bc-gwconfig"); obj.SetNamespace(gwNs.Name) },
		deploy.WithGatewayConfigKonnectAuthRef("my-auth", gwNs.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gwConfig) })

	// Create the GatewayClass without accepting it yet: the reconciler ignores Gateways whose
	// GatewayClass is not Accepted, which gives us a window to seed the pre-existing
	// KonnectGatewayControlPlane before any reconciliation happens.
	gc := deploy.GatewayClass(t, ctx, c,
		func(obj client.Object) { obj.SetName("static-naming-bc-gc") },
		deploy.WithGatewayClassControllerName(vars.ControllerName()),
		deploy.WithGatewayClassParametersRef("gateway-operator.konghq.com", "GatewayConfiguration", gwConfig.Name, gwNs.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gc) })

	gw := deploy.Gateway(t, ctx, c,
		func(obj client.Object) {
			obj.SetName(gatewayName)
			obj.SetNamespace(gwNs.Name)
			obj.SetAnnotations(map[string]string{consts.GatewayStaticNamingAnnotation: "true"})
		},
		deploy.WithGatewayClassName(gc.Name),
		deploy.WithGatewayListeners(gatewayv1.Listener{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80}),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gw) })

	t.Log("seeding a pre-fix KonnectGatewayControlPlane with the old unqualified Konnect name")
	existing := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gwNs.Name,
			Name:      gatewayName,
		},
	}
	existing.Spec.CreateControlPlaneRequest = &sdkkonnectcomp.CreateControlPlaneRequest{
		Name: legacyKonnectName,
	}
	existing.Spec.KonnectConfiguration.APIAuthConfigurationRef = konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
		Name:      "my-auth",
		Namespace: new(gwNs.Name),
	}
	// Match what the reconciler looks for: same namespace, managed-by label, owned by the Gateway.
	// The client strips TypeMeta from the object returned by Create, so restore it before
	// deriving the owner reference from it.
	gw.TypeMeta = metav1.TypeMeta{
		APIVersion: gatewayv1.GroupVersion.String(),
		Kind:       "Gateway",
	}
	k8sutils.SetOwnerForObject(existing, gw)
	gatewayutils.LabelObjectAsGatewayManaged(existing, gw.Name)
	require.NoError(t, c.Create(ctx, existing))

	t.Log("accepting the GatewayClass so the Gateway starts reconciling")
	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), envtestconsts.WaitTime, envtestconsts.TickTime)

	// The seeded Control Plane never becomes Programmed (there is no Konnect backend in envtest),
	// so this condition proves the reconciler took the "one existing Control Plane" branch and
	// called enforceKonnectGatewayControlPlaneSpec rather than creating a new one.
	t.Log("waiting for the Gateway to report on the existing KonnectGatewayControlPlane")
	require.Eventually(t, func() bool {
		var got gatewayv1.Gateway
		if err := c.Get(ctx, client.ObjectKeyFromObject(gw), &got); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(got.Status.Conditions, string(kcfggateway.KonnectGatewayControlPlaneProgrammedType))
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, envtestconsts.WaitTime, envtestconsts.TickTime, "the Gateway should report the existing KonnectGatewayControlPlane as not programmed")

	t.Log("asserting the pre-existing Konnect name is never rewritten and no duplicate is created")
	require.Never(t, func() bool {
		var l konnectv1alpha2.KonnectGatewayControlPlaneList
		if err := c.List(ctx, &l, client.InNamespace(gwNs.Name)); err != nil {
			return false
		}
		if len(l.Items) != 1 {
			t.Logf("expected 1 KonnectGatewayControlPlane, found %d", len(l.Items))
			return true
		}
		req := l.Items[0].Spec.CreateControlPlaneRequest
		if req == nil || req.Name != legacyKonnectName {
			t.Logf("spec.createControlPlaneRequest.name changed: %v", req)
			return true
		}
		return false
	}, envtestconsts.WaitTime, envtestconsts.TickTime,
		"an existing KonnectGatewayControlPlane must be neither renamed nor duplicated")
}
