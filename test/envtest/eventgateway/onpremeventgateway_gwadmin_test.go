package eventgateway

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref/gwadminreftest"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestOnPremEventGatewayGWAdmin checks that an OnPremEventGateway reports
// what its GWAdmin is missing until it has gw-admin pods, that a change of
// the GWAdmin, of its pods or of a KongReferenceGrant updates the gateways
// that reference it, and that a GWAdmin in another namespace is used only
// with a KongReferenceGrant. The GWAdmin controller doesn't run: the test
// publishes the API of the GWAdmin, its TLS material, and the EndpointSlice
// of its API Service, itself.
func TestOnPremEventGatewayGWAdmin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	// A resync would update the gateways without the watches this tests.
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()
	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig, gwAdmin.reconciler(ctx, mgr))

	// reports checks the GWAdminResolved condition of the gateway, and its
	// Programmed condition: True once its GWAdmin is resolved and the
	// configuration pushed, waiting for the GWAdmin otherwise.
	reports := func(t *testing.T, gateway *eventgatewayv1alpha1.OnPremEventGateway, reason kcfgconsts.ConditionReason, message string) {
		t.Helper()
		resolved := reason == eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedReason
		wantStatus, wantProgrammedReason := metav1.ConditionFalse, eventgatewayv1alpha1.DependenciesNotReadyReason
		if resolved {
			wantStatus, wantProgrammedReason = metav1.ConditionTrue, eventgatewayv1alpha1.OnPremEventGatewayProgrammedReason
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
				assert.Equal(ct, wantStatus, programmed.Status)
				assert.Equal(ct, string(wantProgrammedReason), programmed.Reason)
			}
		}, waitTime, tickTime)
	}
	notFound := eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotFoundReason
	notPermitted := eventgatewayv1alpha1.OnPremEventGatewayGWAdminRefNotPermittedReason
	notReady := eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotReadyReason
	resolved := eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedReason
	gwAdminKey := ns.Name + "/" + testGWAdminName
	servedBy := "The GWAdmin " + gwAdminKey + " serves the gateway"

	gateway := &eventgatewayv1alpha1.OnPremEventGateway{
		Name: "gateway", Namespace: ns.Name,
		Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{GWAdminRef: eventgatewayv1alpha1.GWAdminRef{Name: testGWAdminName}},
	}
	require.NoError(t, cl.Create(ctx, gateway))
	reports(t, gateway, notFound, "The GWAdmin "+gwAdminKey+" does not exist")

	gwAdmin.createGWAdmin(ctx)
	reports(t, gateway, notReady, "The GWAdmin "+gwAdminKey+" hasn't published its API yet")

	gwAdmin.publishAPI(ctx)
	reports(t, gateway, notReady, "The operator client certificate Secret "+ns.Name+"/"+gwadminref.OperatorClientTLSSecretName(gwAdmin.gwAdmin)+
		" of the GWAdmin does not exist")

	// A Secret under that name, not issued for the GWAdmin, is not used.
	foreign := &corev1.Secret{
		Name: gwadminref.OperatorClientTLSSecretName(gwAdmin.gwAdmin), Namespace: ns.Name,
		Labels: map[string]string{
			consts.GWAdminUIDLabel:                        "another-uid",
			consts.GatewayOperatorManagedByNameLabel:      testGWAdminName,
			consts.GatewayOperatorManagedByNamespaceLabel: ns.Name,
		},
	}
	require.NoError(t, cl.Create(ctx, foreign))
	reports(t, gateway, notReady, "The operator client certificate Secret "+ns.Name+"/"+gwadminref.OperatorClientTLSSecretName(gwAdmin.gwAdmin)+
		" doesn't belong to the GWAdmin "+gwAdminKey)
	require.NoError(t, cl.Delete(ctx, foreign))

	gwAdmin.issueOperatorClientCertificate(ctx)
	reports(t, gateway, notReady, "The GWAdmin "+gwAdminKey+" has no gw-admin pod")

	pod := gwAdmin.startPod(ctx, "gw-admin-0", "10.0.0.1")
	reports(t, gateway, resolved, servedBy)

	t.Run("a gateway of another namespace needs a KongReferenceGrant", func(t *testing.T) {
		other := envtest.CreateNamespace(ctx, t, cl)
		crossNamespace := &eventgatewayv1alpha1.OnPremEventGateway{
			Name: "gateway", Namespace: other.Name,
			Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{GWAdminRef: eventgatewayv1alpha1.GWAdminRef{Name: testGWAdminName, Namespace: new(ns.Name)}},
		}
		require.NoError(t, cl.Create(ctx, crossNamespace))
		notGranted := "The gateway may not use its GWAdmin: no KongReferenceGrant in namespace " + ns.Name +
			" allows the OnPremEventGateways of namespace " + other.Name + " to reference the GWAdmin " + gwAdminKey
		reports(t, crossNamespace, notPermitted, notGranted)

		// served tells whether the gw-admin pod serves the gateway of the
		// other namespace.
		served := func(t *testing.T, want bool) {
			t.Helper()
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				_, ok, expected := pod.state(gatewayID(ct, crossNamespace))
				assert.Equal(ct, want, ok)
				assert.Equal(ct, want, slices.Contains(expected, gatewayID(ct, crossNamespace)))
			}, waitTime, tickTime)
		}

		grant := gwadminreftest.Grant(ns.Name, "gateways-of-"+other.Name, other.Name, "")
		require.NoError(t, cl.Create(ctx, grant))
		reports(t, crossNamespace, resolved, servedBy)
		served(t, true)

		require.NoError(t, cl.Delete(ctx, grant))
		reports(t, crossNamespace, notPermitted, notGranted)
		served(t, false)
		_, ok, expected := pod.state(gatewayID(t, gateway))
		require.True(t, ok, "the gateway of the namespace of the GWAdmin is still served")
		require.Contains(t, expected, gatewayID(t, gateway))
	})

	t.Run("a certificate deleted while the gateway may not use its GWAdmin isn't trusted again once it may", func(t *testing.T) {
		// The gateway is deleted from gw-admin meanwhile, with its
		// certificates, and registered again under the same id once it may.
		other := envtest.CreateNamespace(ctx, t, cl)
		crossNamespace := &eventgatewayv1alpha1.OnPremEventGateway{
			Name: "gateway", Namespace: other.Name,
			Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{GWAdminRef: eventgatewayv1alpha1.GWAdminRef{Name: testGWAdminName, Namespace: new(ns.Name)}},
		}
		require.NoError(t, cl.Create(ctx, crossNamespace))
		grant := gwadminreftest.Grant(ns.Name, "gateways-of-"+other.Name, other.Name, "")
		require.NoError(t, cl.Create(ctx, grant))
		require.NoError(t, cl.Create(ctx, &corev1.Secret{
			Name: "dp-tls", Namespace: other.Name,
			Data: map[string][]byte{"tls.crt": []byte(selfSignedCertificatePEM(t))},
		}))
		cert := objectFromYAML[configurationv1alpha1.EventGatewayDataPlaneCertificate](t, other.Name, `
metadata: {name: dp-certificate}
spec:`+onPremEventGatewayParentRef(crossNamespace.Name)+`
  apiSpec:
    certificate: {type: secretRef, secretRef: {name: dp-tls, key: tls.crt}}
`)
		require.NoError(t, cl.Create(ctx, cert))
		trusted := func(ct *assert.CollectT) bool {
			g, ok, _ := pod.state(gatewayID(ct, crossNamespace))
			_, held := g.certificates[uuid.MustParse(string(cert.UID))]
			return ok && held
		}
		require.EventuallyWithT(t, func(ct *assert.CollectT) { assert.True(ct, trusted(ct)) }, waitTime, tickTime)

		require.NoError(t, cl.Delete(ctx, grant))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			_, ok, _ := pod.state(gatewayID(ct, crossNamespace))
			assert.False(ct, ok, "the pod still serves the gateway")
		}, waitTime, tickTime)
		require.NoError(t, cl.Delete(ctx, cert))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			err := cl.Get(ctx, client.ObjectKeyFromObject(cert), &configurationv1alpha1.EventGatewayDataPlaneCertificate{})
			assert.True(ct, apierrors.IsNotFound(err), "no pod serves the gateway: nothing holds the certificate: %v", err)
		}, waitTime, tickTime)

		grant.ResourceVersion = ""
		require.NoError(t, cl.Create(ctx, grant))
		reports(t, crossNamespace, resolved, servedBy)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			_, ok, _ := pod.state(gatewayID(ct, crossNamespace))
			assert.True(ct, ok)
		}, waitTime, tickTime)
		require.Never(t, func() bool {
			ct := &assert.CollectT{}
			return trusted(ct)
		}, 2*time.Second, tickTime, "the deleted certificate is trusted again")
	})

	t.Run("a pod that goes away is noticed", func(t *testing.T) {
		gwAdmin.updateEndpoints(ctx, func(endpoints []discoveryv1.Endpoint) []discoveryv1.Endpoint {
			endpoints[0].Conditions.Terminating = new(true)
			endpoints[0].Conditions.Serving = new(false)
			return endpoints
		})
		reports(t, gateway, notReady, "The GWAdmin "+gwAdminKey+" has no gw-admin pod")
	})

	t.Run("a GWAdmin that goes away is noticed", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, gwAdmin.gwAdmin))
		// The pods of a deleted GWAdmin are collected after it.
		reports(t, gateway, notFound, "The GWAdmin "+gwAdminKey+" does not exist, but its gw-admin pods are still running")

		gwAdmin.stopPod(ctx, "10.0.0.1")
		reports(t, gateway, notFound, "The GWAdmin "+gwAdminKey+" does not exist")
	})
}
