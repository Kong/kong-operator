package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// Secret generators
// -----------------------------------------------------------------------------

type SecretOpt func(*corev1.Secret)

func SecretWithLabel(k, v string) func(s *corev1.Secret) {
	return func(s *corev1.Secret) {
		if s.Labels == nil {
			s.Labels = make(map[string]string)
		}
		s.Labels[k] = v
	}
}

// GenerateNewTLSSecret is a helper to generate a TLS Secret
// to be used for mutual TLS.
func GenerateNewTLSSecret(namespace, namePrefix, ownerPrefix string, opts ...SecretOpt) *corev1.Secret {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: fmt.Sprintf("%s-%s-", ownerPrefix, namePrefix),
		},
		Type: corev1.SecretTypeTLS,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
