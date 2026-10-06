package konnect

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

const (
	dpCertTestNamespace = "default"
	dpCertTestExtName   = "extension"
)

func dpCertTestProgrammedStatus(id string) configurationv1alpha1.KongDataPlaneClientCertificateStatus {
	return configurationv1alpha1.KongDataPlaneClientCertificateStatus{
		Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
			ID:             id,
			ControlPlaneID: "control-plane-id",
		},
		Conditions: []metav1.Condition{{Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: metav1.ConditionTrue}},
	}
}

// dpCertTestObject returns a programmed KongDataPlaneClientCertificate owned by the test KonnectExtension.
func dpCertTestObject(name, cert string) *configurationv1alpha1.KongDataPlaneClientCertificate {
	return &configurationv1alpha1.KongDataPlaneClientCertificate{
		Name: name, Namespace: dpCertTestNamespace,
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: konnectv1alpha2.GroupVersion.String(), Kind: "KonnectExtension", Name: dpCertTestExtName, UID: types.UID(dpCertTestExtName),
		}},
		Spec:   configurationv1alpha1.KongDataPlaneClientCertificateSpec{KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{Cert: cert}},
		Status: dpCertTestProgrammedStatus("konnect-id-" + name),
	}
}

// dpCertTestReconciler returns a reconciler for a KonnectExtension whose manually provisioned
// certificate Secret holds secretCert, together with the given extra objects.
func dpCertTestReconciler(t *testing.T, secretCert string, extra ...client.Object) (*KonnectExtensionReconciler, *konnectv1alpha2.KonnectExtension) {
	t.Helper()
	secret := &corev1.Secret{
		Name:      "certificate",
		Namespace: dpCertTestNamespace,
		Labels:    map[string]string{SecretKonnectDataPlaneCertificateLabel: "true"},
		Data:      map[string][]byte{consts.TLSCRT: []byte(secretCert)},
	}
	ext := &konnectv1alpha2.KonnectExtension{
		Name: dpCertTestExtName, Namespace: dpCertTestNamespace, UID: types.UID(dpCertTestExtName),
		Spec: konnectv1alpha2.KonnectExtensionSpec{
			ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
				CertificateSecret: konnectv1alpha2.CertificateSecret{
					Provisioning:         new(konnectv1alpha2.ManualSecretProvisioning),
					CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: secret.Name},
				},
			},
			Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
				ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
					Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "control-plane"},
					},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
		WithObjects(append(extra,
			secret, ext,
			&konnectv1alpha2.KonnectGatewayControlPlane{
				Name: "control-plane", Namespace: dpCertTestNamespace,
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{Name: "auth"},
					},
				},
				Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
					KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "control-plane-id"},
					Conditions:          []metav1.Condition{{Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: metav1.ConditionTrue}},
				},
			},
			&konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "auth", Namespace: dpCertTestNamespace,
				Status: konnectv1alpha1.KonnectAPIAuthConfigurationStatus{
					Conditions: []metav1.Condition{{
						Type:   konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
						Status: metav1.ConditionTrue,
						Reason: konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
					}},
				},
			})...).
		WithStatusSubresource(ext, &configurationv1alpha1.KongDataPlaneClientCertificate{}).
		WithIndex(&operatorv1beta1.DataPlane{}, index.KonnectExtensionIndex, func(client.Object) []string { return nil }).
		WithIndex(&gwtypes.ControlPlane{}, index.KonnectExtensionIndex, func(client.Object) []string { return nil }).
		WithIndex(&configurationv1alpha1.KongDataPlaneClientCertificate{}, index.IndexFieldKongDataPlaneClientCertificateOnKonnectExtensionOwner,
			func(obj client.Object) []string {
				var names []string
				for _, owner := range obj.GetOwnerReferences() {
					names = append(names, owner.Name)
				}
				return names
			}).Build()
	return &KonnectExtensionReconciler{Client: cl, apiReader: cl}, ext
}

func dpCertTestList(t *testing.T, cl client.Client) map[string]configurationv1alpha1.KongDataPlaneClientCertificate {
	t.Helper()
	var l configurationv1alpha1.KongDataPlaneClientCertificateList
	require.NoError(t, cl.List(t.Context(), &l, client.InNamespace(dpCertTestNamespace)))
	out := map[string]configurationv1alpha1.KongDataPlaneClientCertificate{}
	for _, c := range l.Items {
		out[c.Name] = c
	}
	return out
}

func TestDataPlaneClientCertificateName(t *testing.T) {
	existing := func(name, cert string) []configurationv1alpha1.KongDataPlaneClientCertificate {
		c := configurationv1alpha1.KongDataPlaneClientCertificate{Name: name}
		c.Spec.Cert = cert
		return []configurationv1alpha1.KongDataPlaneClientCertificate{c}
	}

	assert.Equal(t, "ext", dataPlaneClientCertificateName("ext", nil, "cert"))
	assert.Equal(t, "ext", dataPlaneClientCertificateName("ext", existing("other", "old"), "cert"))
	assert.Equal(t, "ext", dataPlaneClientCertificateName("ext", existing("ext", "cert\n"), "cert"),
		"the object registering the same certificate keeps the extension name")
	assert.Equal(t, "ext", dataPlaneClientCertificateName("ext", existing("ext", "cert\n\n"), "cert\n\n"),
		"also when the certificate ends with a blank line")

	replacement := dataPlaneClientCertificateName("ext", existing("ext", "old"), "cert")
	assert.NotEqual(t, "ext", replacement, "an object registering another certificate holds the extension name")
	assert.Equal(t, replacement, dataPlaneClientCertificateName("ext", existing("ext", "old"), "cert\n"),
		"the replacement name is stable for the same certificate")
	assert.Equal(t, replacement, dataPlaneClientCertificateName("ext", append(existing("ext", "old"), existing(replacement, "cert")...), "cert"),
		"and it stays the same once the replacement exists")
}

// A certificate is registered once, also while its KongDataPlaneClientCertificate is not programmed yet.
func TestKonnectExtensionRegistersCertificateOnce(t *testing.T) {
	for _, cert := range []string{"cert-v1", "cert-v1\n", "cert-v1\n\n"} {
		t.Run(fmt.Sprintf("%q", cert), func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, cert)
			for range 10 {
				var current konnectv1alpha2.KonnectExtension
				require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), &current))
				_, err := r.Reconcile(t.Context(), &current)
				require.NoError(t, err)
			}

			certs := dpCertTestList(t, r.Client)
			require.Len(t, certs, 1)
			require.Contains(t, certs, dpCertTestExtName)
			assert.Equal(t, cert, certs[dpCertTestExtName].Spec.Cert)
		})
	}
}

// When the certificate in the Secret changes, the new one is registered next to the one registered
// before, which is deleted only once the new one is programmed.
func TestKonnectExtensionReplacesRegisteredCertificate(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "cert-v2", dpCertTestObject(dpCertTestExtName, "cert-v1"))

	var sawBoth bool
	for round := range 40 {
		var current konnectv1alpha2.KonnectExtension
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), &current))
		_, err := r.Reconcile(t.Context(), &current)
		require.NoError(t, err)

		certs := dpCertTestList(t, r.Client)
		require.LessOrEqual(t, len(certs), 2)

		// Program new objects only every few reconciles, as the KongDataPlaneClientCertificate controller would.
		if round%4 == 3 {
			for name, c := range certs {
				if c.Status.Konnect == nil || c.Status.Konnect.ID == "" {
					c.Status = dpCertTestProgrammedStatus("konnect-id-" + name)
					require.NoError(t, r.Status().Update(t.Context(), &c))
				}
			}
		}

		if len(certs) == 2 {
			sawBoth = true
			assert.Contains(t, certs, dpCertTestExtName, "the previous certificate stays registered until the new one is programmed")
		}
	}
	require.True(t, sawBoth)

	certs := dpCertTestList(t, r.Client)
	require.Len(t, certs, 1)
	for name, c := range certs {
		assert.NotEqual(t, dpCertTestExtName, name)
		assert.Equal(t, "cert-v2", c.Spec.Cert)
	}
}

// When the certificate in the Secret changes back to one whose object is being deleted, the object
// registering the other certificate stays until that deletion is complete and the certificate has
// been registered again; the extension must never be left without a registered certificate.
func TestKonnectExtensionKeepsRegisteredCertificateWhilePreviousIsDeleting(t *testing.T) {
	now := metav1.Now()
	deleting := dpCertTestObject(dpCertTestExtName, "cert-a")
	deleting.DeletionTimestamp = &now
	deleting.Finalizers = []string{"test/hold"}
	r, ext := dpCertTestReconciler(t, "cert-a", deleting, dpCertTestObject(dpCertTestExtName+"-b", "cert-b"))

	reconcile := func() {
		t.Helper()
		var current konnectv1alpha2.KonnectExtension
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), &current))
		_, err := r.Reconcile(t.Context(), &current)
		require.NoError(t, err)
	}

	for range 20 {
		reconcile()
		certs := dpCertTestList(t, r.Client)
		assert.Contains(t, certs, dpCertTestExtName+"-b", "the registered certificate stays while the other object is being deleted")
	}

	// Complete the deletion, then let the certificate be registered again.
	held := dpCertTestList(t, r.Client)[dpCertTestExtName]
	held.Finalizers = nil
	require.NoError(t, r.Update(t.Context(), &held))
	for round := range 40 {
		reconcile()
		certs := dpCertTestList(t, r.Client)
		require.NotEmpty(t, certs, "a certificate is always registered")
		if round%4 == 3 {
			for name, c := range certs {
				if c.Status.Konnect == nil || c.Status.Konnect.ID == "" {
					c.Status = dpCertTestProgrammedStatus("konnect-id-" + name)
					require.NoError(t, r.Status().Update(t.Context(), &c))
				}
			}
		}
	}

	certs := dpCertTestList(t, r.Client)
	require.Len(t, certs, 1)
	for _, c := range certs {
		assert.Equal(t, "cert-a", c.Spec.Cert)
	}
}
