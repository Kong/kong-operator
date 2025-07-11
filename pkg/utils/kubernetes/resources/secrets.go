package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
)

// -----------------------------------------------------------------------------
// Secret generators
// -----------------------------------------------------------------------------

// ControlPlaneOrDataPlaneOrKonnectExtension is a type that can be either a ControlPlane, a DataPlane or a KonnectExtension.
// It is used to infer the types that can own secret resources.
type ControlPlaneOrDataPlaneOrKonnectExtension interface {
	*gwtypes.ControlPlane |
		*operatorv1beta1.DataPlane |
		*konnectv1alpha2.KonnectExtension
}

// SecretOpt is an option function for a Secret.
type SecretOpt func(*corev1.Secret)

// SecretWithLabel adds a label to a Secret.
func SecretWithLabel(k, v string) func(s *corev1.Secret) {
	return func(s *corev1.Secret) {
		if s.Labels == nil {
			s.Labels = make(map[string]string)
		}
		s.Labels[k] = v
	}
}

// WithAnnotation adds an annotation to an object.
func WithAnnotation[T client.Object](k, v string) func(d T) {
	return func(obj T) {
		anns := obj.GetAnnotations()
		if anns == nil {
			anns = make(map[string]string)
		}
		anns[k] = v
		obj.SetAnnotations(anns)
	}
}

func getPrefixForOwner[T ControlPlaneOrDataPlaneOrKonnectExtension](owner T) string {
	switch any(owner).(type) {
	case *gwtypes.ControlPlane:
		return consts.ControlPlanePrefix
	case *operatorv1beta1.DataPlane:
		return consts.DataPlanePrefix
	case *konnectv1alpha2.KonnectExtension:
		return consts.KonnectExtensionPrefix
	default:
		return ""
	}
}

// addLabelForOwner labels the provided object as managed by the provided owner.
func addLabelForOwner[T ControlPlaneOrDataPlaneOrKonnectExtension](obj client.Object, owner T) {
	switch any(owner).(type) {
	case *gwtypes.ControlPlane:
		LabelObjectAsControlPlaneManaged(obj)
	case *operatorv1beta1.DataPlane:
		LabelObjectAsDataPlaneManaged(obj)
	case *konnectv1alpha2.KonnectExtension:
		LabelObjectAsKonnectExtensionManaged(obj)
	}
}

// GenerateNewTLSSecret is a helper to generate a TLS Secret
// to be used for mutual TLS.
// It accepts a list of options that can change the generated Secret.
func GenerateNewTLSSecret[
	T interface {
		ControlPlaneOrDataPlaneOrKonnectExtension
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
				GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-%s-", ownerPrefix, owner.GetName())),
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
