package eventgateway

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestOnPremEventGatewayDataPlaneCertificates verifies the lifecycle of the
// data plane certificates of OnPremEventGateways: pushed to every gw-admin
// pod, a new one included, removed from every pod once deleted, moved to
// another gateway only once the gateway it was pushed to no longer trusts
// it, even while that gateway fails to push, and released once their gateway
// is deleted.
func TestOnPremEventGatewayDataPlaneCertificates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()
	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig, gwAdmin.reconciler(ctx, mgr))

	newGateway := func(name string) *eventgatewayv1alpha1.OnPremEventGateway {
		return objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: `+name+`}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)
	}
	gatewayA, gatewayB := newGateway("gateway-a"), newGateway("gateway-b")
	require.NoError(t, cl.Create(ctx, gatewayA))
	require.NoError(t, cl.Create(ctx, gatewayB))
	gwAdmin.createGWAdmin(ctx)
	gwAdmin.publishAPI(ctx)
	gwAdmin.issueOperatorClientCertificate(ctx)
	pod0 := gwAdmin.startPod(ctx, "gw-admin-0", "10.0.0.1")
	pod1 := gwAdmin.startPod(ctx, "gw-admin-1", "10.0.0.2")

	require.NoError(t, cl.Create(ctx, &corev1.Secret{
		Name: "dp-tls", Namespace: ns.Name,
		Data: map[string][]byte{"tls.crt": []byte(selfSignedCertificatePEM(t))},
	}))
	newCertificate := func(name, gateway string) *configurationv1alpha1.EventGatewayDataPlaneCertificate {
		return objectFromYAML[configurationv1alpha1.EventGatewayDataPlaneCertificate](t, ns.Name, `
metadata: {name: `+name+`}
spec:`+onPremEventGatewayParentRef(gateway)+`
  apiSpec:
    certificate: {type: secretRef, secretRef: {name: dp-tls, key: tls.crt}}
`)
	}
	certID := func(cert *configurationv1alpha1.EventGatewayDataPlaneCertificate) uuid.UUID {
		return uuid.MustParse(string(cert.UID))
	}
	// trusts tells whether the pod holds the certificate for the gateway.
	trusts := func(ct *assert.CollectT, pod *fakeGWAdmin, gateway *eventgatewayv1alpha1.OnPremEventGateway, cert *configurationv1alpha1.EventGatewayDataPlaneCertificate) bool {
		g, ok, _ := pod.state(gatewayID(ct, gateway))
		_, held := g.certificates[certID(cert)]
		return ok && held
	}
	getCertificate := func(ct *assert.CollectT, cert *configurationv1alpha1.EventGatewayDataPlaneCertificate) *configurationv1alpha1.EventGatewayDataPlaneCertificate {
		got := &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(cert), got)) {
			return nil
		}
		return got
	}
	// expectTrusted waits for every pod to trust the certificate for the
	// gateway, and for the certificate to be Programmed: it carries no
	// finalizer, nor annotation of the operator.
	expectTrusted := func(t *testing.T, gateway *eventgatewayv1alpha1.OnPremEventGateway, cert *configurationv1alpha1.EventGatewayDataPlaneCertificate, pods ...*fakeGWAdmin) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for i, pod := range pods {
				assert.True(ct, trusts(ct, pod, gateway, cert), "gw-admin pod %d doesn't trust the certificate for %s", i, gateway.Name)
			}
			got := getCertificate(ct, cert)
			if got == nil {
				return
			}
			assert.Empty(ct, got.Finalizers)
			assert.Empty(ct, got.Annotations)
			if programmed := apimeta.FindStatusCondition(got.Status.Conditions, "Programmed"); assert.NotNil(ct, programmed) {
				assert.Equal(ct, metav1.ConditionTrue, programmed.Status, programmed.Message)
				assert.Contains(ct, programmed.Message, "OnPremEventGateway "+ns.Name+"/"+gateway.Name)
			}
		}, waitTime, tickTime)
	}

	first := newCertificate("first", gatewayA.Name)

	t.Run("a certificate is pushed to every gw-admin pod, a new one included", func(t *testing.T) {
		require.NoError(t, cl.Create(ctx, first))
		expectTrusted(t, gatewayA, first, pod0, pod1)
		pod2 := gwAdmin.startPod(ctx, "gw-admin-2", "10.0.0.3")
		t.Cleanup(func() { gwAdmin.stopPod(ctx, "10.0.0.3") })
		expectTrusted(t, gatewayA, first, pod0, pod1, pod2)
	})

	t.Run("a deleted certificate is removed from every gw-admin pod, then deleted", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, first))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for i, pod := range []*fakeGWAdmin{pod0, pod1} {
				assert.False(ct, trusts(ct, pod, gatewayA, first), "gw-admin pod %d still trusts the certificate", i)
			}
			err := cl.Get(ctx, client.ObjectKeyFromObject(first), &configurationv1alpha1.EventGatewayDataPlaneCertificate{})
			assert.True(ct, apierrors.IsNotFound(err), "nothing holds the certificate: %v", err)
		}, waitTime, tickTime)
	})

	t.Run("a pod that can't be reached stops trusting a deleted certificate once reached", func(t *testing.T) {
		leaving := newCertificate("leaving", gatewayA.Name)
		require.NoError(t, cl.Create(ctx, leaving))
		expectTrusted(t, gatewayA, leaving, pod0, pod1)

		// e.g. on a node cut off: it holds what it was told.
		gwAdmin.disconnect(ctx, "10.0.0.2")
		require.NoError(t, cl.Delete(ctx, leaving))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.False(ct, trusts(ct, pod0, gatewayA, leaving))
			err := cl.Get(ctx, client.ObjectKeyFromObject(leaving), &configurationv1alpha1.EventGatewayDataPlaneCertificate{})
			assert.True(ct, apierrors.IsNotFound(err), "nothing holds the certificate: %v", err)
		}, waitTime, tickTime)
		require.True(t, func() bool {
			g, _, _ := pod1.state(gatewayID(t, gatewayA))
			_, held := g.certificates[certID(leaving)]
			return held
		}(), "the pod cut off still trusts it")

		gwAdmin.reconnect(ctx, "10.0.0.2")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.False(ct, trusts(ct, pod1, gatewayA, leaving), "the pod reached again still trusts the deleted certificate")
		}, waitTime, tickTime)
	})

	t.Run("a certificate a pod rejects holds neither the others, their Programmed condition nor the push of the gateway", func(t *testing.T) {
		require.NoError(t, cl.Create(ctx, &corev1.Secret{
			Name: "dp-tls-rejected", Namespace: ns.Name,
			Data: map[string][]byte{"tls.crt": []byte(selfSignedCertificatePEM(t))},
		}))
		rejectedSecret := &corev1.Secret{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "dp-tls-rejected"}, rejectedSecret))
		rejectedPEM := string(rejectedSecret.Data["tls.crt"])
		pod1.setRejectCertificate(func(_ uuid.UUID, body gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem) bool {
			return body.Certificate == rejectedPEM
		})
		t.Cleanup(func() { pod1.setRejectCertificate(nil) })

		kept := newCertificate("kept", gatewayA.Name)
		rejected := newCertificate("rejected", gatewayA.Name)
		rejected.Spec.APISpec.Certificate.SecretRef.Name = "dp-tls-rejected"
		require.NoError(t, cl.Create(ctx, kept))
		require.NoError(t, cl.Create(ctx, rejected))
		expectTrusted(t, gatewayA, kept, pod0, pod1)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.True(ct, trusts(ct, pod0, gatewayA, rejected))
			assert.False(ct, trusts(ct, pod1, gatewayA, rejected))
			if got := getCertificate(ct, rejected); got != nil {
				if programmed := apimeta.FindStatusCondition(got.Status.Conditions, "Programmed"); assert.NotNil(ct, programmed) {
					assert.Equal(ct, metav1.ConditionFalse, programmed.Status)
					assert.Equal(ct, "Invalid", programmed.Reason, "it isn't coming")
					assert.Contains(ct, programmed.Message, "rejected by gw-admin: gw-admin pod gw-admin-1: failed to register the data plane certificate "+ns.Name+"/rejected")
				}
			}
			g := &eventgatewayv1alpha1.OnPremEventGateway{}
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gatewayA), g)) {
				if programmed := apimeta.FindStatusCondition(g.Status.Conditions, "Programmed"); assert.NotNil(ct, programmed) {
					assert.Equal(ct, metav1.ConditionTrue, programmed.Status, "only the certificate reports it: the rest of the configuration doesn't depend on it")
				}
			}
		}, waitTime, tickTime)
		require.NoError(t, cl.Delete(ctx, rejected))
		require.NoError(t, cl.Delete(ctx, kept))
	})

	moved := newCertificate("moved", gatewayA.Name)

	t.Run("a certificate moved to another gateway is pushed to it, and removed from the previous one, even while that one rejected it", func(t *testing.T) {
		require.NoError(t, cl.Create(ctx, moved))
		expectTrusted(t, gatewayA, moved, pod0, pod1)

		// A pod rejects the certificate for gateway-a from now on, so it is
		// no longer Programmed, and may move.
		pod1.setRejectCertificate(func(gateway uuid.UUID, _ gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem) bool {
			return gateway == gatewayID(t, gatewayA)
		})
		t.Cleanup(func() { pod1.setRejectCertificate(nil) })
		// The pod was told already: it is pushed again once the Secret
		// changes.
		secret := &corev1.Secret{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "dp-tls"}, secret))
		secret.Data["tls.crt"] = []byte(selfSignedCertificatePEM(t))
		require.NoError(t, cl.Update(ctx, secret))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if got := getCertificate(ct, moved); got != nil {
				programmed := apimeta.FindStatusCondition(got.Status.Conditions, "Programmed")
				if assert.NotNil(ct, programmed) {
					assert.Equal(ct, metav1.ConditionFalse, programmed.Status)
				}
			}
		}, waitTime, tickTime)

		require.NoError(t, cl.Patch(ctx, moved, client.RawPatch("application/merge-patch+json",
			[]byte(`{"spec":{"parentRef":{"name":"`+gatewayB.Name+`"}}}`))))
		expectTrusted(t, gatewayB, moved, pod0, pod1)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for i, pod := range []*fakeGWAdmin{pod0, pod1} {
				assert.False(ct, trusts(ct, pod, gatewayA, moved), "gw-admin pod %d still trusts the certificate for the previous gateway", i)
			}
		}, waitTime, tickTime)
	})

	t.Run("a deleted gateway takes its certificates with it", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, gatewayB))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			err := cl.Get(ctx, client.ObjectKeyFromObject(gatewayB), &eventgatewayv1alpha1.OnPremEventGateway{})
			assert.True(ct, apierrors.IsNotFound(err), "%v", err)
			for i, pod := range []*fakeGWAdmin{pod0, pod1} {
				_, ok, _ := pod.state(gatewayID(ct, gatewayB))
				assert.False(ct, ok, "gw-admin pod %d still serves the deleted gateway", i)
			}
		}, waitTime, tickTime)
		require.NoError(t, cl.Delete(ctx, moved))
	})
}
