package resources

import (
	"fmt"

	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// KongDataPlaneClientCertificate generators
// -----------------------------------------------------------------------------

// GenerateNewClusterRoleBindingForControlPlane is a helper to generate a ClusterRoleBinding
// resource to bind roles to the service account used by the controlplane deployment.
func GenerateKongDataPlaneClientCertificatesForSecret(secret *corev1.Secret, extension *konnectv1alpha1.KonnectExtension) *configurationv1alpha1.KongDataPlaneClientCertificate {
	cert := &configurationv1alpha1.KongDataPlaneClientCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    secret.Namespace,
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-%s-", consts.SecretPrefix, secret.Name)),
		},
		Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type:                 extension.Spec.Konnect.ControlPlane.Ref.Type,
				KonnectNamespacedRef: extension.Spec.Konnect.ControlPlane.Ref.KonnectNamespacedRef,
			},
			KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
				Cert: string(secret.Data["tls.crt"]),
			},
		},
	}
	LabelObjectAsKonnectExtensionManaged(cert)
	k8sutils.SetOwnerForObject(cert, secret)

	return cert
}
