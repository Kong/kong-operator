package ref

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// ReferenceGrantForSecretFrom returns a predicate function that checks if the
// ReferenceGrant is for the specified group and kind.
func ReferenceGrantForSecretFrom(group gatewayv1.Group, kind gatewayv1.Kind) predicate.TypedFuncs[client.Object] {
	return predicate.NewPredicateFuncs(
		func(obj client.Object) bool {
			grant, ok := obj.(*gatewayv1beta1.ReferenceGrant)
			if !ok {
				return false
			}
			for _, from := range grant.Spec.From {
				if from.Kind == kind && from.Group == group {
					return true
				}
			}
			return false
		},
	)
}

// IsReferenceGrantForObj checks if ReferenceGrant's from clause matches the provided object's Group, Kind and namespace.
func IsReferenceGrantForObj(referenceGrant *gatewayv1beta1.ReferenceGrant, obj client.Object) bool {
	for _, from := range referenceGrant.Spec.From {
		if string(from.Namespace) == obj.GetNamespace() &&
			from.Kind == gatewayv1.Kind(obj.GetObjectKind().GroupVersionKind().Kind) &&
			from.Group == gatewayv1.Group(obj.GetObjectKind().GroupVersionKind().Group) {
			return true
		}
	}
	return false
}

// EnsureNamespaceInSecretRef ensures that the Namespace in the SecretObjectReference is set.
// If it is not set, it is set to referencerNamespace.
func EnsureNamespaceInSecretRef(secretRef *gatewayv1.SecretObjectReference, referencerNamespace gatewayv1.Namespace) {
	if secretRef.Namespace == nil || *secretRef.Namespace == "" {
		secretRef.Namespace = lo.ToPtr(referencerNamespace)
	}
}

// DoesFieldReferenceCoreV1Secret checks if the SecretObjectReference refers to a Secret in the Corev1 group.
// If it does not, an error with explanation is returned.
func DoesFieldReferenceCoreV1Secret(secretRef gatewayv1.SecretObjectReference, fieldName string) error {
	var errMessages []string
	if secretRef.Group != nil && *secretRef.Group != "" && *secretRef.Group != gatewayv1.Group(corev1.SchemeGroupVersion.Group) {
		errMessages = append(errMessages, fmt.Sprintf("Group %s not supported in %s.", *secretRef.Group, fieldName))
	}
	if secretRef.Kind != nil && *secretRef.Kind != "" && *secretRef.Kind != gatewayv1.Kind("Secret") {
		errMessages = append(errMessages, fmt.Sprintf("Kind %s not supported in %s.", *secretRef.Kind, fieldName))
	}
	if len(errMessages) > 0 {
		return errors.New(strings.Join(errMessages, " "))
	}
	return nil
}

// CheckReferenceGrantForSecret checks if the reference from the object (fromObj) to the secret specified in secretRef
// is granted. It is expected that secretRef.Namespace is set otherwise an error is returned. Examining returned values
// makes sense only if err is nil. When isReferenceGranted is false, whyNotGranted provides the reason (otherwise it is
// expected to be discarded).
func CheckReferenceGrantForSecret(
	ctx context.Context, c client.Client, fromObj client.Object, secretRef gatewayv1.SecretObjectReference,
) (whyNotGranted string, isReferenceGranted bool, err error) {
	if secretRef.Namespace == nil || *secretRef.Namespace == "" {
		return "", false, fmt.Errorf("caller must ensure that Namespace in SecretObjectReference is set (bug in the code)")
	}
	if gatewayv1.Namespace(fromObj.GetNamespace()) == *secretRef.Namespace {
		return "", true, nil
	}

	allowed, err := k8sutils.AllowedByReferenceGrants(
		ctx, c,
		gatewayv1beta1.ReferenceGrantFrom{
			Group:     gatewayv1.Group(fromObj.GetObjectKind().GroupVersionKind().Group),
			Kind:      gatewayv1.Kind(fromObj.GetObjectKind().GroupVersionKind().Kind),
			Namespace: gatewayv1.Namespace(fromObj.GetNamespace()),
		},
		string(*secretRef.Namespace),
		gatewayv1beta1.ReferenceGrantTo{
			Kind: "Secret",
			Name: lo.ToPtr(secretRef.Name),
		},
	)
	if err != nil {
		return "", false, fmt.Errorf("failed to check if Secret %s/%s is allowed by ReferenceGrants: %w",
			*secretRef.Namespace, secretRef.Name, err)
	}
	if !allowed {
		return fmt.Sprintf("Secret %s/%s reference not allowed by any ReferenceGrant", *secretRef.Namespace, secretRef.Name), false, nil
	}
	return "", true, nil
}
