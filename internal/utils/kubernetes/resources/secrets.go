package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
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

type controlPlaneOrDataPlane interface {
	*operatorv1alpha1.ControlPlane | *operatorv1beta1.DataPlane
}

func getPrefixForOwner[T controlPlaneOrDataPlane](owner T) string {
	switch any(owner).(type) {
	case *operatorv1alpha1.ControlPlane:
		return consts.ControlPlanePrefix
	case *operatorv1beta1.DataPlane:
		return consts.DataPlanePrefix
	default:
		return ""
	}
}

// addLabelForOwner labels the provided object as managed by the provided owner.
func addLabelForOwner[T controlPlaneOrDataPlane](obj client.Object, owner T) {
	switch any(owner).(type) {
	case *operatorv1alpha1.ControlPlane:
		LabelObjectAsControlPlaneManaged(obj)
	case *operatorv1beta1.DataPlane:
		LabelObjectAsDataPlaneManaged(obj)
	}
}

// GenerateNewTLSSecret is a helper to generate a TLS Secret
// to be used for mutual TLS.
// It accepts a list of options that can change the generated Secret.
func GenerateNewTLSSecret[
	T interface {
		controlPlaneOrDataPlane
		client.Object
	},
](
	owner T, opts ...SecretOpt,
) *corev1.Secret {
	var (
		ownerPrefix = getPrefixForOwner(owner)
		s           = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    owner.GetNamespace(),
				GenerateName: fmt.Sprintf("%s-%s-", ownerPrefix, owner.GetName()),
			},
			Type: corev1.SecretTypeTLS,
		}
	)
	k8sutils.SetOwnerForObject(s, owner)
	addLabelForOwner(s, owner)

	for _, opt := range opts {
		opt(s)
	}
	return s
}
