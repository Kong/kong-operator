package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func GenerateKongDataPlaneClientCertificateForSecret(secret *corev1.Secret, certData string, controlPlaneRef commonv1alpha1.ControlPlaneRef) (*configurationv1alpha1.KongDataPlaneClientCertificate, error) {
	cert := &configurationv1alpha1.KongDataPlaneClientCertificate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", secret.Name),
			Namespace:    secret.Namespace,
			Labels:       GetManagedLabelForOwner(secret),
		},
		Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
			ControlPlaneRef: &controlPlaneRef,
			KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
				Cert: certData,
			},
		},
	}

	k8sutils.SetOwnerForObject(cert, secret)
	return cert, nil
}
