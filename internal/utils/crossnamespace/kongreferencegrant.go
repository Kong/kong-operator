package crossnamespace

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

// ErrReferenceNotGranted is an error type that indicates a cross-namespace
// reference is not granted by any KongReferenceGrant.
type ErrReferenceNotGranted struct {
	FromNamespace string
	FromGVK       metav1.GroupVersionKind
	ToNamespace   string
	ToName        string
	ToGVK         metav1.GroupVersionKind
}

// Error returns a formatted error message indicating that a cross-namespace reference
// is not permitted.
func (e *ErrReferenceNotGranted) Error() string {
	return fmt.Sprintf("cross-namespace reference to %s %s in namespace %s from %s.%s in namespace %s is not granted by any KongReferenceGrant",
		e.ToGVK.Kind,
		e.ToName,
		e.ToNamespace,
		e.FromGVK.Kind,
		e.FromGVK.Group,
		e.FromNamespace,
	)
}

// IsReferenceNotGranted checks if the provided error is or wraps
// an ErrReferenceNotGranted error, indicating that a cross-namespace
// reference was attempted without the proper ReferenceGrant permissions.
// It returns true if the error matches this type, false otherwise.
func IsReferenceNotGranted(err error) bool {
	var target *ErrReferenceNotGranted
	return errors.As(err, &target)
}

// CheckKongReferenceGrantForResource verifies that a cross-namespace reference is permitted by checking
// for an appropriate KongReferenceGrant. It validates whether a resource in one namespace (from) is allowed
// to reference a resource in another namespace (to).
//
// Parameters:
//   - cl: The Kubernetes client used to query KongReferenceGrant resources
//   - ctx: The context for the operation
//   - fromNamespace: The namespace containing the resource making the reference
//   - toNamespace: The namespace containing the resource being referenced
//   - toName: The name of the resource being referenced
//   - fromGVK: The GroupVersionKind of the resource making the reference
//   - toGVK: The GroupVersionKind of the resource being referenced
//
// Returns an error if:
//   - The verification of the KongReferenceGrant fails
//   - No valid KongReferenceGrant exists that permits the cross-namespace reference
//
// Returns nil if the cross-namespace reference is properly granted.
func CheckKongReferenceGrantForResource(
	ctx context.Context,
	cl client.Client,
	fromNamespace string,
	toNamespace string,
	toName string,
	fromGVK,
	toGVK metav1.GroupVersionKind,
) error {
	granted, err := isReferenceGranted(
		cl,
		ctx,
		fromNamespace,
		toNamespace,
		toName,
		fromGVK,
		toGVK,
	)
	if err != nil {
		return fmt.Errorf("failed to verify KongReferenceGrant for resource %q in namespace %q referenced from %s/%s in namespace %q: %w",
			toName,
			toNamespace,
			fromGVK.Kind,
			fromGVK.Group,
			fromNamespace,
			err,
		)
	}
	if !granted {
		return &ErrReferenceNotGranted{
			FromNamespace: fromNamespace,
			FromGVK:       fromGVK,
			ToNamespace:   toNamespace,
			ToName:        toName,
			ToGVK:         toGVK,
		}
	}
	return nil
}

// isReferenceGranted checks if a cross-namespace reference from one resource to another
// is granted by a KongReferenceGrant in the target namespace.
//
// It verifies that a KongReferenceGrant exists in the toNamespace that:
//   - Allows references FROM the specified fromNamespace, fromGVK (Group/Version/Kind)
//   - TO the specified toName and toGVK (Group/Version/Kind)
//
// Parameters:
//   - cl: Kubernetes client for listing KongReferenceGrants
//   - ctx: Context for the operation
//   - fromNamespace: Namespace of the source resource making the reference
//   - toNamespace: Namespace of the target resource being referenced
//   - toName: Name of the specific target resource
//   - fromGVK: GroupVersionKind of the source resource type
//   - toGVK: GroupVersionKind of the target resource type
//
// Returns:
//   - bool: true if a matching KongReferenceGrant is found, false otherwise
//   - error: any error encountered while listing KongReferenceGrants
func isReferenceGranted(cl client.Client, ctx context.Context, fromNamespace string, toNamespace string, toName string, fromGVK, toGVK metav1.GroupVersionKind) (bool, error) {
	var refGrants configurationv1alpha1.KongReferenceGrantList
	if err := cl.List(ctx, &refGrants, client.InNamespace(toNamespace)); err != nil {
		return false, fmt.Errorf("failed to list KongReferenceGrants in namespace %q: %w", toNamespace, err)
	}
	return ReferenceGrantsAllow(refGrants.Items, fromNamespace, toName, fromGVK, toGVK), nil
}

// ReferenceGrantsAllow checks if any of the provided KongReferenceGrants allow a reference
// from a resource in fromNamespace with the specified fromGVK to a resource named toName
// with the specified toGVK.
//
// The function iterates through all grants and returns true if it finds a grant that:
//   - Has a matching 'from' entry with the specified namespace, group, and kind
//   - Has a matching 'to' entry with the specified name (or no name specified), group, and kind
//
// Parameters:
//   - grants: slice of KongReferenceGrants to check
//   - fromNamespace: namespace of the referencing resource
//   - toName: name of the referenced resource
//   - fromGVK: GroupVersionKind of the referencing resource
//   - toGVK: GroupVersionKind of the referenced resource
//
// Returns true if at least one grant allows the reference, false otherwise.
func ReferenceGrantsAllow(grants []configurationv1alpha1.KongReferenceGrant, fromNamespace string, toName string, fromGVK, toGVK metav1.GroupVersionKind) bool {
	for _, refGrant := range grants {
		fromMatched := false
		for _, from := range refGrant.Spec.From {
			if from.Namespace == configurationv1alpha1.Namespace(fromNamespace) &&
				from.Group == configurationv1alpha1.Group(fromGVK.Group) &&
				from.Kind == configurationv1alpha1.Kind(fromGVK.Kind) {
				fromMatched = true
				break
			}
		}
		if !fromMatched {
			continue
		}

		for _, to := range refGrant.Spec.To {
			if (to.Name == nil || (to.Name != nil && *to.Name == configurationv1alpha1.ObjectName(toName))) &&
				to.Group == configurationv1alpha1.Group(toGVK.Group) &&
				to.Kind == configurationv1alpha1.Kind(toGVK.Kind) {
				return true
			}
		}
	}

	return false
}
