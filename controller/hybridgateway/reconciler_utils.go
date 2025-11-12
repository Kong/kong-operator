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
	"github.com/kong/kong-operator/controller/pkg/log"
)

const (
	// FieldManager is the field manager name used for server-side apply operations
	FieldManager = "gateway-operator"
)

// translate performs the full translation process using the provided APIConverter.
// Returns the number of Kong resources created and an error if the translation fails.
func translate[t converter.RootObject](conv converter.APIConverter[t], ctx context.Context, logger logr.Logger) (int, error) {
	return conv.Translate(ctx, logger)
}

// enforceState ensures that the desired state of Kubernetes resources, as provided by the APIConverter,
// is reflected in the cluster. It attempts to create or update resources using server-side apply and
// structured merge. The function returns a boolean indicating if any changes were made and an error
// for any unrecoverable or transient issues. Resources marked for deletion are skipped. Conflict errors
// are returned as errors. All other errors are wrapped with resource kind and name for context.
//
// The function performs the following operations:
// 1. Retrieves the desired state from the converter's output store
// 2. For each desired resource, checks if it exists in the cluster
// 3. Creates new resources using server-side apply if they don't exist
// 4. Skips resources that are marked for deletion
// 5. Updates existing resources if changes are detected using managed fields comparison
// 6. Handles conflicts by returning an error for proper error handling
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - cl: The Kubernetes client for CRUD operations
//   - logger: Logger for structured logging with state-enforcement phase
//   - conv: The APIConverter that provides the desired state
//
// Returns:
//   - bool: true if any resources were created or updated in the cluster
//   - error: Any error that occurred during state enforcement
//
// The function uses server-side apply with the "gateway-operator" field manager to ensure
// proper ownership and conflict resolution when multiple controllers manage the same resources.
func enforceState[t converter.RootObject](ctx context.Context, cl client.Client, logger logr.Logger, conv converter.APIConverter[t]) (bool, error) {
	logger = logger.WithValues("phase", "state-enforcement")
	log.Debug(logger, "Starting state enforcement")

	// Get the desired state from the converter.
	desiredObjects, err := conv.GetOutputStore(ctx, logger)
	if err != nil {
		return false, fmt.Errorf("failed to get desired objects from converter: %w", err)
	}
	if len(desiredObjects) == 0 {
		log.Debug(logger, "No desired objects to enforce")
		return false, nil
	}

	log.Debug(logger, "Retrieved desired objects for enforcement", "objectCount", len(desiredObjects))

	var (
		objectsCreated = 0
		objectsUpdated = 0
		objectsSkipped = 0
	)

	for i, desired := range desiredObjects {
		log.Debug(logger, "Processing desired object", "index", i, "kind", desired.GetKind(), "name", desired.GetName())
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
				log.Debug(logger, "Creating new object", "kind", desired.GetKind(), "obj", namespacedNameDesired)

				// Set field manager for server-side apply
				if err := cl.Patch(ctx, &desired, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
					if errors.IsConflict(err) {
						return false, fmt.Errorf("conflict during create of object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
					}
					return false, fmt.Errorf("failed to create object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
				}
				objectsCreated++
				log.Debug(logger, "Successfully created object", "kind", desired.GetKind(), "obj", namespacedNameDesired)
				continue
			} else {
				// Other error getting the object.
				return false, fmt.Errorf("failed to get object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
			}
		}

		// Handle the case when resource are marked for deletion.
		if !existing.GetDeletionTimestamp().IsZero() {
			log.Debug(logger, "Existing object is marked for deletion, will not enforce state", "kind", existing.GetKind(), "obj", namespacedNameDesired)
			objectsSkipped++
			continue
		}

		// Object exists, check if we need to update it.
		managedFieldsObj, err := managedfields.ExtractAsUnstructured(existing, FieldManager, "")
		if err != nil {
			return false, fmt.Errorf("failed to extract managed fields for kind %s obj %s: %w", existing.GetKind(), namespacedNameExisting, err)
		}
		if managedFieldsObj == nil {
			// No managed fields for our field manager, we should update.
			log.Debug(logger, "No managed fields found for our field manager, will apply desired state", "kind", existing.GetKind(), "obj", namespacedNameExisting)
			if err := cl.Patch(ctx, &desired, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
				if errors.IsConflict(err) {
					return false, fmt.Errorf("conflict during create of object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
				}
				return false, fmt.Errorf("failed to create object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
			}
			objectsUpdated++
			log.Debug(logger, "Successfully applied desired state (no managed fields)", "kind", existing.GetKind(), "obj", namespacedNameExisting)
			continue
		}

		// Convert desired resource to unstructured.
		desiredU, err := utils.ToUnstructured(&desired, cl.Scheme())
		if err != nil {
			return false, fmt.Errorf("failed to convert to unstructured desired obj for kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
		}

		// Compare the two states.
		compare, err := managedfields.Compare(managedFieldsObj, pruneDesiredObj(desiredU))
		if err != nil {
			return false, fmt.Errorf("failed to compare managed fields for kind %s obj %s: %w", existing.GetKind(), namespacedNameExisting, err)
		}

		if compare.IsSame() {
			log.Trace(logger, "No changes detected for obj", "kind", existing.GetKind(), "obj", namespacedNameExisting)
		} else {
			log.Info(logger, "Changes detected for obj, applying desired state", "kind", existing.GetKind(), "obj", namespacedNameExisting, "changes", compare.String())
			// Changes detected, apply the desired state using server-side apply.
			if err := cl.Patch(ctx, &desired, client.Apply, client.FieldOwner(FieldManager), client.ForceOwnership); err != nil {
				if errors.IsConflict(err) {
					return false, fmt.Errorf("conflict during create of object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
				}
				return false, fmt.Errorf("failed to update object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
			}
			objectsUpdated++
			log.Debug(logger, "Successfully applied changes to object", "kind", existing.GetKind(), "obj", namespacedNameExisting)
		}
	}

	log.Debug(logger, "Finished state enforcement",
		"totalObjects", len(desiredObjects),
		"created", objectsCreated,
		"updated", objectsUpdated,
		"skipped", objectsSkipped)

	// Return true if any resources were created or updated
	stateChanged := (objectsCreated + objectsUpdated) > 0
	return stateChanged, nil
}

// enforceStatus updates the status of the root object managed by the provided APIConverter.
// This function delegates to the converter's UpdateRootObjectStatus method to handle
// status condition management and cluster updates.
//
// Parameters:
//   - ctx: The context for API calls
//   - logger: Logger for debugging information
//   - conv: The APIConverter that manages the root object and its status
//
// Returns:
//   - bool: true if the status was actually updated in the cluster
//   - error: Any error that occurred during status processing
//
// This is a generic wrapper function that works with any converter implementing
// the APIConverter interface, providing a consistent interface for status enforcement
// across different resource types.
func enforceStatus[t converter.RootObject](ctx context.Context, logger logr.Logger, conv converter.APIConverter[t]) (bool, error) {
	return conv.UpdateRootObjectStatus(ctx, logger)
}

// cleanOrphanedResources deletes resources previously managed by the converter but no longer present in the desired output.
//
// The function performs the following operations:
// 1. Retrieves the current desired state from the converter's output store
// 2. Builds a set of desired resource keys for quick lookup
// 3. For each expected GroupVersionKind, lists existing resources owned by the root object
// 4. Compares existing resources against the desired set and deletes orphans
// 5. Handles deletion errors gracefully, ignoring NotFound errors
//
// This cleanup process ensures that resources that were previously created by the converter
// but are no longer needed (due to configuration changes) are properly removed from the cluster.
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - cl: The Kubernetes client for listing and deleting resources
//   - logger: Logger for debugging and status information
//   - conv: The APIConverter that manages the root object and its desired state
//
// Returns:
//   - bool: true if any orphaned resources were deleted from the cluster
//   - error: Any error that occurred during the cleanup process
//
// The function uses ownership labels to identify resources managed by the root object
// and only deletes resources that are no longer present in the converter's desired output.
func cleanOrphanedResources[t converter.RootObject, tPtr converter.RootObjectPtr[t]](ctx context.Context, cl client.Client, logger logr.Logger, conv converter.APIConverter[t]) (bool, error) {
	logger = logger.WithValues("phase", "orphan-cleanup")
	log.Debug(logger, "Starting orphaned resource cleanup")

	desiredObjects, err := conv.GetOutputStore(ctx, logger)
	if err != nil {
		return false, fmt.Errorf("failed to get desired objects from converter for cleanup: %w", err)
	}

	desiredSet := make(map[string]struct{})
	expectedGVKs := conv.GetExpectedGVKs()

	log.Debug(logger, "Retrieved desired objects and expected GVKs",
		"desiredObjectCount", len(desiredObjects),
		"expectedGVKCount", len(expectedGVKs))

	// Extract the root object for label selector.
	rootObj := conv.GetRootObject()
	var rootObjPtr tPtr
	switch v := any(&rootObj).(type) {
	case tPtr:
		rootObjPtr = v
	default:
		return false, fmt.Errorf("failed to convert root object to pointer type: got %T, expected %T", &rootObj, rootObjPtr)
	}

	// Build a set of desired resource keys.
	log.Debug(logger, "Building desired resource key set")
	for _, obj := range desiredObjects {
		key := fmt.Sprintf("%s/%s/%s", obj.GetNamespace(), obj.GetName(), obj.GetObjectKind().GroupVersionKind().String())
		desiredSet[key] = struct{}{}
		log.Trace(logger, "Added desired resource key", "key", key, "kind", obj.GetKind(), "name", obj.GetName())
	}
	log.Debug(logger, "Finished building desired resource key set", "totalKeys", len(desiredSet))

	// For each expected GVK, list resources and delete orphans.
	totalOrphansDeleted := 0
	for _, gvk := range expectedGVKs {
		log.Debug(logger, "Processing GVK for orphan cleanup", "gvk", gvk.String())

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		selector := metadata.LabelSelectorForOwnedResources(rootObjPtr, nil)

		// List all resources of this GVK owned by the root object in the same namespace.
		ns := rootObjPtr.GetNamespace()

		if err := cl.List(ctx, list, selector, client.InNamespace(ns)); err != nil {
			return false, fmt.Errorf("unable to list objects with gvk %s in namespace %s: %w", gvk.String(), ns, err)
		}

		log.Debug(logger, "Found existing resources for GVK", "gvk", gvk.String(), "resourceCount", len(list.Items))

		orphansForGVK := 0
		for _, item := range list.Items {
			key := fmt.Sprintf("%s/%s/%s", item.GetNamespace(), item.GetName(), gvk.String())
			if _, found := desiredSet[key]; !found {
				// Not in desired output, delete it.
				log.Info(logger, "Deleting orphaned resource", "kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
				if err := cl.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
					return false, fmt.Errorf("failed to delete orphaned resource kind %s obj %s: %w", item.GetKind(), client.ObjectKeyFromObject(&item), err)
				}
				orphansForGVK++
				totalOrphansDeleted++
			} else {
				log.Trace(logger, "Resource still desired, keeping", "kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
			}
		}

		if orphansForGVK > 0 {
			log.Debug(logger, "Deleted orphaned resources for GVK", "gvk", gvk.String(), "orphansDeleted", orphansForGVK)
		} else {
			log.Debug(logger, "No orphaned resources found for GVK", "gvk", gvk.String())
		}
	}

	log.Debug(logger, "Finished orphaned resource cleanup", "totalOrphansDeleted", totalOrphansDeleted)
	// Return true if any orphans were deleted.
	orphansDeleted := totalOrphansDeleted > 0
	return orphansDeleted, nil
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
