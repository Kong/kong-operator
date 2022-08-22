package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// Secret generators
// -----------------------------------------------------------------------------

// GenerateNewTLSSecret is a helper to generate a TLS Secret
// to be used for mutual TLS.
func GenerateNewTLSSecret(namespace, namePrefix, ownerPrefix string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: fmt.Sprintf("%s-%s-", ownerPrefix, namePrefix),
		},
		Type: corev1.SecretTypeTLS,
	}
}
