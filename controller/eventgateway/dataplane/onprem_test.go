package dataplane

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/pki"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func TestHostAndPort(t *testing.T) {
	for _, tc := range []struct {
		url, want string
		wantErr   bool
	}{
		{url: "https://gw-admin-cp.kong.svc.cluster.local:8099", want: "gw-admin-cp.kong.svc.cluster.local:8099"},
		{url: "https://gw-admin-cp.kong.svc", want: "gw-admin-cp.kong.svc:443"},
		{url: "https://[fd00::1]:8099", want: "[fd00::1]:8099"},
		{url: "http://gw-admin-cp.kong.svc:8099", wantErr: true},
		{url: "https://:8099", wantErr: true},
		{url: "gw-admin-cp", wantErr: true},
	} {
		t.Run(tc.url, func(t *testing.T) {
			got, err := hostAndPort(tc.url)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestOnPremCertificateTTL(t *testing.T) {
	require.Equal(t, consts.GWAdminLeafTTL, onPremCertificateTTL(0, 0))
	require.Equal(t, consts.GWAdminLeafTTL, onPremCertificateTTL(consts.DefaultCertTTL, 0))
	require.Equal(t, consts.GWAdminLeafTTL, onPremCertificateTTL(consts.DefaultCertTTL, consts.GWAdminLeafTTL))
	require.Equal(t, 2*time.Hour, onPremCertificateTTL(2*time.Hour, 0))
	require.Equal(t, 720*time.Hour, onPremCertificateTTL(consts.DefaultCertTTL, 720*time.Hour), "a stricter --gwadmin-cert-ttl applies")
	require.Equal(t, 2000*time.Hour, onPremCertificateTTL(consts.DefaultCertTTL, 2000*time.Hour), "as does a longer one")
	require.Equal(t, 2*time.Hour, onPremCertificateTTL(2*time.Hour, 720*time.Hour), "a shorter --cert-ttl stays")
	require.Equal(t, minOnPremCertificateTTL, onPremCertificateTTL(time.Second, 0), "a validity too short to roll out onto is raised")
	require.Equal(t, minOnPremCertificateTTL, onPremCertificateTTL(consts.DefaultCertTTL, consts.GWAdminMinTTL), "as is a short --gwadmin-cert-ttl")
}

// The GWAdmin controller publishes the cp client CA in the operator namespace,
// whatever --cluster-ca-secret-namespace says.
func TestOnPremDependenciesReadTheOperatorNamespace(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	require.Equal(t, testOperatorNamespace, ca.Namespace)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca).Build()

	r := &Reconciler{ClusterCASecretNamespace: "cluster-ca-namespace", OperatorNamespace: testOperatorNamespace}
	kind, ok := r.config().ControlPlaneKindConfig(onPremEventGatewayKind.Kind)
	require.True(t, ok)
	_, err := kind.Dependencies(t.Context(), cl, gateway)
	require.NoError(t, err)
}

const testOperatorNamespace = "kong-system"

// onPremFixture returns a gateway, its GWAdmin with its cp endpoint published,
// its cp server CA bundle and its cp client CA, as the GWAdmin controller
// publishes them.
func onPremFixture(t *testing.T) (*eventgatewayv1alpha1.OnPremEventGateway, *operatorv1alpha1.GWAdmin, *corev1.Secret, *corev1.Secret) {
	t.Helper()
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{
		Name: "gateway", Namespace: "default", UID: "6a2f1c8e-5b3d-4e7a-9c1f-2d8b4a6e0f13",
		Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{GWAdminRef: eventgatewayv1alpha1.GWAdminRef{Name: "gw-admin"}},
	}
	gw := &operatorv1alpha1.GWAdmin{
		Name: "gw-admin", Namespace: "default", UID: "0b8e4f2a-7c1d-4a9e-8f3b-5d6c2e1a9b47",
		Status: operatorv1alpha1.GWAdminStatus{
			CP:  &operatorv1alpha1.GWAdminCPStatus{ServerURL: "https://gw-admin-cp.default.svc.cluster.local:8099"},
			TLS: &operatorv1alpha1.GWAdminTLSStatus{CPServerCABundleSecretName: "gw-admin-cp-server-ca-bundle"},
		},
	}
	serverCA, _ := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("cp server CA"), certificate.WithCATrue())
	bundle := &corev1.Secret{
		Name: "gw-admin-cp-server-ca-bundle", Namespace: "default",
		Data: map[string][]byte{"ca.crt": serverCA},
	}
	clientCA, clientCAKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("cp client CA"), certificate.WithCATrue())
	ca := &corev1.Secret{
		Name: gwadminref.CPClientCASecretName(gw), Namespace: testOperatorNamespace,
		Labels: map[string]string{consts.GWAdminUIDLabel: string(gw.UID)},
		Data:   map[string][]byte{"tls.crt": clientCA, "tls.key": clientCAKey},
	}
	return gateway, gw, bundle, ca
}

func TestOnPremDependencies(t *testing.T) {
	scheme := managerscheme.Get()
	dependencies := onPremDependenciesFunc(testOperatorNamespace, "")

	t.Run("resolves the cp endpoint and the trust material of the GWAdmin", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca).Build()
		got, err := dependencies(t.Context(), cl, gateway)
		require.NoError(t, err)
		deps, ok := got.(*onPremDependencies)
		require.True(t, ok)
		require.Equal(t, "gw-admin-cp.default.svc.cluster.local:8099", deps.host)
		require.Equal(t, bundle.Name, deps.serverBundle.Name)
		require.Equal(t, "cp client CA", deps.clientCACert.Subject.CommonName)

		env := map[string]string{}
		for _, e := range onPremEnvVars(deps) {
			env[e.Name] = e.Value
		}
		require.Equal(t, map[string]string{
			EnvKonnectRegion:           "testing",
			EnvKonnectDomain:           "gw-admin-cp.default.svc.cluster.local:8099",
			EnvKonnectGatewayClusterID: string(gateway.UID),
			EnvKonnectClientCertPath:   KonnectCertMountPath + "tls.crt",
			EnvKonnectClientKeyPath:    KonnectCertMountPath + "tls.key",
			EnvSSLCertFile:             "/etc/ssl/certs/ca-certificates.crt",
			EnvSSLCertDir:              "/var/gw-admin-cp-server-ca/",
		}, env)
	})

	for _, tc := range []struct {
		name    string
		mutate  func(gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) []client.Object
		message string
	}{
		{
			name: "no GWAdmin",
			mutate: func(_ *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) []client.Object {
				return []client.Object{bundle, ca}
			},
			message: "The GWAdmin default/gw-admin of the OnPremEventGateway does not exist",
		},
		{
			name: "cp endpoint not published",
			mutate: func(gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) []client.Object {
				gw.Status.CP = nil
				return []client.Object{gw, bundle, ca}
			},
			message: "hasn't published its cp endpoint yet",
		},
		{
			name: "no cp server CA bundle",
			mutate: func(gw *operatorv1alpha1.GWAdmin, _, ca *corev1.Secret) []client.Object {
				return []client.Object{gw, ca}
			},
			message: "The cp server CA bundle Secret default/gw-admin-cp-server-ca-bundle of the GWAdmin does not exist",
		},
		{
			name: "cp client CA not published",
			mutate: func(gw *operatorv1alpha1.GWAdmin, bundle, _ *corev1.Secret) []client.Object {
				return []client.Object{gw, bundle}
			},
			message: "cp-client-ca of the GWAdmin does not exist",
		},
		{
			name: "cp client CA of another GWAdmin",
			mutate: func(gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) []client.Object {
				ca.Labels[consts.GWAdminUIDLabel] = "another-uid"
				return []client.Object{gw, bundle, ca}
			},
			message: "doesn't belong to the GWAdmin default/gw-admin",
		},
		{
			name: "cp client CA without its key",
			mutate: func(gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) []client.Object {
				delete(ca.Data, "tls.key")
				return []client.Object{gw, bundle, ca}
			},
			message: "is invalid",
		},
	} {
		t.Run(tc.name+" is not ready", func(t *testing.T) {
			gateway, gw, bundle, ca := onPremFixture(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.mutate(gw, bundle, ca)...).Build()
			_, err := dependencies(t.Context(), cl, gateway)
			notReady, ok := errors.AsType[*shareddataplane.DependenciesNotReadyError](err)
			require.True(t, ok, "got %v", err)
			require.Contains(t, notReady.Message, tc.message)
		})
	}

	t.Run("a gateway being deleted is not ready", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		gateway.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca).Build()
		_, err := dependencies(t.Context(), cl, gateway)
		notReady, ok := errors.AsType[*shareddataplane.DependenciesNotReadyError](err)
		require.True(t, ok, "got %v", err)
		require.Equal(t, "The OnPremEventGateway default/gateway is being deleted", notReady.Message)
	})
}

// onPremDataPlane returns a data plane of the gateway of onPremFixture.
func onPremDataPlane() *eventgatewayv1alpha1.KegDataPlane {
	return &eventgatewayv1alpha1.KegDataPlane{
		APIVersion: eventgatewayv1alpha1.GroupVersion.String(), Kind: "KegDataPlane",
		Name: "keg", Namespace: "default", UID: "3c9d7e1f-2a4b-4c8d-9e6f-1b5a7c3d9e02",
		Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
			ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
				Type:                eventgatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef,
				OnPremNamespacedRef: &eventgatewayv1alpha1.OnPremNamespacedRef{Name: "gateway"},
			},
		},
	}
}

// resolveOnPremDependencies resolves the dependencies of the gateway.
func resolveOnPremDependencies(t *testing.T, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway) *onPremDependencies {
	t.Helper()
	got, err := onPremDependenciesFunc(testOperatorNamespace, "")(t.Context(), cl, gateway)
	require.NoError(t, err)
	return got.(*onPremDependencies)
}

// certificateSecretNames returns the names of the client certificate Secrets
// of the data plane.
func certificateSecretNames(t *testing.T, cl client.Client, r *Reconciler, egdp *eventgatewayv1alpha1.KegDataPlane) []string {
	t.Helper()
	owned, err := r.listOnPremCertificates(t.Context(), cl, egdp)
	require.NoError(t, err)
	names := make([]string, 0, len(owned))
	for _, s := range owned {
		names = append(names, s.Name)
	}
	return names
}

func certificateProvisioned(t *testing.T, egdp *eventgatewayv1alpha1.KegDataPlane) metav1.Condition {
	t.Helper()
	cond := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.CertificateProvisionedType))
	require.NotNil(t, cond)
	return *cond
}

func TestEnsureOnPremCertificate(t *testing.T) {
	scheme := managerscheme.Get()

	t.Run("a renewal issues a new Secret alongside the current one, removed once the rollout completed and no certificate references it", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, cl, gateway)
		r := &Reconciler{CertTTL: 2 * time.Hour}
		now := time.Now()

		res, first, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, now)
		require.NoError(t, err)
		require.Equal(t, op.Created, res)
		cert := pki.LeafCertificate(first.Data["tls.crt"], first.Data["tls.key"])
		require.NotNil(t, cert)
		require.NoError(t, cert.CheckSignatureFrom(deps.clientCACert))
		require.Equal(t, "keg.default", cert.Subject.CommonName)
		require.Equal(t, metav1.ConditionTrue, certificateProvisioned(t, egdp).Status)

		res, got, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, now.Add(time.Minute))
		require.NoError(t, err)
		require.Equal(t, op.Noop, res, "a fresh certificate isn't due: its renewal counts from its issue, not its backdated NotBefore")
		require.Equal(t, first.Name, got.Name)

		cp := shareddataplane.ResolvedControlPlane{Dependencies: deps}
		// Two thirds of the validity counted from the issue: 2h - 5m backdate.
		require.InDelta(t, (2*time.Hour-pki.ClockSkew).Seconds()*2/3, r.onPremCertificateRequeue(egdp, cp, got, now).Seconds(), 60)
		require.Zero(t, r.onPremCertificateRequeue(egdp, shareddataplane.ResolvedControlPlane{}, got, now), "only on-prem certificates are renewed")

		due := now.Add(100 * time.Minute)
		res, second, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, due)
		require.NoError(t, err)
		require.Equal(t, op.Created, res, "a certificate due for renewal is issued again")
		require.NotEqual(t, first.Name, second.Name)
		require.ElementsMatch(t, []string{first.Name, second.Name}, certificateSecretNames(t, cl, r, egdp), "the previous Secret is kept while the Deployment may use it")
		require.Equal(t, metav1.ConditionTrue, certificateProvisioned(t, egdp).Status)

		res, got, err = r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, due)
		require.NoError(t, err)
		require.Equal(t, op.Noop, res)
		require.Equal(t, second.Name, got.Name, "the newest certificate is the current one")

		// The certificate of the previous Secret is being deleted from
		// gw-admin: its Secret stays until it is gone.
		previous := buildOnPremCertificate(egdp, gateway, first.Name, shareddataplane.CertEntityName(egdp.Name, shareddataplane.CertificateChecksum(first)))
		previous.Finalizers = []string{"test/gw-admin-cleanup"}
		require.NoError(t, controllerutil.SetControllerReference(egdp, previous, scheme))
		require.NoError(t, cl.Create(t.Context(), previous))
		checksum := shareddataplane.CertificateChecksum(second)
		require.NoError(t, r.cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, cp, checksum))
		require.ElementsMatch(t, []string{first.Name, second.Name}, certificateSecretNames(t, cl, r, egdp), "a Secret is kept while a certificate references it")

		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(previous), previous))
		require.False(t, previous.DeletionTimestamp.IsZero(), "the stale certificate is deleted")
		previous.Finalizers = nil
		require.NoError(t, cl.Update(t.Context(), previous))
		require.NoError(t, r.cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, cp, checksum))
		require.Equal(t, []string{second.Name}, certificateSecretNames(t, cl, r, egdp))
	})

	t.Run("a duplicate issued while the cache lagged behind is deleted, the referenced certificate stays current", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		apiServer := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, apiServer, gateway)
		r := &Reconciler{}
		now := time.Now()
		res, issued, err := r.ensureOnPremCertificateAt(t.Context(), apiServer, egdp, deps, now)
		require.NoError(t, err)
		require.Equal(t, op.Created, res)
		registered := buildOnPremCertificate(egdp, gateway, issued.Name, shareddataplane.CertEntityName(egdp.Name, shareddataplane.CertificateChecksum(issued)))
		require.NoError(t, controllerutil.SetControllerReference(egdp, registered, scheme))
		require.NoError(t, apiServer.Create(t.Context(), registered))

		// The cache lags behind: it doesn't have the Secret just issued, and
		// another one is issued through it.
		cache := interceptor.NewClient(fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build(), interceptor.Funcs{
			Create: func(ctx context.Context, _ client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				return apiServer.Create(ctx, obj, opts...)
			},
		})
		res, duplicate, err := r.ensureOnPremCertificateAt(t.Context(), cache, egdp, deps, now.Add(time.Second))
		require.NoError(t, err)
		require.Equal(t, op.Created, res)
		require.ElementsMatch(t, []string{issued.Name, duplicate.Name}, certificateSecretNames(t, apiServer, r, egdp))

		res, got, err := r.ensureOnPremCertificateAt(t.Context(), apiServer, egdp, deps, now.Add(2*time.Second))
		require.NoError(t, err)
		require.Equal(t, op.Noop, res)
		require.Equal(t, issued.Name, got.Name, "the registered certificate stays current, though the duplicate is newer")
		require.Equal(t, []string{issued.Name}, certificateSecretNames(t, apiServer, r, egdp), "the duplicate is deleted")
	})

	t.Run("without a registered certificate, the most recently issued duplicate is kept", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, cl, gateway)
		r := &Reconciler{}
		now := time.Now()
		_, first, err := r.issueOnPremCertificate(t.Context(), cl, egdp, deps, time.Hour, now, "")
		require.NoError(t, err)
		_, second, err := r.issueOnPremCertificate(t.Context(), cl, egdp, deps, time.Hour, now.Add(time.Second), "")
		require.NoError(t, err)
		require.ElementsMatch(t, []string{first.Name, second.Name}, certificateSecretNames(t, cl, r, egdp))

		res, got, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, now.Add(time.Minute))
		require.NoError(t, err)
		require.Equal(t, op.Noop, res)
		require.Equal(t, second.Name, got.Name)
		require.Equal(t, []string{second.Name}, certificateSecretNames(t, cl, r, egdp))
	})

	t.Run("a duplicate can't be deleted", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, base, gateway)
		r := &Reconciler{}
		now := time.Now()
		for i := range 2 {
			_, _, err := r.issueOnPremCertificate(t.Context(), base, egdp, deps, time.Hour, now.Add(time.Duration(i)*time.Second), "")
			require.NoError(t, err)
		}
		cl := interceptor.NewClient(base, interceptor.Funcs{
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
				return errors.New("delete failed")
			},
		})
		_, secret, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, now.Add(time.Minute))
		require.ErrorContains(t, err, "delete failed")
		require.Nil(t, secret)
		cond := certificateProvisioned(t, egdp)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Contains(t, cond.Message, "failed to delete the duplicate mTLS certificate Secret")
	})

	t.Run("the certificates can't be listed", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, base, gateway)
		cl := interceptor.NewClient(base, interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return errors.New("list failed")
			},
		})
		_, _, err := (&Reconciler{}).ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, time.Now())
		require.ErrorContains(t, err, "list failed")
		require.Equal(t, metav1.ConditionFalse, certificateProvisioned(t, egdp).Status)
	})

	t.Run("a certificate can't be issued", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, base, gateway)
		cl := interceptor.NewClient(base, interceptor.Funcs{
			Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
				return errors.New("create failed")
			},
		})
		_, _, err := (&Reconciler{}).ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, time.Now())
		require.ErrorContains(t, err, "create failed")
		cond := certificateProvisioned(t, egdp)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Contains(t, cond.Message, "failed to provision mTLS certificate Secret from the cp client CA of the GWAdmin")
	})

	t.Run("a rotation of the CA issues a new Secret alongside the current one", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		r := &Reconciler{}
		_, first, err := r.ensureOnPremCertificate(t.Context(), cl, egdp, resolveOnPremDependencies(t, cl, gateway))
		require.NoError(t, err)

		rotated, rotatedKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("rotated cp client CA"), certificate.WithCATrue())
		ca.Data = map[string][]byte{"tls.crt": rotated, "tls.key": rotatedKey}
		require.NoError(t, cl.Update(t.Context(), ca))
		deps := resolveOnPremDependencies(t, cl, gateway)
		res, second, err := r.ensureOnPremCertificate(t.Context(), cl, egdp, deps)
		require.NoError(t, err)
		require.Equal(t, op.Created, res)
		cert := pki.LeafCertificate(second.Data["tls.crt"], second.Data["tls.key"])
		require.NotNil(t, cert)
		require.NoError(t, cert.CheckSignatureFrom(deps.clientCACert))
		require.ElementsMatch(t, []string{first.Name, second.Name}, certificateSecretNames(t, cl, r, egdp))
		cond := certificateProvisioned(t, egdp)
		require.Equal(t, metav1.ConditionTrue, cond.Status)
		require.Contains(t, cond.Message, "the cp client CA of the GWAdmin didn't issue the current one")

		// The previous certificate is still registered, and a duplicate of
		// the new one was issued: only the duplicate is deleted, the
		// previous one waits for the rollout.
		registered := buildOnPremCertificate(egdp, gateway, first.Name, shareddataplane.CertEntityName(egdp.Name, shareddataplane.CertificateChecksum(first)))
		require.NoError(t, controllerutil.SetControllerReference(egdp, registered, scheme))
		require.NoError(t, cl.Create(t.Context(), registered))
		current, err := parseOnPremCertificate(second)
		require.NoError(t, err)
		_, duplicate, err := r.issueOnPremCertificate(t.Context(), cl, egdp, deps, onPremCertificateTTL(0, 0), current.issuedAt.Add(-time.Second), "")
		require.NoError(t, err)
		require.ElementsMatch(t, []string{first.Name, second.Name, duplicate.Name}, certificateSecretNames(t, cl, r, egdp))
		res, got, err := r.ensureOnPremCertificate(t.Context(), cl, egdp, deps)
		require.NoError(t, err)
		require.Equal(t, op.Noop, res)
		require.Equal(t, second.Name, got.Name)
		require.ElementsMatch(t, []string{first.Name, second.Name}, certificateSecretNames(t, cl, r, egdp), "the Secret of the previous CA is left to the cleanup after the rollout")
	})

	t.Run("a renewal that wouldn't outlive the current certificate waits for the CA to be renewed", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		now := time.Now()
		// The CA expires in 2h: certificates are capped to it.
		caCert, caKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("cp client CA"), certificate.WithCATrue(),
			certificate.WithValidity(now.Add(-time.Hour), now.Add(2*time.Hour)))
		ca.Data = map[string][]byte{"tls.crt": caCert, "tls.key": caKey}
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, cl, gateway)
		r := &Reconciler{}
		res, first, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, now)
		require.NoError(t, err)
		require.Equal(t, op.Created, res)

		// Due, but a new certificate would expire with the CA as well.
		later := now.Add(90 * time.Minute)
		res, got, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, later)
		require.NoError(t, err)
		require.Equal(t, op.Noop, res, "no certificate is issued again")
		require.Equal(t, first.Name, got.Name)
		require.Equal(t, []string{first.Name}, certificateSecretNames(t, cl, r, egdp))
		cond := certificateProvisioned(t, egdp)
		require.Equal(t, metav1.ConditionTrue, cond.Status)
		require.Contains(t, cond.Message, "its renewal waits for the GWAdmin to renew its cp client CA")
		requeue := r.onPremCertificateRequeue(egdp, shareddataplane.ResolvedControlPlane{Dependencies: deps}, got, later)
		require.GreaterOrEqual(t, requeue, minDeferredRenewalRequeue, "no hot loop")
		require.LessOrEqual(t, requeue, maxDeferredRenewalRequeue)

		// Once it expired, nothing can be issued until the CA is renewed.
		res, _, err = r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, now.Add(2*time.Hour))
		require.NoError(t, err)
		require.Equal(t, op.Noop, res)
		require.Equal(t, metav1.ConditionFalse, certificateProvisioned(t, egdp).Status)
	})

	t.Run("no certificate is issued from a CA about to expire", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		now := time.Now()
		caCert, caKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("cp client CA"), certificate.WithCATrue(),
			certificate.WithValidity(now.Add(-time.Hour), now.Add(10*time.Minute)))
		ca.Data = map[string][]byte{"tls.crt": caCert, "tls.key": caKey}
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		r := &Reconciler{}
		_, secret, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, resolveOnPremDependencies(t, cl, gateway), now)
		requeue, ok := errors.AsType[*shareddataplane.RequeueAfterError](err)
		require.True(t, ok, "got %v", err)
		require.Equal(t, maxDeferredRenewalRequeue, requeue.After)
		require.Nil(t, secret)
		require.Empty(t, certificateSecretNames(t, cl, r, egdp))
		require.Equal(t, metav1.ConditionFalse, certificateProvisioned(t, egdp).Status)
	})
}

// The API of the GWAdmin allows a Manual cp client CA whose tls.crt holds a
// chain, the first certificate being the signer and the one gw-admin trusts.
func TestEnsureOnPremCertificateChainedCPClientCA(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	rootPEM, rootKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("root"), certificate.WithCATrue())
	intermediatePEM, intermediateKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("intermediate"),
		certificate.WithCATrue(), certificate.WithParent(keyPair(t, rootPEM, rootKey)))
	ca.Data[corev1.TLSCertKey] = slices.Concat(intermediatePEM, rootPEM)
	ca.Data[corev1.TLSPrivateKeyKey] = intermediateKey

	egdp := onPremDataPlane()
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca, egdp).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)
	require.Equal(t, "intermediate", deps.clientCACert.Subject.CommonName, "the first certificate is the signer")
	r := &Reconciler{}
	now := time.Now()

	res, secret, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, now)
	require.NoError(t, err)
	require.Equal(t, op.Created, res)
	cert := pki.LeafCertificate(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	require.NotNil(t, cert)
	require.NoError(t, cert.CheckSignatureFrom(deps.clientCACert), "issued by the first certificate of the chain")
	require.Equal(t, metav1.ConditionTrue, certificateProvisioned(t, egdp).Status)
	require.Len(t, pki.ParseCertificates(secret.Data["ca.crt"]), 1, "the signer only")
	require.True(t, pki.ParseCertificates(secret.Data["ca.crt"])[0].Equal(deps.clientCACert))

	var published corev1.Secret
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ca), &published))
	require.Equal(t, slices.Concat(intermediatePEM, rootPEM), published.Data[corev1.TLSCertKey], "the published Secret isn't changed")

	res, got, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, op.Noop, res, "recognized as issued from the CA: it isn't issued again on every reconcile")
	require.Equal(t, secret.Name, got.Name)
}

func TestCreateOnPremCertificate(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := onPremDataPlane()
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca, egdp).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)
	r := &Reconciler{SecretLabelSelector: "konghq.com/secret"}
	now := time.Now()

	secret, err := r.createOnPremCertificate(t.Context(), cl, egdp, deps, 2*time.Hour, now)
	require.NoError(t, err)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(secret), secret), "it is created")

	require.Equal(t, corev1.SecretTypeTLS, secret.Type)
	require.True(t, metav1.IsControlledBy(secret, egdp))
	for k, v := range r.onPremCertificateLabels(egdp) {
		require.Equal(t, v, secret.Labels[k], k)
	}
	require.Equal(t, []string{secret.Name}, certificateSecretNames(t, cl, r, egdp), "listed as a client certificate of the data plane")

	cert := pki.LeafCertificate(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	require.NotNil(t, cert)
	require.NoError(t, cert.CheckSignatureFrom(deps.clientCACert))
	require.Equal(t, onPremCertificateSubject(egdp), cert.Subject.CommonName)
	require.Equal(t, []string{onPremCertificateSubject(egdp)}, cert.DNSNames)
	require.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, cert.ExtKeyUsage)
	require.Equal(t, x509.ECDSA, cert.PublicKeyAlgorithm)
	require.True(t, pki.ParseCertificates(secret.Data[consts.CACRT])[0].Equal(deps.clientCACert))

	require.Equal(t, now.UTC().Format(time.RFC3339Nano), secret.Annotations[onPremCertificateIssuedAtAnnotation])
	require.Equal(t, cert.NotAfter.UTC().Format(time.RFC3339), secret.Annotations[consts.CertExpiresAtAnnotation])

	t.Run("the validity expected of a new certificate is the one it gets", func(t *testing.T) {
		require.InDelta(t, cert.NotAfter.Sub(now).Seconds(), freshOnPremCertificateValidity(deps.clientCACert, 2*time.Hour, now).Seconds(), 60)

		shortLived := now.Add(30 * time.Minute)
		caPEM, caKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("short lived"), certificate.WithCATrue(),
			certificate.WithValidity(now.Add(-time.Hour), shortLived))
		caCert := pki.ParseCertificates(caPEM)[0]
		secret, err := r.createOnPremCertificate(t.Context(), cl, egdp, &onPremDependencies{
			clientCA: &corev1.Secret{Data: map[string][]byte{"tls.crt": caPEM, "tls.key": caKey}}, clientCACert: caCert,
		}, 2*time.Hour, now)
		require.NoError(t, err)
		capped := pki.ParseCertificates(secret.Data[corev1.TLSCertKey])[0]
		require.True(t, capped.NotAfter.Equal(caCert.NotAfter), "capped at the expiry of the CA")
		require.InDelta(t, capped.NotAfter.Sub(now).Seconds(), freshOnPremCertificateValidity(caCert, 2*time.Hour, now).Seconds(), 1)
	})

	t.Run("a Secret that can't be created is an error", func(t *testing.T) {
		failing := interceptor.NewClient(cl, interceptor.Funcs{
			Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
				return errors.New("create failed")
			},
		})
		_, err := r.createOnPremCertificate(t.Context(), failing, egdp, deps, time.Hour, now)
		require.ErrorContains(t, err, "create failed")
	})
}

func TestOnPremCertificateRenewalTime(t *testing.T) {
	notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tlsCert := certificate.MustGenerateCert(certificate.WithValidity(notBefore, notBefore.Add(65*time.Minute)))
	certPEM, keyPEM := certificate.CertToPEMFormat(tlsCert)
	secret := &corev1.Secret{Data: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM}}

	c, err := parseOnPremCertificate(secret)
	require.NoError(t, err)
	require.Equal(t, notBefore.Add(pki.ClockSkew), c.issuedAt, "without the annotation, the backdate is accounted for")
	require.Equal(t, notBefore.Add(45*time.Minute), c.renewalTime())

	secret.Annotations = map[string]string{onPremCertificateIssuedAtAnnotation: notBefore.Add(20 * time.Minute).Format(time.RFC3339Nano)}
	c, err = parseOnPremCertificate(secret)
	require.NoError(t, err)
	require.Equal(t, notBefore.Add(50*time.Minute), c.renewalTime())
}

func TestServerBundleCopy(t *testing.T) {
	scheme := managerscheme.Get()
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := onPremDataPlane()
	egdp.Namespace = "data-planes"

	t.Run("refuses to take over a Secret the data plane doesn't control", func(t *testing.T) {
		foreign := &corev1.Secret{Name: serverBundleCopyName(egdp), Namespace: egdp.Namespace, Data: map[string][]byte{"ca.crt": []byte("mine")}}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, foreign).Build()
		r := &Reconciler{}
		_, err := r.ensureServerBundleCopy(t.Context(), cl, egdp, resolveOnPremDependencies(t, cl, gateway))
		require.ErrorContains(t, err, "isn't controlled by the KegDataPlane")
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(foreign), foreign))
		require.Equal(t, "mine", string(foreign.Data["ca.crt"]))
		require.Equal(t, metav1.ConditionFalse, certificateProvisioned(t, egdp).Status)
	})

	t.Run("is removed once no longer needed and no longer mounted", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		r := &Reconciler{}
		deps := resolveOnPremDependencies(t, cl, gateway)
		res, err := r.ensureServerBundleCopy(t.Context(), cl, egdp, deps)
		require.NoError(t, err)
		require.Equal(t, op.Created, res)
		key := client.ObjectKey{Namespace: egdp.Namespace, Name: serverBundleCopyName(egdp)}
		cp := shareddataplane.ResolvedControlPlane{Dependencies: deps}

		require.NoError(t, cleanupServerBundleCopy(t.Context(), cl, logr.Discard(), egdp, cp))
		require.NoError(t, cl.Get(t.Context(), key, &corev1.Secret{}), "the copy is kept while needed")

		// The GWAdmin is now in the namespace of the data plane, but the
		// Deployment still mounts the copy.
		d := &appsv1.Deployment{
			Name: egdp.Name, Namespace: egdp.Namespace,
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
				Name: gwAdminCPServerCAVolumeName, Secret: &corev1.SecretVolumeSource{SecretName: key.Name},
			}}}}},
		}
		require.NoError(t, cl.Create(t.Context(), d))
		sameNamespace := *deps
		sameNamespace.serverBundle = bundle.DeepCopy()
		sameNamespace.serverBundle.Namespace = egdp.Namespace
		cp = shareddataplane.ResolvedControlPlane{Dependencies: &sameNamespace}
		require.NoError(t, cleanupServerBundleCopy(t.Context(), cl, logr.Discard(), egdp, cp))
		require.NoError(t, cl.Get(t.Context(), key, &corev1.Secret{}), "the copy is kept while mounted")

		d.Spec.Template.Spec.Volumes = nil
		require.NoError(t, cl.Update(t.Context(), d))
		d.Status = appsv1.DeploymentStatus{ObservedGeneration: d.Generation, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}
		require.NoError(t, cl.Status().Update(t.Context(), d))
		require.NoError(t, cleanupServerBundleCopy(t.Context(), cl, logr.Discard(), egdp, cp))
		require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), key, &corev1.Secret{})), "the copy is removed")
	})

	t.Run("its name fits a long data plane name", func(t *testing.T) {
		long := onPremDataPlane()
		long.Name = strings.Repeat("a", 250)
		name := serverBundleCopyName(long)
		require.LessOrEqual(t, len(name), validation.DNS1123SubdomainMaxLength)
		require.Empty(t, validation.IsDNS1123Subdomain(name))
		other := onPremDataPlane()
		other.Name = strings.Repeat("a", 249) + "b"
		require.NotEqual(t, name, serverBundleCopyName(other))
		require.Equal(t, "keg-gw-admin-cp-server-ca", serverBundleCopyName(onPremDataPlane()))
	})
}

func TestOnPremAvailable(t *testing.T) {
	served := func(missing ...string) func(schema.GroupVersionResource) (bool, error) {
		return func(gvr schema.GroupVersionResource) (bool, error) {
			return !slices.Contains(missing, gvr.Resource), nil
		}
	}
	ok, err := OnPremAvailable(served())
	require.NoError(t, err)
	require.True(t, ok)
	for _, missing := range []string{"onpremeventgateways", "gwadmins", "kongreferencegrants"} {
		ok, err := OnPremAvailable(served(missing))
		require.NoError(t, err)
		require.False(t, ok, "without %s", missing)
	}
	_, err = OnPremAvailable(func(schema.GroupVersionResource) (bool, error) { return false, errors.New("discovery failed") })
	require.ErrorContains(t, err, "discovery failed")
}

func TestOnPremKindDisabled(t *testing.T) {
	r := &Reconciler{}
	onPrem, ok := r.config().ControlPlaneKindConfig(onPremEventGatewayKind.Kind)
	require.True(t, ok)
	require.Empty(t, onPrem.DisabledMessage)

	r.OnPremUnavailable = true
	cfg := r.config()
	onPrem, ok = cfg.ControlPlaneKindConfig(onPremEventGatewayKind.Kind)
	require.True(t, ok)
	require.Equal(t, onPremDisabledMessage, onPrem.DisabledMessage)
	konnect, ok := cfg.ControlPlaneKindConfig(konnectEventGatewayKind.Kind)
	require.True(t, ok)
	require.Empty(t, konnect.DisabledMessage, "the Konnect kind is always enabled")

	t.Run("a data plane of an OnPremEventGateway is reported, not retried", func(t *testing.T) {
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(egdp).WithStatusSubresource(egdp).Build()
		r.Client = cl
		r.TypeConverter = managedfields.NewDeducedTypeConverter()
		r.eventRecorder = events.NewFakeRecorder(10)
		current := &eventgatewayv1alpha1.KegDataPlane{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(egdp), current))
		current.TypeMeta = metav1.TypeMeta{APIVersion: eventgatewayv1alpha1.GroupVersion.String(), Kind: "KegDataPlane"}
		res, err := r.base().Reconcile(t.Context(), current)
		require.NoError(t, err)
		require.Zero(t, res)

		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(egdp), current))
		cond := apimeta.FindStatusCondition(current.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayResolvedType))
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayNotFoundReason), cond.Reason)
		require.Equal(t, onPremDisabledMessage, cond.Message)
		require.False(t, apimeta.IsStatusConditionTrue(current.Status.Conditions, string(eventgatewayv1alpha1.ReadyType)))
		require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKeyFromObject(egdp), &appsv1.Deployment{})), "no Deployment")
	})
}

func TestOnPremDependenciesFailures(t *testing.T) {
	scheme := managerscheme.Get()
	dependencies := onPremDependenciesFunc(testOperatorNamespace, "")
	errGet := errors.New("get failed")
	// failingGet fails to get the objects of the type of obj.
	failingGet := func(obj client.Object) interceptor.Funcs {
		return interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, got client.Object, opts ...client.GetOption) error {
				if reflect.TypeOf(got) == reflect.TypeOf(obj) && (key.Name == obj.GetName() || obj.GetName() == "") {
					return errGet
				}
				return c.Get(ctx, key, got, opts...)
			},
		}
	}

	_, fixtureGW, _, fixtureCA := onPremFixture(t)
	caName := fixtureCA.Name
	require.Equal(t, gwadminref.CPClientCASecretName(fixtureGW), caName)

	for _, tc := range []struct {
		name string
		// mutate changes the fixture, and returns the objects of the cluster
		// and its interceptors.
		mutate func(gateway *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs)
		// notReady is the message of the DependenciesNotReadyError, or ""
		// when another error is expected.
		notReady string
		err      error
	}{
		{
			name: "a GWAdmin in another namespace without a KongReferenceGrant",
			mutate: func(gateway *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs) {
				gateway.Spec.GWAdminRef.Namespace = new("other")
				gw.Namespace = "other"
				return []client.Object{gw, bundle, ca}, interceptor.Funcs{}
			},
			notReady: "The OnPremEventGateway may not use its GWAdmin: no KongReferenceGrant in namespace other",
		},
		{
			name: "the KongReferenceGrants can't be read",
			mutate: func(gateway *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs) {
				gateway.Spec.GWAdminRef.Namespace = new("other")
				return []client.Object{gw, bundle, ca}, interceptor.Funcs{
					List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return errGet },
				}
			},
			err: errGet,
		},
		{
			name: "the GWAdmin can't be read",
			mutate: func(_ *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs) {
				return []client.Object{gw, bundle, ca}, failingGet(&operatorv1alpha1.GWAdmin{})
			},
			err: errGet,
		},
		{
			name: "the GWAdmin is being deleted",
			mutate: func(_ *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs) {
				gw.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				gw.Finalizers = []string{"test/keep"}
				return []client.Object{gw, bundle, ca}, interceptor.Funcs{}
			},
			notReady: "The GWAdmin default/gw-admin of the OnPremEventGateway is being deleted",
		},
		{
			name: "an invalid cp endpoint",
			mutate: func(_ *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs) {
				gw.Status.CP.ServerURL = "http://gw-admin-cp.default.svc:8099"
				return []client.Object{gw, bundle, ca}, interceptor.Funcs{}
			},
			notReady: "The cp endpoint of the GWAdmin default/gw-admin is invalid",
		},
		{
			name: "the cp server CA bundle can't be read",
			mutate: func(_ *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs) {
				return []client.Object{gw, bundle, ca}, failingGet(bundle)
			},
			err: errGet,
		},
		{
			name: "a cp server CA bundle without ca.crt",
			mutate: func(_ *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs) {
				bundle.Data = map[string][]byte{"tls.crt": []byte("not a bundle")}
				return []client.Object{gw, bundle, ca}, interceptor.Funcs{}
			},
			notReady: "The cp server CA bundle Secret default/gw-admin-cp-server-ca-bundle of the GWAdmin holds no ca.crt",
		},
		{
			name: "the cp client CA can't be read",
			mutate: func(_ *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs) {
				return []client.Object{gw, bundle, ca}, failingGet(ca)
			},
			err: errGet,
		},
		{
			name: "a cp client CA without a certificate",
			mutate: func(_ *eventgatewayv1alpha1.OnPremEventGateway, gw *operatorv1alpha1.GWAdmin, bundle, ca *corev1.Secret) ([]client.Object, interceptor.Funcs) {
				ca.Data["tls.crt"] = []byte("not a certificate")
				return []client.Object{gw, bundle, ca}, interceptor.Funcs{}
			},
			notReady: "The cp client CA Secret kong-system/" + caName + " of the GWAdmin is invalid",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway, gw, bundle, ca := onPremFixture(t)
			objs, funcs := tc.mutate(gateway, gw, bundle, ca)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithInterceptorFuncs(funcs).Build()
			_, err := dependencies(t.Context(), cl, gateway)
			if tc.notReady != "" {
				notReady, ok := errors.AsType[*shareddataplane.DependenciesNotReadyError](err)
				require.True(t, ok, "got %v", err)
				require.Contains(t, notReady.Message, tc.notReady)
				return
			}
			require.ErrorIs(t, err, tc.err)
			_, ok := errors.AsType[*shareddataplane.DependenciesNotReadyError](err)
			require.False(t, ok, "a failure to read isn't reported as not ready: it is retried")
		})
	}

	t.Run("another kind of control plane", func(t *testing.T) {
		_, err := dependencies(t.Context(), fake.NewClientBuilder().WithScheme(scheme).Build(), &konnectv1alpha1.KonnectEventGateway{})
		require.ErrorContains(t, err, "unexpected control plane")
	})
}

func TestParseOnPremCertificate(t *testing.T) {
	notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := notBefore.Add(time.Hour)
	tlsCert := certificate.MustGenerateCert(certificate.WithValidity(notBefore, notAfter))
	certPEM, keyPEM := certificate.CertToPEMFormat(tlsCert)
	_, otherKeyPEM := certificate.MustGenerateCertPEMFormat()
	withIssuedAt := func(issuedAt string) *corev1.Secret {
		return &corev1.Secret{
			Annotations: map[string]string{onPremCertificateIssuedAtAnnotation: issuedAt},
			Data:        map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
		}
	}

	_, err := parseOnPremCertificate(&corev1.Secret{Data: map[string][]byte{"tls.crt": certPEM, "tls.key": otherKeyPEM}})
	require.ErrorContains(t, err, "no certificate forming a key pair with its key")

	for _, tc := range []struct {
		name     string
		issuedAt string
		want     time.Time
	}{
		{name: "the recorded issue", issuedAt: notBefore.Add(20 * time.Minute).Format(time.RFC3339Nano), want: notBefore.Add(20 * time.Minute)},
		{name: "an invalid annotation falls back to the NotBefore without the backdate", issuedAt: "yesterday", want: notBefore.Add(pki.ClockSkew)},
		{name: "an issue before the NotBefore is clamped to it", issuedAt: notBefore.Add(-time.Hour).Format(time.RFC3339Nano), want: notBefore},
		{name: "an issue after the NotAfter is clamped to it", issuedAt: notAfter.Add(time.Hour).Format(time.RFC3339Nano), want: notAfter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := parseOnPremCertificate(withIssuedAt(tc.issuedAt))
			require.NoError(t, err)
			require.True(t, tc.want.Equal(c.issuedAt), "got %s, want %s", c.issuedAt, tc.want)
		})
	}

	require.Equal(t, "gateway-operator.konghq.com/keg-dp-cert-issued-at", onPremCertificateIssuedAtAnnotation)
	require.Equal(t, "keg.default", onPremCertificateSubject(onPremDataPlane()))
}

func TestCustomizeDeployment(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)
	egdp := onPremDataPlane()

	t.Run("a data plane of another kind of control plane is left unchanged", func(t *testing.T) {
		d := &appsv1.Deployment{Labels: map[string]string{"a": "b"}}
		customizeDeployment(egdp, shareddataplane.ResolvedControlPlane{}, d)
		require.Equal(t, &appsv1.Deployment{Labels: map[string]string{"a": "b"}}, d)
	})

	t.Run("labels and annotations are set on copies", func(t *testing.T) {
		labels := map[string]string{"a": "b"}
		annotations := map[string]string{"c": "d"}
		d := &appsv1.Deployment{
			Labels: labels,
			Spec:   appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Annotations: annotations}},
		}
		customizeDeployment(egdp, shareddataplane.ResolvedControlPlane{Dependencies: deps}, d)
		require.Equal(t, map[string]string{
			"a":                                   "b",
			consts.GWAdminDataPlaneNamespaceLabel: "default",
			consts.GWAdminDataPlaneNameLabel:      "gw-admin",
		}, d.Labels)
		require.Equal(t, map[string]string{
			"c": "d",
			consts.GWAdminDataPlaneServerBundleAnnotation: gwadminref.DataPlaneServerBundleChecksum(bundle),
			consts.GWAdminDataPlaneClientCAAnnotation:     gwadminref.DataPlaneClientCAFingerprint(ca.Data["tls.crt"]),
		}, d.Spec.Template.Annotations)
		require.Equal(t, map[string]string{"a": "b"}, labels, "the labels it had are cloned")
		require.Equal(t, map[string]string{"c": "d"}, annotations, "the annotations it had are cloned")
	})

	t.Run("a Deployment without labels nor annotations", func(t *testing.T) {
		d := &appsv1.Deployment{}
		customizeDeployment(egdp, shareddataplane.ResolvedControlPlane{Dependencies: deps}, d)
		require.Len(t, d.Labels, 2)
		require.Len(t, d.Spec.Template.Annotations, 2)
	})
}

func TestOnPremVolume(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)

	for _, tc := range []struct {
		name, namespace, want string
	}{
		{name: "in the namespace of the GWAdmin, its Secret", namespace: "default", want: bundle.Name},
		{name: "in another namespace, its copy", namespace: "data-planes", want: "keg-gw-admin-cp-server-ca"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			egdp := onPremDataPlane()
			egdp.Namespace = tc.namespace
			require.Equal(t, tc.want, serverBundleSecretName(egdp, deps))
			volume, mount := onPremVolume(egdp, deps)
			require.Equal(t, corev1.Volume{
				Name: gwAdminCPServerCAVolumeName,
				Secret: &corev1.SecretVolumeSource{
					SecretName: tc.want,
					Items:      []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
				},
			}, volume)
			require.Equal(t, corev1.VolumeMount{Name: gwAdminCPServerCAVolumeName, MountPath: "/var/gw-admin-cp-server-ca/", ReadOnly: true}, mount)
		})
	}
}

func TestResolveCertificateSecretOnPrem(t *testing.T) {
	scheme := managerscheme.Get()

	t.Run("in the namespace of the GWAdmin, the certificate is issued from its cp client CA", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
		r := &Reconciler{}
		res, secret, err := r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.Equal(t, op.Created, res)
		require.NotNil(t, secret)
		require.Equal(t, []string{secret.Name}, certificateSecretNames(t, cl, r, egdp))
	})

	t.Run("in another namespace, the cp server CA bundle is copied first", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		egdp.Namespace = "data-planes"
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
		r := &Reconciler{}
		res, secret, err := r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.Equal(t, op.Created, res)
		require.Nil(t, secret, "the certificate waits for the next reconcile")
		require.Empty(t, certificateSecretNames(t, cl, r, egdp))

		res, secret, err = r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.Equal(t, op.Created, res)
		require.NotNil(t, secret)
	})

	t.Run("a copy that fails stops", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		egdp.Namespace = "data-planes"
		foreign := &corev1.Secret{Name: serverBundleCopyName(egdp), Namespace: egdp.Namespace}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, foreign).Build()
		cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
		_, secret, err := (&Reconciler{}).resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.ErrorContains(t, err, "isn't controlled by the KegDataPlane")
		require.Nil(t, secret)
	})
}

func TestServerBundleCopyLifecycle(t *testing.T) {
	scheme := managerscheme.Get()
	gateway, gw, bundle, ca := onPremFixture(t)
	newDataPlane := func() *eventgatewayv1alpha1.KegDataPlane {
		egdp := onPremDataPlane()
		egdp.Namespace = "data-planes"
		return egdp
	}

	t.Run("is owned by the data plane without blocking its deletion, and kept up to date", func(t *testing.T) {
		egdp := newDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, cl, gateway)
		r := &Reconciler{SecretLabelSelector: "konghq.com/secret"}
		res, err := r.ensureServerBundleCopy(t.Context(), cl, egdp, deps)
		require.NoError(t, err)
		require.Equal(t, op.Created, res)

		var copied corev1.Secret
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: egdp.Namespace, Name: serverBundleCopyName(egdp)}, &copied))
		require.True(t, metav1.IsControlledBy(&copied, egdp))
		require.Len(t, copied.OwnerReferences, 1)
		require.Nil(t, copied.OwnerReferences[0].BlockOwnerDeletion, "blocking the deletion of the owner needs the permission to update its finalizers")
		require.Equal(t, "KegDataPlane", copied.OwnerReferences[0].Kind)
		require.Equal(t, "true", copied.Labels["konghq.com/secret"])
		require.Equal(t, bundle.Data["ca.crt"], copied.Data["ca.crt"])

		res, err = r.ensureServerBundleCopy(t.Context(), cl, egdp, deps)
		require.NoError(t, err)
		require.Equal(t, op.Noop, res)

		rotated := *deps
		rotated.serverBundle = bundle.DeepCopy()
		rotated.serverBundle.Data = map[string][]byte{"ca.crt": []byte("rotated")}
		res, err = r.ensureServerBundleCopy(t.Context(), cl, egdp, &rotated)
		require.NoError(t, err)
		require.Equal(t, op.Updated, res)
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(&copied), &copied))
		require.Equal(t, "rotated", string(copied.Data["ca.crt"]))
		require.Len(t, copied.OwnerReferences, 1)
	})

	t.Run("is not needed in the namespace of the GWAdmin", func(t *testing.T) {
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		res, err := (&Reconciler{}).ensureServerBundleCopy(t.Context(), cl, egdp, resolveOnPremDependencies(t, cl, gateway))
		require.NoError(t, err)
		require.Equal(t, op.Noop, res)
	})

	for _, tc := range []struct {
		name, selector, message string
	}{
		{
			name:     "a Secret the cache doesn't see, with a label selector",
			selector: "konghq.com/secret",
			message:  "isn't visible to the operator (it must exist and carry the \"konghq.com/secret\" label to be visible to the operator), or it must be removed",
		},
		{
			name:    "a Secret the cache doesn't see",
			message: "isn't visible to the operator: it must be removed",
		},
	} {
		t.Run(tc.name+" is reported, not read", func(t *testing.T) {
			egdp := newDataPlane()
			invisible := &corev1.Secret{Name: serverBundleCopyName(egdp), Namespace: egdp.Namespace}
			apiServer := fake.NewClientBuilder().WithScheme(scheme).WithObjects(invisible).Build()
			cache := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).WithInterceptorFuncs(interceptor.Funcs{
				Create: func(ctx context.Context, _ client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					return apiServer.Create(ctx, obj, opts...)
				},
				// As the API server answers a JSON patch whose test fails.
				Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
					return apierrors.NewGenericServerResponse(http.StatusUnprocessableEntity, "patch", schema.GroupResource{Resource: "secrets"}, invisible.Name, "test failed", 0, false)
				},
			}).Build()
			r := &Reconciler{SecretLabelSelector: tc.selector}
			_, err := r.ensureServerBundleCopy(t.Context(), cache, egdp, resolveOnPremDependencies(t, cache, gateway))
			require.ErrorContains(t, err, tc.message)
			cond := certificateProvisioned(t, egdp)
			require.Equal(t, metav1.ConditionFalse, cond.Status)
			require.Equal(t, string(eventgatewayv1alpha1.UnableToProvisionReason), cond.Reason)
			require.Equal(t, "A Secret named data-planes/keg-gw-admin-cp-server-ca, where the cp server CA bundle of the GWAdmin is copied, already exists but "+tc.message, cond.Message)
		})
	}

	t.Run("a failure to read the copy is returned", func(t *testing.T) {
		egdp := newDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, cl, gateway)
		failing := interceptor.NewClient(cl, interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return errors.New("get failed")
			},
		})
		_, err := (&Reconciler{}).ensureServerBundleCopy(t.Context(), failing, egdp, deps)
		require.ErrorContains(t, err, "failed to copy the cp server CA bundle of the GWAdmin into data-planes/keg-gw-admin-cp-server-ca: get failed")
	})
}

// keyPair parses the certificate and key, in PEM.
func keyPair(t *testing.T, certPEM, keyPEM []byte) tls.Certificate {
	t.Helper()
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return pair
}

// serverOnlyCertificate returns a certificate issued by parent for server
// authentication only, and its key, in PEM.
func serverOnlyCertificate(t *testing.T, parent tls.Certificate) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	issuer, err := x509.ParseCertificate(parent.Certificate[0])
	require.NoError(t, err)
	now := time.Now()
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "server only"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(1, 0, 0),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, issuer, &key.PublicKey, parent.PrivateKey)
	require.NoError(t, err)
	return certificate.CertToPEMFormat(tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key})
}

// manualOnPremSecret returns the Secret of a Manual certificate, in the
// namespace, holding certPEM and keyPEM.
func manualOnPremSecret(namespace string, certPEM, keyPEM []byte) *corev1.Secret {
	return &corev1.Secret{
		Name: "user-cert", Namespace: namespace,
		Data: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
	}
}

// withManualCertificate sets a Manual certificate, in the Secret of
// manualOnPremSecret, on the data plane.
func withManualCertificate(egdp *eventgatewayv1alpha1.KegDataPlane) *eventgatewayv1alpha1.KegDataPlane {
	egdp.Spec.CertificateSecret = &eventgatewayv1alpha1.CertificateSecret{
		Provisioning: new(eventgatewayv1alpha1.ManualCertificateProvisioning),
		SecretRef:    &eventgatewayv1alpha1.SecretRef{Name: "user-cert"},
	}
	return egdp
}

func TestManualCertificateIssuer(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	published := keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])
	previousPEM, previousKeyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("previous cp client CA"), certificate.WithCATrue())
	previous := keyPair(t, previousPEM, previousKeyPEM)
	now := time.Now()
	expiredPEM, expiredKeyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("expired cp client CA"), certificate.WithCATrue(),
		certificate.WithValidity(now.AddDate(-1, 0, 0), now.Add(-time.Hour)))
	expired := keyPair(t, expiredPEM, expiredKeyPEM)
	ca.Data[gwadminref.PreviousCAKey] = slices.Concat(previousPEM, expiredPEM)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)
	trusted := deps.trustedClientCAs(now)
	require.Len(t, trusted, 2, "the expired previous CA isn't trusted anymore")

	intermediatePEM, intermediateKeyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("intermediate"), certificate.WithCATrue(), certificate.WithParent(published))
	intermediate := keyPair(t, intermediatePEM, intermediateKeyPEM)
	otherPEM, otherKeyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("another CA"), certificate.WithCATrue())
	issued := func(parent tls.Certificate) ([]byte, []byte) {
		return certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg.default"), certificate.WithParent(parent))
	}

	for _, tc := range []struct {
		name string
		cert func() ([]byte, []byte)
		// want is the CA that issued the certificate, nil when gw-admin
		// trusts none that did.
		want *x509.Certificate
	}{
		{
			name: "issued from the published CA",
			cert: func() ([]byte, []byte) { return issued(published) },
			want: deps.clientCACert,
		},
		{
			name: "issued from a previous CA",
			cert: func() ([]byte, []byte) { return issued(previous) },
			want: trusted[1],
		},
		{
			name: "issued through an intermediate tls.crt holds",
			cert: func() ([]byte, []byte) {
				certPEM, keyPEM := issued(intermediate)
				return slices.Concat(certPEM, intermediatePEM), keyPEM
			},
			want: deps.clientCACert,
		},
		{
			name: "issued through an intermediate tls.crt lacks",
			cert: func() ([]byte, []byte) { return issued(intermediate) },
		},
		{
			name: "issued from an expired previous CA",
			cert: func() ([]byte, []byte) {
				return certificate.MustGenerateCertPEMFormat(certificate.WithParent(expired),
					certificate.WithValidity(now.AddDate(-1, 0, 0), now.AddDate(1, 0, 0)))
			},
		},
		{
			name: "issued from another CA",
			cert: func() ([]byte, []byte) { return issued(keyPair(t, otherPEM, otherKeyPEM)) },
		},
		{
			name: "issued for server authentication only",
			cert: func() ([]byte, []byte) { return serverOnlyCertificate(t, published) },
		},
		{
			name: "valid in a couple of minutes, within the tolerated clock skew",
			cert: func() ([]byte, []byte) {
				return certificate.MustGenerateCertPEMFormat(certificate.WithParent(published),
					certificate.WithValidity(now.Add(2*time.Minute), now.AddDate(0, 1, 0)))
			},
			want: deps.clientCACert,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			certPEM, keyPEM := tc.cert()
			got := manualCertificateIssuer(manualOnPremSecret("default", certPEM, keyPEM), trusted, now)
			if tc.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.True(t, tc.want.Equal(got), "issued from %s, not %s", tc.want.Subject.CommonName, got.Subject.CommonName)
		})
	}

	t.Run("a Secret whose key doesn't match its certificate", func(t *testing.T) {
		certPEM, _ := issued(published)
		_, otherKey := issued(published)
		require.Nil(t, manualCertificateIssuer(manualOnPremSecret("default", certPEM, otherKey), trusted, now))
	})
}

func TestResolveManualCertificateOnPrem(t *testing.T) {
	scheme := managerscheme.Get()

	t.Run("a certificate issued from the cp client CA is used as-is, once the cp server CA bundle is copied", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := withManualCertificate(onPremDataPlane())
		egdp.Namespace = "data-planes"
		certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
			certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])))
		user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user).Build()
		deps := resolveOnPremDependencies(t, cl, gateway)
		cp := shareddataplane.ResolvedControlPlane{Dependencies: deps}
		r := &Reconciler{}

		res, secret, err := r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.Equal(t, op.Created, res)
		require.Nil(t, secret, "the certificate waits for the next reconcile")
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: egdp.Namespace, Name: serverBundleCopyName(egdp)}, &corev1.Secret{}))

		res, secret, err = r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.Equal(t, op.Created, res, "the certificate is copied")
		require.Nil(t, secret, "the certificate waits for the next reconcile")

		res, secret, err = r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.Equal(t, op.Noop, res)
		require.NotNil(t, secret)
		require.Equal(t, shareddataplane.CertEntityName(egdp.Name, shareddataplane.CertificateChecksum(user)), secret.Name, "its copy is used")
		require.Equal(t, certPEM, secret.Data["tls.crt"])
		require.Equal(t, keyPEM, secret.Data["tls.key"])
		require.True(t, deps.clientCACert.Equal(deps.manualCertIssuer))
		require.Empty(t, certificateSecretNames(t, cl, r, egdp), "no certificate is issued")
		require.Equal(t, metav1.ConditionTrue, certificateProvisioned(t, egdp).Status)
		require.Positive(t, r.onPremCertificateRequeue(egdp, cp, secret, time.Now()), "checked again, for the expiry of what it depends on")
	})

	t.Run("a certificate gw-admin wouldn't trust isn't used", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := withManualCertificate(onPremDataPlane())
		certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"))
		user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user).Build()
		deps := resolveOnPremDependencies(t, cl, gateway)

		res, secret, err := (&Reconciler{}).resolveCertificateSecret(t.Context(), cl, egdp,
			shareddataplane.ResolvedControlPlane{Dependencies: deps}, automaticFallbackUnexpected(t))
		require.NoError(t, err, "a change of the Secret or of the cp client CA reconciles the data plane again")
		require.Equal(t, op.Noop, res)
		require.Nil(t, secret)
		require.Nil(t, deps.manualCertIssuer)
		cond := certificateProvisioned(t, egdp)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, string(eventgatewayv1alpha1.CertificateSecretInvalidReason), cond.Reason)
		require.Equal(t, "The certificate of the Secret user-cert isn't issued, for client authentication, from the cp client CA of the GWAdmin default/gw-admin, "+
			"nor from one it still trusts: gw-admin would reject it", cond.Message)
	})

	t.Run("an invalid Secret is reported as for any Manual certificate", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := withManualCertificate(onPremDataPlane())
		user := manualOnPremSecret(egdp.Namespace, []byte("not a certificate"), nil)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user).Build()

		_, secret, err := (&Reconciler{}).resolveCertificateSecret(t.Context(), cl, egdp,
			shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.Nil(t, secret)
		require.Equal(t, string(eventgatewayv1alpha1.CertificateSecretInvalidReason), certificateProvisioned(t, egdp).Reason)
	})
}

// The user, or cert-manager, replaces a Manual certificate in place: each
// EventGatewayDataPlaneCertificate keeps registering its own from a copy, so
// that gw-admin keeps trusting the previous one until the pods rolled out.
func TestOnPremManualCertificateCopies(t *testing.T) {
	scheme := managerscheme.Get()
	issue := func(t *testing.T, ca *corev1.Secret) ([]byte, []byte) {
		t.Helper()
		return certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
			certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])))
	}
	// resolve reconciles until the Secret of the certificate is resolved.
	resolve := func(t *testing.T, cl client.Client, r *Reconciler, gateway *eventgatewayv1alpha1.OnPremEventGateway, egdp *eventgatewayv1alpha1.KegDataPlane) (*corev1.Secret, shareddataplane.ResolvedControlPlane) {
		t.Helper()
		cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
		for range 3 {
			res, secret, err := r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
			require.NoError(t, err)
			if res == op.Noop {
				return secret, cp
			}
			require.Nil(t, secret, "the certificate waits for the next reconcile")
		}
		require.Fail(t, "the certificate isn't resolved")
		return nil, cp
	}
	secretData := func(t *testing.T, cl client.Client, namespace, name string) map[string][]byte {
		t.Helper()
		var s corev1.Secret
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: name}, &s))
		return s.Data
	}

	t.Run("replaced in the same Secret, each certificate registers its own, from a copy with its key, until it is stale", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := withManualCertificate(onPremDataPlane())
		firstPEM, firstKey := issue(t, ca)
		user := manualOnPremSecret(egdp.Namespace, firstPEM, firstKey)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user).Build()
		r := &Reconciler{}

		secret, cp := resolve(t, cl, r, gateway, egdp)
		require.NotEqual(t, user.Name, secret.Name, "the Deployment doesn't mount the Secret of the user")
		firstChecksum := shareddataplane.CertificateChecksum(secret)
		require.Equal(t, shareddataplane.CertificateChecksum(user), firstChecksum, "the same certificate")
		first := buildEventGatewayDataPlaneCertificate(egdp, cp, secret.Name, firstChecksum)
		require.Equal(t, first.Name, first.Spec.APISpec.Certificate.SecretRef.Name, "registered from its copy, named like it")
		require.NoError(t, controllerutil.SetControllerReference(egdp, first, scheme))
		require.NoError(t, cl.Create(t.Context(), first))
		require.Equal(t, map[string][]byte{"tls.crt": firstPEM, "tls.key": firstKey}, secretData(t, cl, egdp.Namespace, first.Name))
		require.Equal(t, first.Name, secret.Name, "the Deployment mounts the copy it is registered from")

		secondPEM, secondKey := issue(t, ca)
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(user), user))
		user.Data = map[string][]byte{"tls.crt": secondPEM, "tls.key": secondKey}
		require.NoError(t, cl.Update(t.Context(), user))
		secret, cp = resolve(t, cl, r, gateway, egdp)
		secondChecksum := shareddataplane.CertificateChecksum(secret)
		second := buildEventGatewayDataPlaneCertificate(egdp, cp, secret.Name, secondChecksum)
		require.NotEqual(t, first.Name, second.Name)
		require.NoError(t, controllerutil.SetControllerReference(egdp, second, scheme))
		require.NoError(t, cl.Create(t.Context(), second))
		require.Equal(t, firstPEM, secretData(t, cl, egdp.Namespace, first.Name)["tls.crt"], "the previous certificate is still registered as it was")
		require.Equal(t, secondPEM, secretData(t, cl, egdp.Namespace, second.Name)["tls.crt"])

		// The rollout completed: the previous certificate is deleted first.
		first.Finalizers = []string{"test/gw-admin-cleanup"}
		require.NoError(t, cl.Update(t.Context(), first))
		require.NoError(t, r.cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, cp, secondChecksum))
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(first), first))
		require.False(t, first.DeletionTimestamp.IsZero())
		require.Equal(t, firstPEM, secretData(t, cl, egdp.Namespace, first.Name)["tls.crt"], "a copy is kept while a certificate references it")

		first.Finalizers = nil
		require.NoError(t, cl.Update(t.Context(), first))
		require.NoError(t, r.cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, cp, secondChecksum))
		require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKey{Namespace: egdp.Namespace, Name: first.Name}, &corev1.Secret{})), "gone with its certificate")
		require.Equal(t, secondPEM, secretData(t, cl, egdp.Namespace, second.Name)["tls.crt"], "the current copy stays")
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(user), &corev1.Secret{}), "the Secret of the user is never deleted")
	})

	t.Run("the current copy isn't deleted while its certificate isn't in the cache yet", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := withManualCertificate(onPremDataPlane())
		certPEM, keyPEM := issue(t, ca)
		user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user).Build()
		r := &Reconciler{}
		secret, cp := resolve(t, cl, r, gateway, egdp)
		checksum := shareddataplane.CertificateChecksum(secret)
		require.NoError(t, r.cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, cp, checksum))
		require.Equal(t, certPEM, secretData(t, cl, egdp.Namespace, shareddataplane.CertEntityName(egdp.Name, checksum))["tls.crt"])
	})

	t.Run("the copy of another data plane isn't deleted", func(t *testing.T) {
		egdp := withManualCertificate(onPremDataPlane())
		other := withManualCertificate(onPremDataPlane())
		other.Name, other.UID = "other", "7f3a2b1c-0d4e-4f5a-8b6c-9d0e1f2a3b4c"
		copied := &corev1.Secret{Name: "other-copy", Namespace: egdp.Namespace, Labels: map[string]string{secretCopyLabel: secretCopyManualCertificate}}
		require.NoError(t, controllerutil.SetControllerReference(other, copied, scheme))
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(egdp, other, copied).Build()
		require.NoError(t, cleanupStaleManualCertificateCopies(t.Context(), cl, logr.Discard(), egdp, map[string]bool{}, "current"))
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(copied), &corev1.Secret{}))
	})

	t.Run("a Secret of the same name the data plane doesn't control is reported, not taken over", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := withManualCertificate(onPremDataPlane())
		certPEM, keyPEM := issue(t, ca)
		user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
		name := shareddataplane.CertEntityName(egdp.Name, shareddataplane.CertificateChecksum(user))
		foreign := &corev1.Secret{Name: name, Namespace: egdp.Namespace, Data: map[string][]byte{"a": []byte("b")}}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user, foreign).Build()
		cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}

		_, secret, err := (&Reconciler{}).resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.ErrorContains(t, err, "isn't controlled by the KegDataPlane")
		require.Nil(t, secret)
		require.Equal(t, map[string][]byte{"a": []byte("b")}, secretData(t, cl, egdp.Namespace, name))
		require.Equal(t, metav1.ConditionFalse, certificateProvisioned(t, egdp).Status)
	})

	t.Run("an Automatic certificate references its Secret", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).Build()
		deps := resolveOnPremDependencies(t, cl, gateway)
		cert := buildEventGatewayDataPlaneCertificate(egdp, shareddataplane.ResolvedControlPlane{Dependencies: deps}, "issued", "checksum")
		require.Equal(t, "issued", cert.Spec.APISpec.Certificate.SecretRef.Name)
	})
}

func TestSecretCopiesAreMarked(t *testing.T) {
	scheme := managerscheme.Get()
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := withManualCertificate(onPremDataPlane())
	egdp.Namespace = "data-planes"
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
		certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])))
	user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)
	r := &Reconciler{}
	ctx := t.Context()

	_, err := r.ensureServerBundleCopy(ctx, cl, egdp, deps)
	require.NoError(t, err)
	_, manualCopy, err := r.ensureManualCertificateCopy(ctx, cl, egdp, user)
	require.NoError(t, err)
	require.Equal(t, corev1.SecretTypeTLS, manualCopy.Type)

	for _, tc := range []struct{ name, kind, source string }{
		{serverBundleCopyName(egdp), secretCopyServerBundle, "default/gw-admin-cp-server-ca-bundle"},
		{manualCopy.Name, secretCopyManualCertificate, "data-planes/user-cert"},
	} {
		var copied corev1.Secret
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: egdp.Namespace, Name: tc.name}, &copied))
		require.Equal(t, tc.kind, copied.Labels[secretCopyLabel])
		require.Equal(t, tc.source, copied.Annotations[secretCopiedFromAnnotation])
		require.Equal(t, consts.KEGDataPlaneManagedByLabelValue, copied.Labels[consts.GatewayOperatorManagedByLabel])
		require.True(t, metav1.IsControlledBy(&copied, egdp))
	}

	t.Run("a bundle copy is not a stale Manual certificate copy", func(t *testing.T) {
		require.NoError(t, cleanupStaleManualCertificateCopies(ctx, cl, logr.Discard(), egdp, map[string]bool{}, "current"))
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: egdp.Namespace, Name: serverBundleCopyName(egdp)}, &corev1.Secret{}))
		require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKey{Namespace: egdp.Namespace, Name: manualCopy.Name}, &corev1.Secret{})))
	})

	t.Run("a bundle copy made before these markers gets them, keeping the annotations it has", func(t *testing.T) {
		existing := onPremDataPlane()
		existing.Name, existing.Namespace, existing.UID = "older", "data-planes", "1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
		older := &corev1.Secret{Name: serverBundleCopyName(existing), Namespace: existing.Namespace,
			Annotations: map[string]string{"a": "b"}, Data: map[string][]byte{consts.CACRT: bundle.Data[consts.CACRT]}}
		require.NoError(t, controllerutil.SetControllerReference(existing, older, scheme))
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing, older).Build()
		res, err := r.ensureServerBundleCopy(ctx, cl, existing, deps)
		require.NoError(t, err)
		require.Equal(t, op.Updated, res)
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(older), older))
		require.Equal(t, secretCopyServerBundle, older.Labels[secretCopyLabel])
		require.Equal(t, "b", older.Annotations["a"])
		require.Equal(t, "default/gw-admin-cp-server-ca-bundle", older.Annotations[secretCopiedFromAnnotation])
	})
}

// A Manual certificate that stays untrusted stays False: when it became False
// doesn't change on every reconcile, which would write the status every time.
func TestManualCertificateConditionKeepsItsTransitionTime(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := withManualCertificate(onPremDataPlane())
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"))
	user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca, egdp, user).Build()
	cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
	r := &Reconciler{}
	resolve := func() {
		t.Helper()
		_, secret, err := r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.Nil(t, secret)
	}

	resolve()
	first := certificateProvisioned(t, egdp)
	require.Equal(t, metav1.ConditionFalse, first.Status)
	since := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
	apimeta.FindStatusCondition(egdp.Status.Conditions, first.Type).LastTransitionTime = since

	resolve()
	second := certificateProvisioned(t, egdp)
	require.Equal(t, metav1.ConditionFalse, second.Status)
	require.Equal(t, first.Message, second.Message)
	require.True(t, since.Equal(&second.LastTransitionTime), "it stayed False since %s, but reports a transition at %s", since, second.LastTransitionTime)

	t.Run("a certificate that was True moves to False at a new time", func(t *testing.T) {
		condition := apimeta.FindStatusCondition(egdp.Status.Conditions, first.Type)
		condition.Status, condition.LastTransitionTime = metav1.ConditionTrue, since
		resolve()
		got := certificateProvisioned(t, egdp)
		require.Equal(t, metav1.ConditionFalse, got.Status)
		require.False(t, since.Equal(&got.LastTransitionTime))
	})
}

// A Pod template overlay can't pin what the rotations of the GWAdmin rely on,
// as for the certificate checksum annotation (see shareddataplane.BuildDeployment).
func TestOverlayCantPinGWAdminTrustAnnotations(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)
	egdp := onPremDataPlane()
	overlay := map[string]string{
		"keep": "this",
		consts.GWAdminDataPlaneClientCAAnnotation:     "pinned-by-the-overlay",
		consts.GWAdminDataPlaneServerBundleAnnotation: "pinned-by-the-overlay",
	}
	egdp.Spec.Deployment = &eventgatewayv1alpha1.DeploymentOptions{
		PodTemplateSpec: &corev1.PodTemplateSpec{Annotations: maps.Clone(overlay)},
	}

	u, err := shareddataplane.BuildDeployment(logr.Discard(), managedfields.NewDeducedTypeConverter(), egdp,
		shareddataplane.ResolvedControlPlane{Dependencies: deps}, "image", "cert-secret", "", "checksum",
		config.Deployment, config.Certificate.ChecksumAnnotation)
	require.NoError(t, err)
	annotations, _, err := unstructured.NestedStringMap(u.Object, "spec", "template", "metadata", "annotations")
	require.NoError(t, err)
	require.Equal(t, gwadminref.DataPlaneClientCAFingerprint(ca.Data[corev1.TLSCertKey]), annotations[consts.GWAdminDataPlaneClientCAAnnotation])
	require.Equal(t, gwadminref.DataPlaneServerBundleChecksum(bundle), annotations[consts.GWAdminDataPlaneServerBundleAnnotation])
	require.Equal(t, "this", annotations["keep"], "the other annotations of the overlay are kept")
	require.Equal(t, overlay, egdp.Spec.Deployment.PodTemplateSpec.Annotations, "the KegDataPlane isn't changed")
}

// A certificate that can't be used, replacing the current one in the Secret of the
// user, leaves the copy the pods mount as it is.
func TestManualCertificateCopyOutlivesAnUnusableSecret(t *testing.T) {
	scheme := managerscheme.Get()
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := withManualCertificate(onPremDataPlane())
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
		certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])))
	user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user).Build()
	r := &Reconciler{}
	resolve := func(t *testing.T) (op.Result, *corev1.Secret) {
		t.Helper()
		cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
		res, secret, err := r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		return res, secret
	}
	res, _ := resolve(t)
	require.Equal(t, op.Created, res)
	_, current := resolve(t)
	require.NotNil(t, current)
	assertCurrent := func(t *testing.T) {
		t.Helper()
		var copied corev1.Secret
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(current), &copied))
		require.Equal(t, certPEM, copied.Data["tls.crt"])
		require.Equal(t, keyPEM, copied.Data["tls.key"])
	}

	t.Run("replaced by a certificate gw-admin wouldn't trust", func(t *testing.T) {
		untrustedPEM, untrustedKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"))
		user.Data = map[string][]byte{"tls.crt": untrustedPEM, "tls.key": untrustedKey}
		require.NoError(t, cl.Update(t.Context(), user))
		_, secret := resolve(t)
		require.Nil(t, secret, "the Deployment isn't built")
		assertCurrent(t)
	})
	t.Run("the Secret of the user is gone", func(t *testing.T) {
		require.NoError(t, cl.Delete(t.Context(), user))
		_, secret := resolve(t)
		require.Nil(t, secret, "the Deployment isn't built")
		assertCurrent(t)
	})
}

func TestSecretCopyFailureIsReported(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := withManualCertificate(onPremDataPlane())
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
		certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])))
	user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca, egdp, user).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
			return errors.New("denied by a webhook")
		},
	}).Build()
	cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}

	_, secret, err := (&Reconciler{}).resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
	require.ErrorContains(t, err, "denied by a webhook")
	require.Nil(t, secret)
	cond := certificateProvisioned(t, egdp)
	require.Equal(t, metav1.ConditionFalse, cond.Status, "Ready doesn't stay True while the certificate can't be registered")
	require.Equal(t, string(eventgatewayv1alpha1.UnableToProvisionReason), cond.Reason)
	require.Contains(t, cond.Message, "denied by a webhook")
}

func TestManualCertificateCantBeACertificateAuthority(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := withManualCertificate(onPremDataPlane())
	// The cp client CA itself, or a CA it issued: its key would be in the pods.
	user := manualOnPremSecret(egdp.Namespace, ca.Data["tls.crt"], ca.Data["tls.key"])
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca, egdp, user).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)

	_, secret, err := (&Reconciler{}).resolveCertificateSecret(t.Context(), cl, egdp,
		shareddataplane.ResolvedControlPlane{Dependencies: deps}, automaticFallbackUnexpected(t))
	require.NoError(t, err)
	require.Nil(t, secret)
	require.Nil(t, deps.manualCertIssuer)
	cond := certificateProvisioned(t, egdp)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(eventgatewayv1alpha1.CertificateSecretInvalidReason), cond.Reason)
	require.Contains(t, cond.Message, "is a certificate authority")
}

// A CA replaced by one with the same key under another subject verifies the
// signature, but not the chain, which a verifier like gw-admin's refuses.
func TestIssuedOnPremCertificatesRequireTheIssuerName(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	selfSigned := func(name string) (*x509.Certificate, []byte) {
		template := &x509.Certificate{
			SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: name},
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
			IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		require.NoError(t, err)
		parsed, err := x509.ParseCertificate(der)
		require.NoError(t, err)
		return parsed, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	original, originalPEM := selfSigned("original")
	renamed, _ := selfSigned("renamed")
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	egdp := onPremDataPlane()
	certPEM, keyPEM, err := pki.IssueCertificate(&corev1.Secret{Data: map[string][]byte{
		"tls.crt": originalPEM, "tls.key": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}),
	}}, pki.CertRequest{CommonName: onPremCertificateSubject(egdp), Usages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, TTL: time.Hour, Now: now})
	require.NoError(t, err)
	owned := []corev1.Secret{{Name: "issued", Data: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM}}}

	cert := pki.ParseCertificates(certPEM)[0]
	require.NoError(t, cert.CheckSignatureFrom(renamed), "the signature alone doesn't tell them apart")
	require.Len(t, issuedOnPremCertificates(owned, onPremCertificateSubject(egdp), original), 1)
	require.Empty(t, issuedOnPremCertificates(owned, onPremCertificateSubject(egdp), renamed), "it is issued again from the new CA")
}

func TestRenewalDeferred(t *testing.T) {
	now := time.Now()
	current := func(remaining time.Duration) onPremCertificate {
		return onPremCertificate{cert: &x509.Certificate{NotAfter: now.Add(remaining)}}
	}
	ca := func(remaining time.Duration) *x509.Certificate {
		return &x509.Certificate{NotAfter: now.Add(remaining)}
	}

	require.False(t, renewalDeferred(current(30*24*time.Hour), ca(365*24*time.Hour), time.Hour, now),
		"a lowered validity isn't a reason to wait: the CA doesn't cap it")
	require.False(t, renewalDeferred(current(time.Minute), ca(365*24*time.Hour), 90*24*time.Hour, now))
	require.True(t, renewalDeferred(current(time.Hour), ca(90*time.Minute), 90*24*time.Hour, now),
		"the CA caps the new one, which wouldn't double the remaining validity")
	require.False(t, renewalDeferred(current(10*time.Minute), ca(2*time.Hour), 90*24*time.Hour, now),
		"capped by the CA, but it does extend it")
	require.True(t, renewalDeferred(current(time.Minute), ca(5*time.Minute), 90*24*time.Hour, now),
		"too short to be worth issuing")
}

func TestManualCertificateNotRegisteredCondition(t *testing.T) {
	registered := metav1.Condition{
		Type: string(eventgatewayv1alpha1.GWAdminCertificateRegisteredType), Status: metav1.ConditionTrue,
		Reason: string(eventgatewayv1alpha1.GWAdminCertificateRegisteredReason), Message: "ensured and programmed on every gw-admin pod",
	}
	resolve := func(t *testing.T, secretData func(*corev1.Secret) *corev1.Secret, conditions ...metav1.Condition) *eventgatewayv1alpha1.KegDataPlane {
		t.Helper()
		gateway, gw, bundle, ca := onPremFixture(t)
		egdp := withManualCertificate(onPremDataPlane())
		egdp.Status.Conditions = conditions
		certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"))
		objects := []client.Object{gw, bundle, ca, egdp}
		if user := secretData(manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)); user != nil {
			objects = append(objects, user)
		}
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objects...).Build()
		_, secret, err := (&Reconciler{}).resolveCertificateSecret(t.Context(), cl, egdp,
			shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.Nil(t, secret)
		return egdp
	}
	untrusted := func(s *corev1.Secret) *corev1.Secret { return s }
	missing := func(*corev1.Secret) *corev1.Secret { return nil }

	for name, secretData := range map[string]func(*corev1.Secret) *corev1.Secret{"an untrusted certificate": untrusted, "a Secret gone": missing} {
		t.Run(name+" is no longer reported registered", func(t *testing.T) {
			egdp := resolve(t, secretData, registered)
			cond := apimeta.FindStatusCondition(egdp.Status.Conditions, registered.Type)
			require.NotNil(t, cond)
			require.Equal(t, metav1.ConditionUnknown, cond.Status)
			require.Contains(t, cond.Message, "no usable certificate")
		})
	}
	t.Run("a data plane that never registered one has no such condition", func(t *testing.T) {
		require.Nil(t, apimeta.FindStatusCondition(resolve(t, untrusted).Status.Conditions, registered.Type))
	})
}

func TestCertificateProvisionedKeepsItsTransitionWhenOnlyTheMessageChanges(t *testing.T) {
	egdp := onPremDataPlane()
	since := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
	egdp.Status.Conditions = []metav1.Condition{{
		Type: string(eventgatewayv1alpha1.CertificateProvisionedType), Status: metav1.ConditionTrue,
		Reason: string(eventgatewayv1alpha1.CertificateProvisionedReason), Message: "issued again", LastTransitionTime: since,
	}}
	setCertificateProvisioned(egdp, metav1.ConditionTrue, eventgatewayv1alpha1.CertificateProvisionedReason, "provisioned")
	cond := certificateProvisioned(t, egdp)
	require.Equal(t, "provisioned", cond.Message)
	require.True(t, since.Equal(&cond.LastTransitionTime))

	setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.UnableToProvisionReason, "failed")
	cond = certificateProvisioned(t, egdp)
	require.False(t, since.Equal(&cond.LastTransitionTime), "a change of status is a transition")
}

// The same as the GWAdmin controller does for the certificates the user provides:
// a warning in the condition, and an event once.
func TestManualCertificateExpiryIsReported(t *testing.T) {
	now := time.Now()
	scheme := managerscheme.Get()
	// resolved returns the data plane with a Manual certificate valid for
	// leafFor, of which leafLeft is left, from a cp client CA valid for caFor,
	// of which caLeft is left, reconciled twice.
	resolved := func(t *testing.T, caFor, caLeft, leafFor, leafLeft time.Duration) (*eventgatewayv1alpha1.KegDataPlane, *events.FakeRecorder, *corev1.Secret, shareddataplane.ResolvedControlPlane, *Reconciler) {
		t.Helper()
		gateway, gw, bundle, ca := onPremFixture(t)
		caPEM, caKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("user CA"), certificate.WithCATrue(),
			certificate.WithValidity(now.Add(caLeft-caFor), now.Add(caLeft)))
		ca.Data["tls.crt"], ca.Data["tls.key"] = caPEM, caKey
		egdp := withManualCertificate(onPremDataPlane())
		certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
			certificate.WithParent(keyPair(t, caPEM, caKey)), certificate.WithValidity(now.Add(leafLeft-leafFor), now.Add(leafLeft)))
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)).Build()
		cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
		recorder := events.NewFakeRecorder(10)
		r := &Reconciler{eventRecorder: recorder}
		var secret *corev1.Secret
		for range 3 {
			var err error
			_, secret, err = r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
			require.NoError(t, err)
		}
		return egdp, recorder, secret, cp, r
	}
	const day = 24 * time.Hour

	t.Run("nothing expires soon", func(t *testing.T) {
		egdp, recorder, secret, _, _ := resolved(t, 365*day, 300*day, 90*day, 80*day)
		require.NotNil(t, secret)
		cond := certificateProvisioned(t, egdp)
		require.Equal(t, metav1.ConditionTrue, cond.Status)
		require.NotContains(t, cond.Message, "expires at")
		require.Empty(t, recorder.Events)
	})

	t.Run("the certificate expires soon: reported in the condition, and once as an event", func(t *testing.T) {
		egdp, recorder, secret, _, _ := resolved(t, 365*day, 300*day, 90*day, 5*day)
		require.NotNil(t, secret, "it is still used")
		cond := certificateProvisioned(t, egdp)
		require.Equal(t, metav1.ConditionTrue, cond.Status)
		require.Contains(t, cond.Message, `client certificate Secret "user-cert" expires at`)
		require.Len(t, recorder.Events, 1, "not again on every reconcile")
		require.Contains(t, <-recorder.Events, "Warning CertificateExpiringSoon")
	})

	t.Run("the certificate authority it depends on expires soon", func(t *testing.T) {
		egdp, recorder, secret, _, _ := resolved(t, 365*day, 5*day, 90*day, 80*day)
		require.NotNil(t, secret)
		require.Contains(t, certificateProvisioned(t, egdp).Message, `certificate authority "user CA" issuing the client certificate Secret "user-cert" expires at`)
		require.Len(t, recorder.Events, 1)
	})

	t.Run("a certificate authority that expired before the certificate: rejected", func(t *testing.T) {
		egdp, _, secret, _, _ := resolved(t, 365*day, -time.Hour, 90*day, 80*day)
		require.Nil(t, secret)
		cond := certificateProvisioned(t, egdp)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, string(eventgatewayv1alpha1.CertificateSecretInvalidReason), cond.Reason)
	})

	t.Run("it is checked again when the certificate authority expires, before the certificate", func(t *testing.T) {
		egdp, _, secret, cp, r := resolved(t, 365*day, time.Hour, 90*day, 80*day)
		require.InDelta(t, time.Hour.Seconds(), r.onPremCertificateRequeue(egdp, cp, secret, now).Seconds(), 5)
		// Already past it: nothing left to wait for.
		require.Zero(t, r.onPremCertificateRequeue(egdp, cp, secret, now.Add(100*day)))
	})

	t.Run("it is checked again when it starts being reported, capped to a day", func(t *testing.T) {
		egdp, _, secret, cp, r := resolved(t, 6*time.Hour, 5*time.Hour, 90*day, 80*day)
		require.InDelta(t, (4 * time.Hour).Seconds(), r.onPremCertificateRequeue(egdp, cp, secret, now).Seconds(), 5, "a sixth of the 6h of the CA before it expires")
		egdp, _, secret, cp, r = resolved(t, 365*day, 300*day, 90*day, 80*day)
		require.Equal(t, 24*time.Hour, r.onPremCertificateRequeue(egdp, cp, secret, now))
	})
}

func TestSecretCopyTheCacheDoesntHoldYet(t *testing.T) {
	scheme := managerscheme.Get()
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := onPremDataPlane()
	egdp.Namespace = "data-planes"
	created := &corev1.Secret{Name: serverBundleCopyName(egdp), Namespace: egdp.Namespace}
	require.NoError(t, controllerutil.SetControllerReference(egdp, created, scheme))
	apiServer := fake.NewClientBuilder().WithScheme(scheme).WithObjects(created).Build()
	cache := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, _ client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			return apiServer.Create(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, _ client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			return apiServer.Patch(ctx, obj, patch, opts...)
		},
	}).Build()
	setCertificateProvisioned(egdp, metav1.ConditionTrue, eventgatewayv1alpha1.CertificateProvisionedReason, "provisioned")

	_, err := (&Reconciler{}).ensureServerBundleCopy(t.Context(), cache, egdp, resolveOnPremDependencies(t, cache, gateway))
	requeue, ok := errors.AsType[*shareddataplane.RequeueAfterError](err)
	require.True(t, ok, "looked at again, not retried with backoff: %v", err)
	require.Equal(t, secretCopyCacheRequeue, requeue.After)
	require.Equal(t, metav1.ConditionTrue, certificateProvisioned(t, egdp).Status, "nothing is wrong")
}

func TestManualSecretCantBeACopy(t *testing.T) {
	for _, kind := range []string{secretCopyManualCertificate, secretCopyServerBundle} {
		t.Run(kind, func(t *testing.T) {
			gateway, gw, bundle, ca := onPremFixture(t)
			egdp := withManualCertificate(onPremDataPlane())
			certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
				certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])))
			user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
			user.Labels = map[string]string{secretCopyLabel: kind}
			cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca, egdp, user).Build()

			_, secret, err := (&Reconciler{}).resolveCertificateSecret(t.Context(), cl, egdp,
				shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}, automaticFallbackUnexpected(t))
			require.NoError(t, err)
			require.Nil(t, secret)
			cond := certificateProvisioned(t, egdp)
			require.Equal(t, metav1.ConditionFalse, cond.Status)
			require.Equal(t, string(eventgatewayv1alpha1.CertificateSecretRefOperatorManagedReason), cond.Reason)

			// A data plane of Konnect is refused too: a copy is deleted all the same.
			konnect := withManualCertificate(newReconcileEGDP())
			konnect.Namespace = user.Namespace
			_, secret, err = (&Reconciler{}).resolveCertificateSecret(t.Context(), cl, konnect, shareddataplane.ResolvedControlPlane{}, automaticFallbackUnexpected(t))
			require.NoError(t, err)
			require.Nil(t, secret)
		})
	}
}

func TestManualCertificateExpiryOnlyLooksAtTheVerifiedChain(t *testing.T) {
	now := time.Now()
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := withManualCertificate(onPremDataPlane())
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
		certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])))
	expired, _ := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("old cross-sign"), certificate.WithCATrue(),
		certificate.WithValidity(now.Add(-48*time.Hour), now.Add(-time.Hour)))
	user := manualOnPremSecret(egdp.Namespace, slices.Concat(certPEM, expired, ca.Data["tls.crt"]), keyPEM)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca, egdp, user).Build()
	cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
	recorder := events.NewFakeRecorder(10)
	r := &Reconciler{eventRecorder: recorder}
	var secret *corev1.Secret
	for range 3 {
		var err error
		_, secret, err = r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
	}
	require.NotNil(t, secret)
	require.NotContains(t, certificateProvisioned(t, egdp).Message, "old cross-sign", "gw-admin doesn't depend on it")
	require.Empty(t, recorder.Events)
}

func TestManualCertificateExpiryEventSurvivesAnOverwrittenCondition(t *testing.T) {
	now := time.Now()
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := withManualCertificate(onPremDataPlane())
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
		certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])), certificate.WithValidity(now.Add(-85*24*time.Hour), now.Add(5*24*time.Hour)))
	user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca, egdp, user).Build()
	cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
	recorder := events.NewFakeRecorder(10)
	r := &Reconciler{eventRecorder: recorder}
	resolve := func() {
		t.Helper()
		_, _, err := r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
	}
	resolve()
	resolve()
	require.Len(t, recorder.Events, 1)

	setCertificateProvisioned(egdp, metav1.ConditionUnknown, "NotReady", "The GWAdmin is down")
	resolve()
	require.Len(t, recorder.Events, 1, "the same warning isn't reported again")
	require.Contains(t, certificateProvisioned(t, egdp).Message, "expires at")
}

func TestManualCertificateReadErrorKeepsItRegistered(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := withManualCertificate(onPremDataPlane())
	registered := metav1.Condition{
		Type: string(eventgatewayv1alpha1.GWAdminCertificateRegisteredType), Status: metav1.ConditionTrue,
		Reason: string(eventgatewayv1alpha1.GWAdminCertificateRegisteredReason), LastTransitionTime: metav1.Now(),
	}
	egdp.Status.Conditions = []metav1.Condition{registered}
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca, egdp).Build()
	cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
	failing := interceptor.NewClient(cl, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if key.Name == "user-cert" {
				return errors.New("read failed")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	_, secret, err := (&Reconciler{}).resolveCertificateSecret(t.Context(), failing, egdp, cp, automaticFallbackUnexpected(t))
	require.ErrorContains(t, err, "read failed")
	require.Nil(t, secret)
	require.Equal(t, metav1.ConditionTrue, apimeta.FindStatusCondition(egdp.Status.Conditions, registered.Type).Status,
		"a read error says nothing about the certificate")
}

// The copy of the Manual certificate doesn't outlive the switch to Automatic.
func TestSwitchFromManualToAutomaticRemovesTheCopy(t *testing.T) {
	scheme := managerscheme.Get()
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := withManualCertificate(onPremDataPlane())
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
		certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])))
	user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user).Build()
	cp := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
	r := &Reconciler{}
	for range 2 {
		_, _, err := r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
		require.NoError(t, err)
	}
	copyName := shareddataplane.CertEntityName(egdp.Name, shareddataplane.CertificateChecksum(user))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: egdp.Namespace, Name: copyName}, &corev1.Secret{}))

	egdp.Spec.CertificateSecret = nil
	cp = shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
	_, issued, err := r.resolveCertificateSecret(t.Context(), cl, egdp, cp, automaticFallbackUnexpected(t))
	require.NoError(t, err)
	require.NotEqual(t, copyName, issued.Name)
	require.NoError(t, r.cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, cp, shareddataplane.CertificateChecksum(issued)))
	require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKey{Namespace: egdp.Namespace, Name: copyName}, &corev1.Secret{})), "with its key")
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(user), &corev1.Secret{}), "the Secret of the user stays")
}

func TestCustomizeDeploymentManualCertificate(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)
	previous := certificate.MustGenerateCert(certificate.WithCommonName("previous cp client CA"), certificate.WithCATrue())
	issuer, err := x509.ParseCertificate(previous.Certificate[0])
	require.NoError(t, err)
	deps.manualCertIssuer = issuer

	d := &appsv1.Deployment{}
	customizeDeployment(withManualCertificate(onPremDataPlane()), shareddataplane.ResolvedControlPlane{Dependencies: deps}, d)
	require.Equal(t, pki.Fingerprint(issuer), d.Spec.Template.Annotations[consts.GWAdminDataPlaneClientCAAnnotation],
		"the GWAdmin keeps trusting the CA that issued the Manual certificate until the data plane runs another one")
}

func TestCleanupStaleCertificatesAfterSwitchToManual(t *testing.T) {
	scheme := managerscheme.Get()
	gateway, gw, bundle, ca := onPremFixture(t)
	egdp := onPremDataPlane()
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("keg"),
		certificate.WithParent(keyPair(t, ca.Data["tls.crt"], ca.Data["tls.key"])))
	user := manualOnPremSecret(egdp.Namespace, certPEM, keyPEM)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, bundle, ca, egdp, user).Build()
	deps := resolveOnPremDependencies(t, cl, gateway)
	cp := shareddataplane.ResolvedControlPlane{Dependencies: deps}
	r := &Reconciler{}

	_, automatic, err := r.ensureOnPremCertificateAt(t.Context(), cl, egdp, deps, time.Now())
	require.NoError(t, err)
	// The certificate of the Automatic Secret is being deleted from gw-admin.
	registered := buildOnPremCertificate(egdp, gateway, automatic.Name, shareddataplane.CertEntityName(egdp.Name, shareddataplane.CertificateChecksum(automatic)))
	registered.Finalizers = []string{"test/gw-admin-cleanup"}
	require.NoError(t, controllerutil.SetControllerReference(egdp, registered, scheme))
	require.NoError(t, cl.Create(t.Context(), registered))

	withManualCertificate(egdp)
	checksum := shareddataplane.CertificateChecksum(user)
	require.NoError(t, r.cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, cp, checksum))
	require.Equal(t, []string{automatic.Name}, certificateSecretNames(t, cl, r, egdp), "the Automatic Secret is kept while a certificate references it")

	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(registered), registered))
	registered.Finalizers = nil
	require.NoError(t, cl.Update(t.Context(), registered))
	require.NoError(t, r.cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, cp, checksum))
	require.Empty(t, certificateSecretNames(t, cl, r, egdp))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(user), &corev1.Secret{}), "the user's Secret is never touched")
}

func TestReleaseDeploymentFromGWAdmin(t *testing.T) {
	scheme := managerscheme.Get()
	gwAdminLabels := map[string]string{
		"app":                                 "keg",
		consts.GWAdminDataPlaneNamespaceLabel: "default",
		consts.GWAdminDataPlaneNameLabel:      "gw-admin",
	}
	// labelledDeployment returns the Deployment of the data plane, labelled
	// for the GWAdmin, controlled by owner when set.
	labelledDeployment := func(t *testing.T, egdp, owner *eventgatewayv1alpha1.KegDataPlane) *appsv1.Deployment {
		t.Helper()
		d := &appsv1.Deployment{Name: egdp.Name, Namespace: egdp.Namespace, Labels: maps.Clone(gwAdminLabels)}
		d.Spec.Template.Annotations = map[string]string{consts.GWAdminDataPlaneClientCAAnnotation: "fingerprint"}
		if owner != nil {
			require.NoError(t, controllerutil.SetControllerReference(owner, d, scheme))
		}
		return d
	}
	reconcileDataPlane := func(t *testing.T, cl client.Client, egdp *eventgatewayv1alpha1.KegDataPlane) {
		t.Helper()
		r := &Reconciler{Client: cl, TypeConverter: managedfields.NewDeducedTypeConverter(), eventRecorder: events.NewFakeRecorder(10)}
		current := &eventgatewayv1alpha1.KegDataPlane{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(egdp), current))
		current.TypeMeta = metav1.TypeMeta{APIVersion: eventgatewayv1alpha1.GroupVersion.String(), Kind: "KegDataPlane"}
		_, err := r.base().Reconcile(t.Context(), current)
		require.NoError(t, err)
	}
	labelsOf := func(t *testing.T, cl client.Client, egdp *eventgatewayv1alpha1.KegDataPlane) (map[string]string, map[string]string) {
		t.Helper()
		var d appsv1.Deployment
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(egdp), &d))
		return d.Labels, d.Spec.Template.Annotations
	}

	t.Run("once the gateway is gone, the Deployment is no longer labelled for the GWAdmin, and its pods aren't rolled", func(t *testing.T) {
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(egdp, labelledDeployment(t, egdp, egdp)).WithStatusSubresource(egdp).Build()
		reconcileDataPlane(t, cl, egdp)
		labels, annotations := labelsOf(t, cl, egdp)
		require.Equal(t, map[string]string{"app": "keg"}, labels)
		require.Equal(t, "fingerprint", annotations[consts.GWAdminDataPlaneClientCAAnnotation], "the Pod template is left as it is")
	})

	t.Run("a gateway being deleted releases it too", func(t *testing.T) {
		gateway, gw, bundle, ca := onPremFixture(t)
		gateway.Finalizers = []string{"test/cleanup"}
		gateway.DeletionTimestamp = new(metav1.Now())
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gateway, gw, bundle, ca, egdp, labelledDeployment(t, egdp, egdp)).WithStatusSubresource(egdp).Build()
		reconcileDataPlane(t, cl, egdp)
		labels, _ := labelsOf(t, cl, egdp)
		require.Equal(t, map[string]string{"app": "keg"}, labels)
	})

	t.Run("a GWAdmin not ready keeps it labelled: the data plane may come back", func(t *testing.T) {
		gateway, _, _, _ := onPremFixture(t)
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gateway, egdp, labelledDeployment(t, egdp, egdp)).WithStatusSubresource(egdp).Build()
		reconcileDataPlane(t, cl, egdp)
		labels, _ := labelsOf(t, cl, egdp)
		require.Equal(t, gwAdminLabels, labels)
	})

	t.Run("a Deployment the data plane doesn't control, or none, is left alone", func(t *testing.T) {
		egdp := onPremDataPlane()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(labelledDeployment(t, egdp, nil)).Build()
		require.NoError(t, releaseDeploymentFromGWAdmin(t.Context(), cl, egdp))
		labels, _ := labelsOf(t, cl, egdp)
		require.Equal(t, gwAdminLabels, labels)

		require.NoError(t, releaseDeploymentFromGWAdmin(t.Context(), fake.NewClientBuilder().WithScheme(scheme).Build(), egdp))
	})
}
