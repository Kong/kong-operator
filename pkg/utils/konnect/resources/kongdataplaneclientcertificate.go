package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/pkg/consts"
)

// GenerateKongDataPlaneClientCertificate generates a KongDataPlaneClientCertificate object setting
// the provided controlPlaneRef as the certificate controlPlaneRef.
// The cert parameter is the actual certificate pushed into Konnect.
func GenerateKongDataPlaneClientCertificate(
	name, namespace string,
	controlPlaneRef *commonv1alpha1.KonnectExtensionControlPlaneRef,
	cert string,
	owner client.Object,
	opts ...func(dpCert *configurationv1alpha1.KongDataPlaneClientCertificate),
) configurationv1alpha1.KongDataPlaneClientCertificate {
	dpCert := configurationv1alpha1.KongDataPlaneClientCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				consts.GatewayOperatorManagedByLabel:     "KonnectExtension",
				consts.GatewayOperatorManagedByNameLabel: owner.GetName(),
			},
		},
		Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
			KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
				Cert: cert,
			},
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: controlPlaneRef.KonnectNamespacedRef.Name,
					// no cross-namespace references supported yet
				},
			},
		},
	}

	for _, opt := range opts {
		opt(&dpCert)
	}

	return dpCert
}
