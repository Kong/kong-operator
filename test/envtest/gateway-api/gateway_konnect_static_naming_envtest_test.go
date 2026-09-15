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

// TestGatewayKonnectControlPlaneStaticNaming asserts that a statically-named Gateway gets a
// KonnectGatewayControlPlane keeping the bare Gateway name in Kubernetes, and a
// namespace-qualified name in Konnect so same-named Gateways do not collide (#4079).
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

	// Unqualified Kubernetes name: the point of the annotation.
	assert.Equal(t, gatewayName, kgcp.Name,
		"the KonnectGatewayControlPlane Kubernetes name must match the Gateway name under static naming")
	// Qualified Konnect name, unique within the org.
	assert.Equal(t, gwNs.Name+"_"+gatewayName, kgcp.Spec.CreateControlPlaneRequest.Name,
		"the Konnect Control Plane name must be qualified with the Gateway namespace")
}

// TestGatewayKonnectControlPlaneStaticNamingBackwardCompatibility asserts that a
// KonnectGatewayControlPlane created before the #4079 fix keeps its old unqualified Konnect
// name on upgrade -- renaming it would rename the Control Plane in Konnect -- and keeps being
// resolved by the resources referring to it.
func TestGatewayKonnectControlPlaneStaticNamingBackwardCompatibility(t *testing.T) {
	t.Parallel()

	const (
		gatewayName = "edge-gw"
		// What the operator wrote before the fix.
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

	// Not Accepted yet: the reconciler ignores such Gateways, leaving a window to seed the
	// pre-existing KonnectGatewayControlPlane before any reconciliation.
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
	// Match what the reconciler looks for: namespace, managed-by label, owner. Create strips
	// TypeMeta from the returned object, so restore it before deriving the owner reference.
	gw.TypeMeta = metav1.TypeMeta{
		APIVersion: gatewayv1.GroupVersion.String(),
		Kind:       "Gateway",
	}
	k8sutils.SetOwnerForObject(existing, gw)
	gatewayutils.LabelObjectAsGatewayManaged(existing, gw.Name)
	require.NoError(t, c.Create(ctx, existing))

	t.Log("accepting the GatewayClass so the Gateway starts reconciling")
	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), envtestconsts.WaitTime, envtestconsts.TickTime)

	// This condition is only set on the "found exactly one" branch, so it proves the reconciler
	// picked up the seeded Control Plane instead of creating a new one.
	t.Log("waiting for the Gateway to report on the existing KonnectGatewayControlPlane")
	require.Eventually(t, func() bool {
		var got gatewayv1.Gateway
		if err := c.Get(ctx, client.ObjectKeyFromObject(gw), &got); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(got.Status.Conditions, string(kcfggateway.KonnectGatewayControlPlaneProgrammedType))
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, envtestconsts.WaitTime, envtestconsts.TickTime, "the Gateway should report the existing KonnectGatewayControlPlane as not programmed")

	t.Log("asserting the pre-existing Konnect name is never rewritten")
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
		"an existing KonnectGatewayControlPlane must not be renamed")

	// References to the Control Plane use the Kubernetes name, which the fix leaves alone.
	// The KonnectExtension is the one the operator creates itself, and it is only reached once
	// the Control Plane is Programmed -- faked here, as envtest has no Konnect backend.
	t.Log("marking the existing KonnectGatewayControlPlane as Programmed")
	require.Eventually(t, func() bool {
		var got konnectv1alpha2.KonnectGatewayControlPlane
		if err := c.Get(ctx, client.ObjectKeyFromObject(existing), &got); err != nil {
			return false
		}
		got.SetConditions([]metav1.Condition{
			{
				Type:               string(gatewayv1.GatewayConditionProgrammed),
				Status:             metav1.ConditionTrue,
				Reason:             string(gatewayv1.GatewayReasonProgrammed),
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: got.Generation,
			},
		})
		return c.Status().Update(ctx, &got) == nil
	}, envtestconsts.WaitTime, envtestconsts.TickTime, "failed marking the KonnectGatewayControlPlane as Programmed")

	t.Log("waiting for the KonnectExtension to reference the Control Plane by its unqualified Kubernetes name")
	require.Eventually(t, func() bool {
		var l konnectv1alpha2.KonnectExtensionList
		if err := c.List(ctx, &l, client.InNamespace(gwNs.Name)); err != nil {
			return false
		}
		if len(l.Items) != 1 {
			return false
		}
		ref := l.Items[0].Spec.Konnect.ControlPlane.Ref
		if ref.KonnectNamespacedRef == nil {
			return false
		}
		// The Kubernetes name, not the Konnect one.
		return ref.KonnectNamespacedRef.Name == gatewayName
	}, envtestconsts.WaitTime, envtestconsts.TickTime,
		"the KonnectExtension must reference the existing Control Plane by its Kubernetes name")
}
