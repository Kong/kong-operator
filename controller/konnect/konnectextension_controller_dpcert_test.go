package konnect

import (
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

	replacement := dataPlaneClientCertificateName("ext", existing("ext", "old"), "cert")
	assert.NotEqual(t, "ext", replacement, "an object registering another certificate holds the extension name")
	assert.Equal(t, replacement, dataPlaneClientCertificateName("ext", existing("ext", "old"), "cert\n"),
		"the replacement name is stable for the same certificate")
	assert.Equal(t, replacement, dataPlaneClientCertificateName("ext", append(existing("ext", "old"), existing(replacement, "cert")...), "cert"),
		"and it stays the same once the replacement exists")
}

// A certificate is registered once, also while its KongDataPlaneClientCertificate is not programmed yet.
func TestKonnectExtensionRegistersCertificateOnce(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "cert-v1")
	for range 10 {
		var current konnectv1alpha2.KonnectExtension
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), &current))
		_, err := r.Reconcile(t.Context(), &current)
		require.NoError(t, err)
	}

	certs := dpCertTestList(t, r.Client)
	require.Len(t, certs, 1)
	require.Contains(t, certs, dpCertTestExtName)
	assert.Equal(t, "cert-v1", certs[dpCertTestExtName].Spec.Cert)
}

// When the certificate in the Secret changes, the new one is registered next to the one registered
// before, which is deleted only once the new one is programmed.
func TestKonnectExtensionReplacesRegisteredCertificate(t *testing.T) {
	previous := &configurationv1alpha1.KongDataPlaneClientCertificate{
		Name: dpCertTestExtName, Namespace: dpCertTestNamespace,
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: konnectv1alpha2.GroupVersion.String(), Kind: "KonnectExtension", Name: dpCertTestExtName, UID: types.UID(dpCertTestExtName),
		}},
		Spec:   configurationv1alpha1.KongDataPlaneClientCertificateSpec{KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{Cert: "cert-v1"}},
		Status: dpCertTestProgrammedStatus("konnect-id-v1"),
	}
	r, ext := dpCertTestReconciler(t, "cert-v2", previous)

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
