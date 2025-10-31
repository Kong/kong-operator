package hybridgateway

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/hybridgateway/converter"
	"github.com/kong/kong-operator/controller/hybridgateway/managedfields"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
)

const (
	// FieldManager is the field manager name used for server-side apply operations
	FieldManager = "gateway-operator"
)

// Translate performs the full translation process using the provided APIConverter.
func Translate[t converter.RootObject](conv converter.APIConverter[t], ctx context.Context, logger logr.Logger) error {
	return conv.Translate(ctx, logger)
}

// EnforceState ensures that the desired state of Kubernetes resources, as provided by the APIConverter,
// is reflected in the cluster. It attempts to create or update resources using server-side apply and
// structured merge. The function returns requeue and stop flags to control reconciliation flow, and an error
// for any unrecoverable or transient issues. Resources marked for deletion are skipped. Conflict errors
// trigger a requeue for optimistic concurrency. All other errors are wrapped with resource kind and name for context.
func EnforceState[t converter.RootObject](ctx context.Context, cl client.Client, logger logr.Logger, conv converter.APIConverter[t]) (requeue bool, stop bool, err error) {
	// Get the desired state from the converter.
	desiredObjects := conv.GetOutputStore(ctx)
	if len(desiredObjects) == 0 {
		logger.V(1).Info("No desired objects to enforce")
		return false, false, nil
	}

	for _, desired := range desiredObjects {
		// Get the existing object by name from the API server.
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(desired.GetObjectKind().GroupVersionKind())

		err := cl.Get(ctx, client.ObjectKey{
			Namespace: desired.GetNamespace(),
			Name:      desired.GetName(),
		}, existing)

		namespacedNameDesired := client.ObjectKeyFromObject(&desired)
		namespacedNameExisting := client.ObjectKeyFromObject(existing)

		if err != nil {
			if errors.IsNotFound(err) {
				// Object doesn't exist, create it using server-side apply.
				logger.V(1).Info("Creating new object", "kind", desired.GetKind(), "obj", namespacedNameDesired)

				// Set field manager for server-side apply
				if err := cl.Patch(ctx, &desired, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
					if errors.IsConflict(err) {
						return true, false, fmt.Errorf("conflict during create of object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
					}
					return true, false, fmt.Errorf("failed to create object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
				}
				continue
			} else {
				// Other error getting the object.
				return true, false, fmt.Errorf("failed to get object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
			}
		}

		// Handle the case when resource are marked for deletion.
		if !existing.GetDeletionTimestamp().IsZero() {
			logger.V(1).Info("Existing object is marked for deletion, will not enforce state", "kind", existing.GetKind(), "obj", namespacedNameDesired)
			continue
		}

		// Object exists, check if we need to update it.
		managedFieldsObj, err := managedfields.ExtractAsUnstructured(existing, FieldManager, "")
		if err != nil {
			return true, false, fmt.Errorf("failed to extract managed fields for kind %s obj %s: %w", existing.GetKind(), namespacedNameExisting, err)
		}
		if managedFieldsObj == nil {
			// No managed fields for our field manager, we should update.
			logger.V(1).Info("No managed fields found for our field manager, will apply desired state", "kind", existing.GetKind(), "obj", namespacedNameExisting)
			if err := cl.Patch(ctx, &desired, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
				if errors.IsConflict(err) {
					return true, false, fmt.Errorf("conflict during create of object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
				}
				return true, false, fmt.Errorf("failed to create object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
			}
			continue
		}

		// Convert desired resource to unstructured.
		desiredU, err := utils.ToUnstructured(&desired, cl.Scheme())
		if err != nil {
			return true, false, fmt.Errorf("failed to convert to unstructured desired obj for kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
		}

		// Compare the two states.
		compare, err := managedfields.Compare(managedFieldsObj, pruneDesiredObj(desiredU))
		if err != nil {
			return true, false, fmt.Errorf("failed to compare managed fields for kind %s obj %s: %w", existing.GetKind(), namespacedNameExisting, err)
		}

		if compare.IsSame() {
			logger.V(3).Info("No changes detected for obj", "kind", existing.GetKind(), "obj", namespacedNameExisting)
		} else {
			logger.Info("Changes detected for obj, applying desired state", "kind", existing.GetKind(), "obj", namespacedNameExisting, "changes", compare.String())
			// Changes detected, apply the desired state using server-side apply.
			if err := cl.Patch(ctx, &desired, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
				if errors.IsConflict(err) {
					return true, false, fmt.Errorf("conflict during create of object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
				}
				return true, false, fmt.Errorf("failed to update object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
			}
		}
	}

	return false, false, nil
}

// EnforceStatus updates the status of the root object managed by the provided APIConverter.
// This function delegates to the converter's UpdateRootObjectStatus method to handle
// status condition management and cluster updates.
//
// Parameters:
//   - ctx: The context for API calls
//   - logger: Logger for debugging information
//   - conv: The APIConverter that manages the root object and its status
//
// Returns:
//   - stop: true if reconciliation should stop, false to continue
//   - err: Any error that occurred during status processing
//
// This is a generic wrapper function that works with any converter implementing
// the APIConverter interface, providing a consistent interface for status enforcement
// across different resource types.
func EnforceStatus[t converter.RootObject](ctx context.Context, logger logr.Logger, conv converter.APIConverter[t]) (stop bool, err error) {
	return conv.UpdateRootObjectStatus(ctx, logger)
}

// CleanOrphanedResources deletes resources previously managed by the converter but no longer present in the desired output.
func CleanOrphanedResources[t converter.RootObject, tPtr converter.RootObjectPtr[t]](ctx context.Context, cl client.Client, logger logr.Logger, conv converter.APIConverter[t]) error {
	desiredObjects := conv.GetOutputStore(ctx)
	desiredSet := make(map[string]struct{})
	expectedGVKs := conv.GetExpectedGVKs()

	// Extract the root object for label selector.
	rootObj := conv.GetRootObject()
	var rootObjPtr tPtr
	switch v := any(&rootObj).(type) {
	case tPtr:
		rootObjPtr = v
	default:
		return fmt.Errorf("failed to convert root object to pointer type: got %T, expected %T", &rootObj, rootObjPtr)
	}

	// Build a set of desired resource keys.
	for _, obj := range desiredObjects {
		key := fmt.Sprintf("%s/%s/%s", obj.GetNamespace(), obj.GetName(), obj.GetObjectKind().GroupVersionKind().String())
		desiredSet[key] = struct{}{}
	}

	// For each expected GVK, list resources and delete orphans.
	for _, gvk := range expectedGVKs {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		selector := metadata.LabelSelectorForOwnedResources(rootObjPtr, nil)

		// List all resources of this GVK owned by the root object in the same namespace.
		ns := rootObjPtr.GetNamespace()

		if err := cl.List(ctx, list, selector, client.InNamespace(ns)); err != nil {
			return fmt.Errorf("unable to list objects with gvk %s in namespace %s: %w", gvk.String(), ns, err)
		}

		for _, item := range list.Items {
			key := fmt.Sprintf("%s/%s/%s", item.GetNamespace(), item.GetName(), gvk.String())
			if _, found := desiredSet[key]; !found {
				// Not in desired output, delete it.
				logger.Info("Deleting orphaned resource", "kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
				if err := cl.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
					return fmt.Errorf("failed to delete orphaned resource kind %s obj %s: %w", item.GetKind(), client.ObjectKeyFromObject(&item), err)
				}
			}
		}
	}
	return nil
}

// pruneDesiredObj removes fields that should not be compared when checking for differences.
func pruneDesiredObj(obj unstructured.Unstructured) *unstructured.Unstructured {
	u := obj.DeepCopy()
	// Remove metadata fields such as name and namespace from the desired object that are not managed by the controller.
	unstructured.RemoveNestedField(u.Object, "metadata", "name")
	unstructured.RemoveNestedField(u.Object, "metadata", "namespace")
	managedfields.PruneEmptyFields(u)
	return u
}
