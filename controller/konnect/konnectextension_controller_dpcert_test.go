package konnect

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
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
		Data:      map[string][]byte{consts.TLSCRT: []byte(secretCert), consts.TLSKey: []byte("private-key")},
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
	assert.Equal(t, replacement, dataPlaneClientCertificateName("ext", existing(replacement, "cert"), "cert"),
		"removing the original object must not duplicate a pending replacement")

	t.Run("long extension names", func(t *testing.T) {
		names := []string{
			strings.Repeat("a", validation.DNS1123SubdomainMaxLength),
			strings.Repeat("a", 243) + "." + strings.Repeat("b", 9),
			strings.Repeat("a", 244) + strings.Repeat("b", 9),
		}
		replacements := map[string]bool{}
		for _, name := range names {
			replacement := dataPlaneClientCertificateName(name, existing(name, "old"), "cert")
			assert.Empty(t, validation.IsDNS1123Subdomain(replacement))
			assert.LessOrEqual(t, len(replacement), validation.DNS1123SubdomainMaxLength)
			assert.False(t, replacements[replacement], "different extensions must not share a replacement name")
			replacements[replacement] = true
			assert.Equal(t, replacement, dataPlaneClientCertificateName(name, existing(name, "old"), "cert\n"))
		}
	})
}

func TestKonnectExtensionWaitsForRegisteredCertificate(t *testing.T) {
	for _, state := range []string{"missing", "no ID", "not programmed", "deleting"} {
		t.Run(state, func(t *testing.T) {
			previous := dpCertTestObject(dpCertTestExtName, "cert-v1")
			desired := dpCertTestObject(dataPlaneClientCertificateName(
				dpCertTestExtName,
				[]configurationv1alpha1.KongDataPlaneClientCertificate{*previous},
				"cert-v2",
			), "cert-v2")
			switch state {
			case "no ID":
				desired.Status = configurationv1alpha1.KongDataPlaneClientCertificateStatus{}
			case "not programmed":
				desired.Status.Conditions[0].Status = metav1.ConditionFalse
			case "deleting":
				now := metav1.Now()
				desired.DeletionTimestamp = &now
				desired.Finalizers = []string{"test/hold"}
			}
			objects := []client.Object{previous}
			if state != "missing" {
				objects = append(objects, desired)
			}
			r, ext := dpCertTestReconciler(t, "cert-v2", objects...)
			ext.Status.Conditions = []metav1.Condition{
				{Type: konnectv1alpha2.KonnectExtensionReadyConditionType, Status: metav1.ConditionTrue},
				{Type: konnectv1alpha1.DataPlaneCertificateProvisionedConditionType, Status: metav1.ConditionTrue},
			}
			require.NoError(t, r.Status().Update(t.Context(), ext))
			for range 10 {
				require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
				_, err := r.Reconcile(t.Context(), ext)
				require.NoError(t, err)
			}
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
			assert.True(t, k8sutils.HasConditionFalse(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
			assert.True(t, k8sutils.HasConditionFalse(konnectv1alpha1.DataPlaneCertificateProvisionedConditionType, ext))
			assert.Contains(t, dpCertTestList(t, r.Client), previous.Name)
		})
	}
}

func TestKonnectExtensionReportsCertificateCreateError(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "cert-v2", dpCertTestObject(dpCertTestExtName, "cert-v1"))
	createErr := apierrors.NewForbidden(
		configurationv1alpha1.GroupVersion.WithResource("kongdataplaneclientcertificates").GroupResource(),
		"replacement",
		fmt.Errorf("certificate creation denied"),
	)
	cl, ok := r.Client.(client.WithWatch)
	require.True(t, ok)
	r.Client = interceptor.NewClient(cl, interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*configurationv1alpha1.KongDataPlaneClientCertificate); ok {
				return createErr
			}
			return cl.Create(ctx, obj, opts...)
		},
	})
	for range 10 {
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
		_, err := r.Reconcile(t.Context(), ext)
		require.NoError(t, err)
	}
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
	condition, found := k8sutils.GetCondition(konnectv1alpha1.DataPlaneCertificateProvisionedConditionType, ext)
	require.True(t, found)
	assert.Equal(t, metav1.ConditionFalse, condition.Status)
	assert.Equal(t, konnectv1alpha1.DataPlaneCertificateProvisionedReasonKonnectAPIOpFailed, condition.Reason)
	assert.Equal(t, createErr.Error(), condition.Message)
	assert.True(t, k8sutils.HasConditionFalse(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
	require.Len(t, dpCertTestList(t, r.Client), 1)
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
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
	assert.True(t, k8sutils.HasConditionTrue(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
	assert.True(t, k8sutils.HasConditionTrue(konnectv1alpha1.DataPlaneCertificateProvisionedConditionType, ext))
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
