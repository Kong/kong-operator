package eventgateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	crdschema "github.com/kong/kong-operator/v2/controller/crdschema"
	egdataplane "github.com/kong/kong-operator/v2/controller/eventgateway/dataplane"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// TestKEGDataPlaneOnPrem verifies that a KegDataPlane of an
// OnPremEventGateway gets a client certificate issued from the cp client CA
// of the GWAdmin of the gateway, registered on every gw-admin pod before its
// Deployment uses it, and a Deployment pulling its configuration from the cp
// endpoint of the GWAdmin; that a rotation of the cp client CA issues a new
// certificate, and that the previous one is removed from gw-admin once the
// Deployment rolled out onto the new one.
func TestKEGDataPlaneOnPrem(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, kegCRDGroups)
	require.NoError(t, err)

	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig,
		gwAdmin.reconciler(ctx, mgr),
		&egdataplane.Reconciler{
			Client: cl,
			// The cluster CA isn't used for the data planes of an
			// OnPremEventGateway: the GWAdmin controller publishes its cp
			// client CA in the operator namespace.
			ClusterCASecretName:      "unused-cluster-ca",
			ClusterCASecretNamespace: "unused-cluster-ca-namespace",
			OperatorNamespace:        ns.Name,
			CertTTL:                  consts.DefaultCertTTL,
			TypeConverter:            ssaProvider,
		},
		&crdschema.Reconciler{Client: cl, Provider: ssaProvider},
	)

	pod := gwAdmin.provision(ctx)
	gateway := objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: gateway}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)
	require.NoError(t, cl.Create(ctx, gateway))
	egdp := objectFromYAML[eventgatewayv1alpha1.KegDataPlane](t, ns.Name, `
metadata: {name: keg}
spec:
  controlPlaneRef:
    type: onpremNamespacedRef
    onpremNamespacedRef: {name: gateway}
`)
	require.NoError(t, cl.Create(ctx, egdp))

	conditionOf := func(ct *assert.CollectT, conditionType kcfgconsts.ConditionType) *metav1.Condition {
		current := &eventgatewayv1alpha1.KegDataPlane{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(egdp), current)) {
			return nil
		}
		cond := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionType))
		assert.NotNil(ct, cond, "condition %s", conditionType)
		return cond
	}
	expectNotResolved := func(t *testing.T, message string) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := conditionOf(ct, eventgatewayv1alpha1.OnPremEventGatewayResolvedType); cond != nil {
				assert.Equal(ct, metav1.ConditionFalse, cond.Status)
				assert.Equal(ct, string(eventgatewayv1alpha1.KegDataPlaneGWAdminNotReadyReason), cond.Reason)
				assert.Contains(ct, cond.Message, message)
			}
		}, waitTime, tickTime)
	}
	deploymentOf := func(ct *assert.CollectT) *appsv1.Deployment {
		d := &appsv1.Deployment{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(egdp), d)) {
			return nil
		}
		return d
	}
	// certificateIn returns the client certificate in the Secret.
	certificateIn := func(ct *assert.CollectT, secretName string) *x509.Certificate {
		secret := &corev1.Secret{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: secretName}, secret)) {
			return nil
		}
		block, _ := pem.Decode(secret.Data["tls.crt"])
		if !assert.NotNil(ct, block) {
			return nil
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if !assert.NoError(ct, err) {
			return nil
		}
		return cert
	}
	parseCA := func(caPEM []byte) *x509.Certificate {
		block, _ := pem.Decode(caPEM)
		require.NotNil(t, block)
		ca, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)
		return ca
	}
	certVolumeSecret := func(d *appsv1.Deployment) string {
		for _, v := range d.Spec.Template.Spec.Volumes {
			if v.Name == egdataplane.KonnectCertVolumeName && v.Secret != nil {
				return v.Secret.SecretName
			}
		}
		return ""
	}

	t.Run("a data plane waits for what its GWAdmin publishes", func(t *testing.T) {
		expectNotResolved(t, "hasn't published its cp endpoint yet")
		gwAdmin.publishCP(ctx)
		expectNotResolved(t, "cp-client-ca of the GWAdmin does not exist")
		require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(egdp), &appsv1.Deployment{})), "no Deployment yet")
	})

	clientCA := parseCA(gwAdmin.publishClientCA(ctx))
	var firstSecret string
	var firstCertID uuid.UUID

	t.Run("its client certificate is issued from the cp client CA and registered on gw-admin before the Deployment uses it", func(t *testing.T) {
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := conditionOf(ct, eventgatewayv1alpha1.GWAdminCertificateRegisteredType); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
			d := deploymentOf(ct)
			if d == nil {
				return
			}
			firstSecret = certVolumeSecret(d)
			cert := certificateIn(ct, firstSecret)
			if cert == nil {
				return
			}
			assert.NoError(ct, cert.CheckSignatureFrom(clientCA), "the certificate must be issued by the cp client CA")
			assert.Contains(ct, cert.ExtKeyUsage, x509.ExtKeyUsageClientAuth)

			// The EventGatewayDataPlaneCertificate references the gateway, is
			// Programmed, and every gw-admin pod trusts the certificate.
			certs := &configurationv1alpha1.EventGatewayDataPlaneCertificateList{}
			if !assert.NoError(ct, cl.List(ctx, certs, client.InNamespace(ns.Name))) || !assert.Len(ct, certs.Items, 1) {
				return
			}
			certObj := certs.Items[0]
			if assert.NotNil(ct, certObj.Spec.ParentRef) {
				assert.Equal(ct, "OnPremEventGateway", certObj.Spec.ParentRef.Kind)
				assert.Equal(ct, gateway.Name, certObj.Spec.ParentRef.Name)
			}
			assert.True(ct, apimeta.IsStatusConditionTrue(certObj.Status.Conditions, "Programmed"))
			firstCertID = uuid.MustParse(string(certObj.UID))
			g, ok, _ := pod.state(gatewayID(ct, gateway))
			if assert.True(ct, ok) {
				pinned, ok := g.certificates[firstCertID]
				if assert.True(ct, ok, "gw-admin must trust the certificate") {
					secret := &corev1.Secret{}
					if assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: firstSecret}, secret)) {
						assert.Equal(ct, string(secret.Data["tls.crt"]), pinned.Certificate)
					}
				}
			}
		}, waitTime, tickTime)
	})

	t.Run("its Deployment pulls the configuration from the cp endpoint of the GWAdmin and is labelled for it", func(t *testing.T) {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(gateway), gateway))
		d := &appsv1.Deployment{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(egdp), d))
		container := getKEGContainer(t, *d)
		env := map[string]string{}
		for _, e := range container.Env {
			env[e.Name] = e.Value
		}
		require.Equal(t, "testing", env[egdataplane.EnvKonnectRegion])
		require.Equal(t, strings.TrimPrefix(gwadminsynctest.ServerURL(ns.Name), "https://"), env[egdataplane.EnvKonnectDomain])
		require.Equal(t, string(gateway.UID), env[egdataplane.EnvKonnectGatewayClusterID])
		require.Equal(t, "/etc/ssl/certs/ca-certificates.crt", env[egdataplane.EnvSSLCertFile], "the system trust store is kept")
		require.Equal(t, "/var/gw-admin-cp-server-ca/", env[egdataplane.EnvSSLCertDir], "the cp server CA bundle is added to it")
		require.Equal(t, egdataplane.KonnectCertMountPath+"tls.crt", env[egdataplane.EnvKonnectClientCertPath])

		var bundleVolume *corev1.Volume
		for i, v := range d.Spec.Template.Spec.Volumes {
			if v.Secret != nil && v.Secret.SecretName == testGWAdminCPBundle {
				bundleVolume = &d.Spec.Template.Spec.Volumes[i]
			}
		}
		require.NotNil(t, bundleVolume, "the cp server CA bundle must be mounted")

		require.Equal(t, ns.Name, d.Labels[consts.GWAdminDataPlaneNamespaceLabel])
		require.Equal(t, testGWAdminName, d.Labels[consts.GWAdminDataPlaneNameLabel])
		bundle := &corev1.Secret{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: testGWAdminCPBundle}, bundle))
		require.Equal(t, gwadminref.DataPlaneServerBundleChecksum(bundle), d.Spec.Template.Annotations[consts.GWAdminDataPlaneServerBundleAnnotation])
		caSecret := &corev1.Secret{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: gwadminref.CPClientCASecretName(gwAdmin.gwAdmin)}, caSecret))
		require.Equal(t, gwadminref.DataPlaneClientCAFingerprint(caSecret.Data["tls.crt"]), d.Spec.Template.Annotations[consts.GWAdminDataPlaneClientCAAnnotation])
	})

	t.Run("a rotation of the cp client CA issues a new certificate, and the previous one is removed from gw-admin after the rollout", func(t *testing.T) {
		// gw-admin refuses the new certificate at first: the Deployment must
		// keep the previous one until it is registered.
		pod.setRejectCertificates(true)
		rotatedCA := parseCA(gwAdmin.publishClientCA(ctx))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := conditionOf(ct, eventgatewayv1alpha1.GWAdminCertificateRegisteredType); cond != nil {
				assert.Equal(ct, metav1.ConditionFalse, cond.Status)
				assert.Equal(ct, string(eventgatewayv1alpha1.GWAdminCertificateNotProgrammedReason), cond.Reason)
			}
		}, waitTime, tickTime)
		// asserts.Never, not testify's require.Never: the latter runs the
		// condition in a goroutine that it can leave running after it returns.
		asserts.Never(t, func(ctx context.Context) bool {
			d := &appsv1.Deployment{}
			if cl.Get(ctx, client.ObjectKeyFromObject(egdp), d) == nil && certVolumeSecret(d) != firstSecret {
				t.Log("the Deployment uses a certificate gw-admin doesn't trust")
				return true
			}
			// The Secret the Deployment, and the registered certificate, use
			// is kept until the rollout onto the new one.
			if err := cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: firstSecret}, &corev1.Secret{}); err != nil {
				t.Logf("the Secret of the current certificate must be kept: %v", err)
				return true
			}
			return false
		}, 2*time.Second, tickTime, "the Deployment must keep the certificate gw-admin trusts")
		pod.setRejectCertificates(false)
		// A change of the certificate triggers the gateway again.
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(gateway), gateway))
		require.NoError(t, cl.Patch(ctx, gateway, client.RawPatch("application/merge-patch+json", []byte(`{"metadata":{"annotations":{"test/retry":"1"}}}`))))

		var newSecret string
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			d := deploymentOf(ct)
			if d == nil {
				return
			}
			newSecret = certVolumeSecret(d)
			if !assert.NotEqual(ct, firstSecret, newSecret, "the Deployment must use a new certificate") {
				return
			}
			if cert := certificateIn(ct, newSecret); cert != nil {
				assert.NoError(ct, cert.CheckSignatureFrom(rotatedCA))
			}
			// The new certificate is trusted before the Deployment uses it.
			g, ok, _ := pod.state(gatewayID(ct, gateway))
			if assert.True(ct, ok) {
				assert.Len(ct, g.certificates, 2, "both certificates are trusted until the rollout completes")
			}
		}, waitTime, tickTime)

		// The Deployment rolls out onto the new certificate.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			d := deploymentOf(ct)
			if d == nil {
				return
			}
			d.Status = appsv1.DeploymentStatus{
				ObservedGeneration: d.Generation,
				Replicas:           1,
				UpdatedReplicas:    1,
				ReadyReplicas:      1,
				AvailableReplicas:  1,
			}
			assert.NoError(ct, cl.Status().Update(ctx, d))
		}, waitTime, tickTime)

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			g, ok, _ := pod.state(gatewayID(ct, gateway))
			if assert.True(ct, ok) {
				_, stillTrusted := g.certificates[firstCertID]
				assert.False(ct, stillTrusted, "gw-admin must stop trusting the previous certificate")
				assert.Len(ct, g.certificates, 1)
			}
			certs := &configurationv1alpha1.EventGatewayDataPlaneCertificateList{}
			if assert.NoError(ct, cl.List(ctx, certs, client.InNamespace(ns.Name))) {
				assert.Len(ct, certs.Items, 1, "the previous EventGatewayDataPlaneCertificate must be gone")
			}
			err := cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: firstSecret}, &corev1.Secret{})
			assert.True(ct, apierrors.IsNotFound(err), "the previous Secret must be gone: %v", err)
		}, waitTime, tickTime)
	})

	t.Run("the certificate of a deleted data plane is removed from gw-admin", func(t *testing.T) {
		deleted := objectFromYAML[eventgatewayv1alpha1.KegDataPlane](t, ns.Name, `
metadata: {name: keg-deleted}
spec:
  controlPlaneRef:
    type: onpremNamespacedRef
    onpremNamespacedRef: {name: gateway}
`)
		require.NoError(t, cl.Create(ctx, deleted))
		ownedBy := client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: deleted.Name}
		var certObj configurationv1alpha1.EventGatewayDataPlaneCertificate
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			certs := &configurationv1alpha1.EventGatewayDataPlaneCertificateList{}
			if !assert.NoError(ct, cl.List(ctx, certs, client.InNamespace(ns.Name), ownedBy)) || !assert.Len(ct, certs.Items, 1) {
				return
			}
			certObj = certs.Items[0]
			assert.True(ct, apimeta.IsStatusConditionTrue(certObj.Status.Conditions, "Programmed"))
			g, ok, _ := pod.state(gatewayID(ct, gateway))
			if assert.True(ct, ok) {
				assert.Contains(ct, g.certificates, uuid.MustParse(string(certObj.UID)), "gw-admin must trust the certificate")
			}
		}, waitTime, tickTime)

		require.NoError(t, cl.Delete(ctx, deleted))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(deleted), &eventgatewayv1alpha1.KegDataPlane{})), "nothing holds the data plane")
		}, waitTime, tickTime)

		// envtest runs no garbage collector: what the data plane controls
		// must be owned by it, for the garbage collector to delete it.
		require.True(t, metav1.IsControlledBy(&certObj, deleted), "the certificate must be controlled by the data plane")
		secrets := &corev1.SecretList{}
		require.NoError(t, cl.List(ctx, secrets, client.InNamespace(ns.Name), client.MatchingLabels{consts.SecretKEGDataPlaneCertificateLabel: "true"}))
		var owned int
		for i := range secrets.Items {
			if metav1.IsControlledBy(&secrets.Items[i], deleted) {
				owned++
			}
		}
		require.Equal(t, 1, owned, "the client certificate Secret must be controlled by the data plane")

		// The garbage collector deletes the certificate: its finalizer is
		// released once gw-admin no longer trusts it.
		require.NoError(t, cl.Delete(ctx, &certObj, client.PropagationPolicy(metav1.DeletePropagationBackground)))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			err := cl.Get(ctx, client.ObjectKeyFromObject(&certObj), &configurationv1alpha1.EventGatewayDataPlaneCertificate{})
			assert.True(ct, apierrors.IsNotFound(err), "the finalizer of the certificate must be released: %v", err)
			g, ok, _ := pod.state(gatewayID(ct, gateway))
			if assert.True(ct, ok) {
				assert.NotContains(ct, g.certificates, uuid.MustParse(string(certObj.UID)), "gw-admin must stop trusting the certificate")
				assert.Len(ct, g.certificates, 1, "the certificate of the other data plane is kept")
			}
		}, waitTime, tickTime)
	})

	t.Run("a Manual certificate is used once a cp client CA gw-admin trusts issued it, and registered on gw-admin", func(t *testing.T) {
		untrustedPEM, untrustedKeyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg-manual"))
		userSecret := &corev1.Secret{
			Name: "keg-manual-cert", Namespace: ns.Name,
			Data: map[string][]byte{"tls.crt": untrustedPEM, "tls.key": untrustedKeyPEM},
		}
		require.NoError(t, cl.Create(ctx, userSecret))
		manual := objectFromYAML[eventgatewayv1alpha1.KegDataPlane](t, ns.Name, `
metadata: {name: keg-manual}
spec:
  controlPlaneRef:
    type: onpremNamespacedRef
    onpremNamespacedRef: {name: gateway}
  certificateSecret:
    provisioning: Manual
    secretRef: {name: keg-manual-cert}
`)
		require.NoError(t, cl.Create(ctx, manual))
		manualCondition := func(ct *assert.CollectT, conditionType kcfgconsts.ConditionType) *metav1.Condition {
			current := &eventgatewayv1alpha1.KegDataPlane{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(manual), current)) {
				return nil
			}
			cond := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionType))
			assert.NotNil(ct, cond, "condition %s", conditionType)
			return cond
		}

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := manualCondition(ct, eventgatewayv1alpha1.CertificateProvisionedType); cond != nil {
				assert.Equal(ct, metav1.ConditionFalse, cond.Status)
				assert.Equal(ct, string(eventgatewayv1alpha1.CertificateSecretInvalidReason), cond.Reason)
				assert.Contains(ct, cond.Message, "gw-admin would reject it")
			}
		}, waitTime, tickTime)
		require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(manual), &appsv1.Deployment{})), "no Deployment with a certificate gw-admin would reject")

		caSecret := &corev1.Secret{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: gwadminref.CPClientCASecretName(gwAdmin.gwAdmin)}, caSecret))
		ca, err := tls.X509KeyPair(caSecret.Data["tls.crt"], caSecret.Data["tls.key"])
		require.NoError(t, err)
		certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg-manual"), certificate.WithParent(ca))
		userSecret.Data = map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM}
		require.NoError(t, cl.Update(ctx, userSecret))

		ownedBy := client.MatchingLabels{consts.GatewayOperatorManagedByNameLabel: manual.Name}
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := manualCondition(ct, eventgatewayv1alpha1.GWAdminCertificateRegisteredType); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
			certs := &configurationv1alpha1.EventGatewayDataPlaneCertificateList{}
			if !assert.NoError(ct, cl.List(ctx, certs, client.InNamespace(ns.Name), ownedBy)) || !assert.Len(ct, certs.Items, 1) {
				return
			}
			assert.Equal(ct, certs.Items[0].Name, certs.Items[0].Spec.APISpec.Certificate.SecretRef.Name, "registered from a copy of the Secret of the user")
			g, ok, _ := pod.state(gatewayID(ct, gateway))
			if assert.True(ct, ok) {
				pinned, ok := g.certificates[uuid.MustParse(string(certs.Items[0].UID))]
				if assert.True(ct, ok, "gw-admin must trust the certificate") {
					assert.Equal(ct, string(certPEM), pinned.Certificate)
				}
			}
			d := &appsv1.Deployment{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(manual), d)) {
				return
			}
			assert.Equal(ct, certs.Items[0].Name, certVolumeSecret(d), "the Deployment mounts the copy of the user's Secret")
			assert.Equal(ct, gwadminref.DataPlaneClientCAFingerprint(caSecret.Data["tls.crt"]), d.Spec.Template.Annotations[consts.GWAdminDataPlaneClientCAAnnotation])
		}, waitTime, tickTime)

		secrets := &corev1.SecretList{}
		require.NoError(t, cl.List(ctx, secrets, client.InNamespace(ns.Name), client.MatchingLabels{consts.SecretKEGDataPlaneCertificateLabel: "true"}))
		for i := range secrets.Items {
			require.False(t, metav1.IsControlledBy(&secrets.Items[i], manual), "no certificate is issued for a Manual one")
		}

		// The GWAdmin rotates its cp client CA, keeping the previous one
		// trusted while a data plane runs a certificate from it.
		previousCA := caSecret.Data["tls.crt"]
		rotatedPEM, rotatedKeyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("rotated cp client CA"), certificate.WithCATrue())
		caSecret.Data = map[string][]byte{"tls.crt": rotatedPEM, "tls.key": rotatedKeyPEM, gwadminref.PreviousCAKey: previousCA}
		require.NoError(t, cl.Update(ctx, caSecret))
		clientCAOf := func(ct *assert.CollectT) string {
			d := &appsv1.Deployment{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(manual), d)) {
				return ""
			}
			return d.Spec.Template.Annotations[consts.GWAdminDataPlaneClientCAAnnotation]
		}
		asserts.Never(t, func(ctx context.Context) bool {
			d := &appsv1.Deployment{}
			// A failed read tells nothing about the certificate it runs.
			return cl.Get(ctx, client.ObjectKeyFromObject(manual), d) == nil &&
				d.Spec.Template.Annotations[consts.GWAdminDataPlaneClientCAAnnotation] != gwadminref.DataPlaneClientCAFingerprint(previousCA)
		}, 2*time.Second, tickTime, "the data plane still runs a certificate from the previous CA: the rotation waits for it")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := manualCondition(ct, eventgatewayv1alpha1.CertificateProvisionedType); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
		}, waitTime, tickTime)

		rotated, err := tls.X509KeyPair(rotatedPEM, rotatedKeyPEM)
		require.NoError(t, err)
		previousCertPEM := certPEM
		certPEM, keyPEM = certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg-manual"), certificate.WithParent(rotated))
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(userSecret), userSecret))
		userSecret.Data = map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM}
		require.NoError(t, cl.Update(ctx, userSecret))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.Equal(ct, gwadminref.DataPlaneClientCAFingerprint(rotatedPEM), clientCAOf(ct),
				"once the user replaces it, the data plane runs a certificate from the rotated CA")
			// The pods don't roll out here: the certificate they run, replaced
			// in the same Secret, stays trusted.
			g, ok, _ := pod.state(gatewayID(ct, gateway))
			if !assert.True(ct, ok) {
				return
			}
			pinned := make([]string, 0, len(g.certificates))
			for _, c := range g.certificates {
				pinned = append(pinned, c.Certificate)
			}
			assert.Contains(ct, pinned, string(previousCertPEM), "gw-admin keeps trusting the certificate the pods run")
			assert.Contains(ct, pinned, string(certPEM), "gw-admin trusts the certificate the pods roll out onto")
		}, waitTime, tickTime)
	})

	t.Run("the Deployments of the data planes of a deleted gateway are no longer labelled for its GWAdmin", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, gateway))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(gateway), &eventgatewayv1alpha1.OnPremEventGateway{})))
			for _, name := range []string{egdp.Name, "keg-manual"} {
				d := &appsv1.Deployment{}
				if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: name}, d)) {
					continue
				}
				assert.NotContains(ct, d.Labels, consts.GWAdminDataPlaneNamespaceLabel, "the GWAdmin must not wait for %s", name)
				assert.NotContains(ct, d.Labels, consts.GWAdminDataPlaneNameLabel, "the GWAdmin must not wait for %s", name)
			}
		}, waitTime, tickTime)
	})

}

// TestOnPremEventGatewayCrossNamespace verifies that a gateway uses a GWAdmin
// of another namespace only while a KongReferenceGrant allows it, and that its
// data plane mounts a copy of the cp server CA bundle of the GWAdmin.
func TestOnPremEventGatewayCrossNamespace(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, gwAdminNS := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()
	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, kegCRDGroups)
	require.NoError(t, err)

	gwAdmin := newTestGWAdmin(t, cl, gwAdminNS.Name)
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig,
		gwAdmin.reconciler(ctx, mgr),
		&egdataplane.Reconciler{
			Client:                   cl,
			ClusterCASecretName:      "unused-cluster-ca",
			ClusterCASecretNamespace: "unused-cluster-ca-namespace",
			OperatorNamespace:        gwAdminNS.Name,
			CertTTL:                  consts.DefaultCertTTL,
			TypeConverter:            ssaProvider,
		},
		&crdschema.Reconciler{Client: cl, Provider: ssaProvider},
	)
	pod := gwAdmin.provision(ctx)
	gwAdmin.publishCP(ctx)
	gwAdmin.publishClientCA(ctx)

	teamNS := &corev1.Namespace{GenerateName: "team-"}
	require.NoError(t, cl.Create(ctx, teamNS))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), teamNS) })

	gateway := objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, teamNS.Name, `
metadata: {name: gateway}
spec:
  gwAdminRef: {name: `+testGWAdminName+`, namespace: `+gwAdminNS.Name+`}
`)
	require.NoError(t, cl.Create(ctx, gateway))
	expectGateway := func(t *testing.T, status metav1.ConditionStatus, message string) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			g := &eventgatewayv1alpha1.OnPremEventGateway{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gateway), g)) {
				return
			}
			cond := apimeta.FindStatusCondition(g.Status.Conditions, "Programmed")
			if status == metav1.ConditionFalse {
				// The GWAdminResolved condition tells why.
				cond = apimeta.FindStatusCondition(g.Status.Conditions, "GWAdminResolved")
			}
			if assert.NotNil(ct, cond) {
				assert.Equal(ct, status, cond.Status)
				assert.Contains(ct, cond.Message, message)
			}
		}, waitTime, tickTime)
	}

	t.Run("a gateway may not use a GWAdmin of another namespace without a grant", func(t *testing.T) {
		expectGateway(t, metav1.ConditionFalse, "no KongReferenceGrant in namespace "+gwAdminNS.Name+" allows the OnPremEventGateways of namespace "+teamNS.Name)
		_, ok, _ := pod.state(gatewayID(t, gateway))
		require.False(t, ok, "nothing must be pushed")
	})

	grant := &configurationv1alpha1.KongReferenceGrant{
		Name: "team", Namespace: gwAdminNS.Name,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "eventgateway.konghq.com", Kind: "OnPremEventGateway", Namespace: configurationv1alpha1.Namespace(teamNS.Name)}},
			To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "gateway-operator.konghq.com", Kind: "GWAdmin"}},
		},
	}
	t.Run("a grant lets the gateway use it", func(t *testing.T) {
		require.NoError(t, cl.Create(ctx, grant))
		expectGateway(t, metav1.ConditionTrue, "pushed to every gw-admin pod")
		_, ok, expected := pod.state(gatewayID(t, gateway))
		require.True(t, ok)
		require.Contains(t, expected, gatewayID(t, gateway))
	})

	t.Run("its data plane mounts a copy of the cp server CA bundle", func(t *testing.T) {
		egdp := objectFromYAML[eventgatewayv1alpha1.KegDataPlane](t, teamNS.Name, `
metadata: {name: keg}
spec:
  controlPlaneRef:
    type: onpremNamespacedRef
    onpremNamespacedRef: {name: gateway}
`)
		require.NoError(t, cl.Create(ctx, egdp))
		bundle := &corev1.Secret{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: gwAdminNS.Name, Name: testGWAdminCPBundle}, bundle))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			copied := &corev1.Secret{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: teamNS.Name, Name: "keg-gw-admin-cp-server-ca"}, copied)) {
				return
			}
			assert.Equal(ct, bundle.Data["ca.crt"], copied.Data["ca.crt"])
			d := &appsv1.Deployment{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(egdp), d)) {
				return
			}
			var mounted string
			for _, v := range d.Spec.Template.Spec.Volumes {
				if v.Name == "gw-admin-cp-server-ca" && v.Secret != nil {
					mounted = v.Secret.SecretName
				}
			}
			assert.Equal(ct, "keg-gw-admin-cp-server-ca", mounted)
			assert.Equal(ct, gwAdminNS.Name, d.Labels[consts.GWAdminDataPlaneNamespaceLabel])
		}, waitTime, tickTime)
	})

	t.Run("without the grant, the gateway is removed from gw-admin", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, grant))
		expectGateway(t, metav1.ConditionFalse, "no KongReferenceGrant")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			_, ok, expected := pod.state(gatewayID(ct, gateway))
			assert.False(ct, ok, "gw-admin must stop serving the gateway")
			assert.NotContains(ct, expected, gatewayID(ct, gateway))
		}, waitTime, tickTime)
	})
}
