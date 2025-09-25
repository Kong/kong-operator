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
func Translate[t converter.RootObject](conv converter.APIConverter[t], ctx context.Context) error {
	return conv.Translate()
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

		if err != nil {
			if errors.IsNotFound(err) {
				// Object doesn't exist, create it using server-side apply.
				logger.V(1).Info("Creating new object", "kind", desired.GetKind(), "name", desired.GetName(), "namespace", desired.GetNamespace())

				// Set field manager for server-side apply
				if err := cl.Patch(ctx, &desired, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
					if errors.IsConflict(err) {
						logger.V(1).Info("Conflict during create, will retry", "error", err)
						return true, false, nil
					}
					return true, false, fmt.Errorf("failed to create object kind %s name %s: %w", desired.GetKind(), desired.GetName(), err)
				}
				continue
			} else {
				// Other error getting the object.
				logger.Error(err, "Failed to get existing object", "kind", desired.GetKind(), "name", desired.GetName())
				return true, false, err
			}
		}

		// Handle the case when resource are marked for deletion.
		if !existing.GetDeletionTimestamp().IsZero() {
			logger.V(1).Info("Existing object is marked for deletion, will not enforce state", "kind", existing.GetKind(), "name", existing.GetName())
			continue
		}

		// Object exists, check if we need to update it.
		managedFieldsObj, err := managedfields.ExtractAsUnstructured(existing, FieldManager, "")
		if err != nil {
			return true, false, fmt.Errorf("failed to extract managed fields for kind %s name %s: %w", existing.GetKind(), existing.GetName(), err)
		}
		if managedFieldsObj == nil {
			// No managed fields for our field manager, we should update.
			logger.V(1).Info("No managed fields found for our field manager, will apply desired state", "kind", existing.GetKind(), "name", existing.GetName())
			if err := cl.Patch(ctx, &desired, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
				if errors.IsConflict(err) {
					logger.V(1).Info("Conflict during create, will retry", "error", err)
					return true, false, nil
				}
				return true, false, fmt.Errorf("failed to create object kind %s name %s: %w", desired.GetKind(), desired.GetName(), err)
			}
			continue
		}

		// Convert desired resource to unstructured.
		desiredU, err := utils.ToUnstructured(&desired, cl.Scheme())
		if err != nil {
			return true, false, fmt.Errorf("failed to convert to unstructured desired resources for kind %s name %s: %w", desired.GetKind(), desired.GetName(), err)
		}

		// Compare the two states.
		compare, err := managedfields.Compare(managedFieldsObj, pruneDesiredObj(desiredU))
		if err != nil {
			return true, false, fmt.Errorf("failed to compare managed fields for kind %s name %s: %w", existing.GetKind(), existing.GetName(), err)
		}

		if compare.IsSame() {
			logger.V(3).Info("No changes detected for resource", "kind", existing.GetKind(), "name", existing.GetName())
		} else {
			logger.Info("Changes detected for resource, applying desired state", "kind", existing.GetKind(), "name", existing.GetName(), "changes", compare.String())
			// Changes detected, apply the desired state using server-side apply.
			if err := cl.Patch(ctx, &desired, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
				if errors.IsConflict(err) {
					logger.V(1).Info("Conflict during update, will retry", "error", err)
					return true, false, nil
				}
				return true, false, fmt.Errorf("failed to update object kind %s name %s: %w", desired.GetKind(), desired.GetName(), err)
			}
		}
	}

	return false, false, nil
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
		selector := metadata.LabelSelectorForOwnedResources(rootObjPtr)

		// List all resources of this GVK owned by the root object in the same namespace.
		ns := rootObjPtr.GetNamespace()

		if err := cl.List(ctx, list, selector, client.InNamespace(ns)); err != nil {
			return fmt.Errorf("unable to list objects with gvk %s: %w", gvk.String(), err)
		}

		for _, item := range list.Items {
			key := fmt.Sprintf("%s/%s/%s", item.GetNamespace(), item.GetName(), gvk.String())
			if _, found := desiredSet[key]; !found {
				// Not in desired output, delete it.
				logger.Info("Deleting orphaned resource", "kind", item.GetKind(), "name", item.GetName(), "namespace", item.GetNamespace())
				if err := cl.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
					return fmt.Errorf("failed to delete orphaned resource kind %s name %s: %w", item.GetKind(), item.GetName(), err)
				}
			}
		}
	}
	return nil
}

// pruneDesiredObj removes fields that should not be compared when checking for differences.
func pruneDesiredObj(obj unstructured.Unstructured) *unstructured.Unstructured {
	u := obj.DeepCopy()
	unstructured.RemoveNestedField(u.Object, "metadata", "name")
	unstructured.RemoveNestedField(u.Object, "metadata", "namespace")
	managedfields.PruneEmptyFields(u)
	return u
}
