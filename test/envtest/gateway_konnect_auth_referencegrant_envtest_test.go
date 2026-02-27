package envtest

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	kogateway "github.com/kong/kong-operator/v2/controller/gateway"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/pkg/vars"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
)

func TestGatewayKonnectAPIAuthReferenceGrant(t *testing.T) {
	t.Parallel()

	scheme := managerscheme.Get()
	ctx := t.Context()

	cfg, gwNs := Setup(t, ctx, scheme, WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme)

	r := &kogateway.Reconciler{
		Client:                mgr.GetClient(),
		Scheme:                scheme,
		Namespace:             gwNs.Name,
		DefaultDataPlaneImage: "kong:latest",
	}
	StartReconcilers(ctx, t, mgr, logs, r)

	c := mgr.GetClient()

	// Create a second namespace for KonnectAPIAuthConfiguration.
	authNs := deploy.Namespace(t, ctx, c)

	// Create GatewayConfiguration in the gateway namespace that references
	// a KonnectAPIAuthConfiguration in the auth namespace (cross-namespace).
	gwConfig := deploy.GatewayConfiguration(t, ctx, c,
		func(obj client.Object) { obj.SetName("test-gwconfig"); obj.SetNamespace(gwNs.Name) },
		deploy.WithGatewayConfigKonnectAuthRef("my-auth", authNs.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gwConfig) })

	// Create a GatewayClass that references the GatewayConfiguration.
	gc := deploy.GatewayClass(t, ctx, c,
		func(obj client.Object) { obj.SetName("test-gc-xns-auth") },
		deploy.WithGatewayClassControllerName(vars.ControllerName()),
		deploy.WithGatewayClassParametersRef("gateway-operator.konghq.com", "GatewayConfiguration", gwConfig.Name, gwNs.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gc) })

	t.Log("patching GatewayClass status to Accepted=True")
	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), waitTime, tickTime)

	// Create a Gateway that uses the GatewayClass.
	gw := deploy.Gateway(t, ctx, c,
		func(obj client.Object) { obj.SetName("test-gw-xns-auth"); obj.SetNamespace(gwNs.Name) },
		deploy.WithGatewayClassName(gc.Name),
		deploy.WithGatewayListeners(gatewayv1.Listener{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80}),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gw) })

	t.Run("no derived grant is created without user KongReferenceGrant", func(t *testing.T) {
		require.Never(t, func() bool {
			var grants configurationv1alpha1.KongReferenceGrantList
			if err := c.List(ctx, &grants, client.InNamespace(authNs.Name)); err != nil {
				return false
			}
			for _, g := range grants.Items {
				for _, from := range g.Spec.From {
					if string(from.Group) == konnectv1alpha1.GroupVersion.Group &&
						string(from.Kind) == "KonnectGatewayControlPlane" {
						return true
					}
				}
			}
			return false
		}, waitTime, tickTime, "derived KonnectGatewayControlPlane grant should not exist without user grant")
	})

	t.Run("derived grant is created after user creates KongReferenceGrant", func(t *testing.T) {
		t.Log("creating user KongReferenceGrant allowing GatewayConfiguration -> KonnectAPIAuthConfiguration")
		userGrant := deploy.KongReferenceGrant(t, ctx, c,
			func(obj client.Object) {
				obj.SetNamespace(authNs.Name)
			},
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group:     configurationv1alpha1.Group(operatorv2beta1.SchemeGroupVersion.Group),
				Kind:      "GatewayConfiguration",
				Namespace: configurationv1alpha1.Namespace(gwNs.Name),
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
				Kind:  "KonnectAPIAuthConfiguration",
				Name:  new(configurationv1alpha1.ObjectName("my-auth")),
			}),
		)

		t.Log("waiting for the operator to create the derived KonnectGatewayControlPlane -> KonnectAPIAuthConfiguration grant")
		require.Eventually(t, func() bool {
			var grants configurationv1alpha1.KongReferenceGrantList
			if err := c.List(ctx, &grants, client.InNamespace(authNs.Name)); err != nil {
				t.Logf("error listing grants: %v", err)
				return false
			}
			for _, g := range grants.Items {
				if g.Name == userGrant.Name {
					continue
				}
				if isDerivedKonnectCPGrant(&g, gwNs.Name, "my-auth") {
					return true
				}
			}
			return false
		}, waitTime, tickTime, "derived KonnectGatewayControlPlane grant should be created")

		t.Run("derived grant is removed after user deletes KongReferenceGrant", func(t *testing.T) {
			t.Log("deleting user KongReferenceGrant")
			require.NoError(t, c.Delete(ctx, userGrant))

			t.Log("waiting for the derived grant to be removed")
			require.Eventually(t, func() bool {
				var grants configurationv1alpha1.KongReferenceGrantList
				if err := c.List(ctx, &grants, client.InNamespace(authNs.Name)); err != nil {
					t.Logf("error listing grants: %v", err)
					return false
				}
				for _, g := range grants.Items {
					if isDerivedKonnectCPGrant(&g, gwNs.Name, "my-auth") {
						return false
					}
				}
				return true
			}, waitTime, tickTime, "derived grant should be cleaned up after user grant is deleted")
		})
	})

	t.Run("derived grant without name restriction is created when user grant omits Name", func(t *testing.T) {
		t.Log("creating user KongReferenceGrant without Name in To (allowing any KonnectAPIAuthConfiguration)")
		userGrant := deploy.KongReferenceGrant(t, ctx, c,
			func(obj client.Object) {
				obj.SetNamespace(authNs.Name)
			},
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group:     configurationv1alpha1.Group(operatorv2beta1.SchemeGroupVersion.Group),
				Kind:      "GatewayConfiguration",
				Namespace: configurationv1alpha1.Namespace(gwNs.Name),
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
				Kind:  "KonnectAPIAuthConfiguration",
			}),
		)
		t.Cleanup(func() { _ = c.Delete(ctx, userGrant) })

		t.Log("waiting for the operator to create the derived grant")
		require.Eventually(t, func() bool {
			var grants configurationv1alpha1.KongReferenceGrantList
			if err := c.List(ctx, &grants, client.InNamespace(authNs.Name)); err != nil {
				t.Logf("error listing grants: %v", err)
				return false
			}
			for _, g := range grants.Items {
				if g.Name == userGrant.Name {
					continue
				}
				if isDerivedKonnectCPGrant(&g, gwNs.Name, "my-auth") {
					return true
				}
			}
			return false
		}, waitTime, tickTime, "derived grant should be created when user grant allows any auth name")

		t.Log("cleaning up user grant for next sub-test")
		require.NoError(t, c.Delete(ctx, userGrant))

		require.Eventually(t, func() bool {
			var grants configurationv1alpha1.KongReferenceGrantList
			if err := c.List(ctx, &grants, client.InNamespace(authNs.Name)); err != nil {
				return false
			}
			for _, g := range grants.Items {
				if isDerivedKonnectCPGrant(&g, gwNs.Name, "my-auth") {
					return false
				}
			}
			return true
		}, waitTime, tickTime, "derived grant should be cleaned up")
	})
}

// isDerivedKonnectCPGrant checks whether a KongReferenceGrant is a derived grant
// from KonnectGatewayControlPlane to KonnectAPIAuthConfiguration.
func isDerivedKonnectCPGrant(g *configurationv1alpha1.KongReferenceGrant, fromNS, authName string) bool {
	hasFrom := lo.ContainsBy(g.Spec.From, func(from configurationv1alpha1.ReferenceGrantFrom) bool {
		return string(from.Group) == konnectv1alpha1.GroupVersion.Group &&
			string(from.Kind) == "KonnectGatewayControlPlane" &&
			string(from.Namespace) == fromNS
	})
	hasTo := lo.ContainsBy(g.Spec.To, func(to configurationv1alpha1.ReferenceGrantTo) bool {
		return string(to.Group) == konnectv1alpha1.GroupVersion.Group &&
			string(to.Kind) == "KonnectAPIAuthConfiguration" &&
			(authName == "" || (to.Name != nil && string(*to.Name) == authName))
	})
	return hasFrom && hasTo
}

func TestGatewayKonnectAPIAuthReferenceGrant_CleanupOnGatewayDeletion(t *testing.T) {
	t.Parallel()

	scheme := managerscheme.Get()
	ctx := t.Context()

	cfg, gwNs := Setup(t, ctx, scheme, WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme)

	r := &kogateway.Reconciler{
		Client:                mgr.GetClient(),
		Scheme:                scheme,
		Namespace:             gwNs.Name,
		DefaultDataPlaneImage: "kong:latest",
	}
	StartReconcilers(ctx, t, mgr, logs, r)

	c := mgr.GetClient()
	authNs := deploy.Namespace(t, ctx, c)

	gwConfig := deploy.GatewayConfiguration(t, ctx, c,
		func(obj client.Object) { obj.SetName("test-gwconfig-cleanup"); obj.SetNamespace(gwNs.Name) },
		deploy.WithGatewayConfigKonnectAuthRef("my-auth-cleanup", authNs.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gwConfig) })

	gc := deploy.GatewayClass(t, ctx, c,
		func(obj client.Object) { obj.SetName("test-gc-xns-cleanup") },
		deploy.WithGatewayClassControllerName(vars.ControllerName()),
		deploy.WithGatewayClassParametersRef("gateway-operator.konghq.com", "GatewayConfiguration", gwConfig.Name, gwNs.Name),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, gc) })

	t.Log("patching GatewayClass status to Accepted=True")
	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), waitTime, tickTime)

	// Create user KongReferenceGrant first.
	userGrant := deploy.KongReferenceGrant(t, ctx, c,
		func(obj client.Object) {
			obj.SetNamespace(authNs.Name)
		},
		deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
			Group:     configurationv1alpha1.Group(operatorv2beta1.SchemeGroupVersion.Group),
			Kind:      "GatewayConfiguration",
			Namespace: configurationv1alpha1.Namespace(gwNs.Name),
		}),
		deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
			Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
			Kind:  "KonnectAPIAuthConfiguration",
			Name:  new(configurationv1alpha1.ObjectName("my-auth-cleanup")),
		}),
	)
	t.Cleanup(func() { _ = c.Delete(ctx, userGrant) })

	gw := deploy.Gateway(t, ctx, c,
		func(obj client.Object) { obj.SetName("test-gw-xns-cleanup"); obj.SetNamespace(gwNs.Name) },
		deploy.WithGatewayClassName(gc.Name),
		deploy.WithGatewayListeners(gatewayv1.Listener{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80}),
	)

	t.Log("waiting for derived grant to appear")
	require.Eventually(t, func() bool {
		var grants configurationv1alpha1.KongReferenceGrantList
		if err := c.List(ctx, &grants, client.InNamespace(authNs.Name)); err != nil {
			return false
		}
		for _, g := range grants.Items {
			if g.Name == userGrant.Name {
				continue
			}
			if isDerivedKonnectCPGrant(&g, gwNs.Name, "my-auth-cleanup") {
				return true
			}
		}
		return false
	}, waitTime, tickTime, "derived grant should exist")

	t.Log("deleting Gateway to trigger cleanup")
	require.NoError(t, c.Delete(ctx, gw))

	t.Log("waiting for derived grant to be cleaned up after Gateway deletion")
	require.Eventually(t, func() bool {
		var grants configurationv1alpha1.KongReferenceGrantList
		if err := c.List(ctx, &grants, client.InNamespace(authNs.Name)); err != nil {
			t.Logf("error listing grants: %v", err)
			return false
		}
		for _, g := range grants.Items {
			if g.Name == userGrant.Name {
				continue
			}
			if isDerivedKonnectCPGrant(&g, gwNs.Name, "my-auth-cleanup") {
				return false
			}
		}
		return true
	}, waitTime, tickTime, "derived grant should be cleaned up after Gateway deletion")

}
