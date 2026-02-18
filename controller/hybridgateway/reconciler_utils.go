package hybridgateway

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/converter"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ownedResourcesWithHits is a struct that holds an unstructured resource and the number of times it has been accessed.
type ownedResourcesWithHits struct {
	resources []unstructured.Unstructured
	hits      int
}

// Translate performs the full translation process using the provided APIConverter.
func Translate[t converter.RootObject](conv converter.APIConverter[t], ctx context.Context) error {
	return conv.Translate()
}

// EnforceState ensures that the actual state of resources in the cluster matches the desired state
// defined by the provided APIConverter. It creates missing resources, marks obsolete or duplicate
// resources for deletion, and returns whether a requeue is needed along with any error encountered.
// The function is generic over types implementing converter.RootObject.
func EnforceState[t converter.RootObject](ctx context.Context, cl client.Client, logger logr.Logger, conv converter.APIConverter[t]) (requeue bool, stop bool, err error) {
	store := conv.GetOutputStore(ctx)
	rootObject := conv.GetRootObject()

	resources, err := conv.ListExistingObjects(ctx)
	if err != nil {
		return false, false, err
	}

	// Convert rootObject to client.Object using its pointer type
	rootObjectPtr, ok := any(&rootObject).(client.Object)
	if !ok {
		return false, false, fmt.Errorf("failed to convert rootObject to client.Object")
	}

	// Create a map of the owned resources using the hash spec as index.
	ownedResourceMap := mapOwnedResources(rootObjectPtr, resources)
	for _, expectedObject := range store {
		expectedHash := expectedObject.GetLabels()[consts.GatewayOperatorHashSpecLabel]
		existingObject, found := ownedResourceMap[expectedHash]
		if found {
			existingObject.hits++
		} else {
			if err := cl.Create(ctx, &expectedObject); err != nil {
				return false, false, err
			}
			return false, true, nil
		}

		// TODO: ensure the spec is up to date. This print is meant
		// to act as a placeholder for the actual update logic.
		// https://github.com/Kong/kong-operator/issues/2171
		logger.Info("TODO: ensure the spec is up to date for", "name", expectedObject.GetName())
	}

	resourcesToDelete := make([]unstructured.Unstructured, 0)
	for _, v := range ownedResourceMap {
		// mark for deletion all the resources with no hits.
		if v.hits == 0 {
			resourcesToDelete = append(resourcesToDelete, v.resources...)
		}
		// mark for deletion all the resources with duplicates
		resourcesToDelete = append(resourcesToDelete, reduceDuplicates(v.resources, conv.Reduce(v.resources[0])...)...)
	}

	// delete all the resources marked for deletion
	for _, resource := range resourcesToDelete {
		if err := cl.Delete(ctx, &resource); err != nil {
			return false, false, err
		}
	}
	if stop {
		return false, true, nil
	}

	if err := conv.UpdateSharedRouteStatus(resources); err != nil {
		return false, false, err
	}

	return false, false, nil
}

// reduceDuplicates applies a series of reducer functions to a slice of unstructured resources,
// identifying and collecting duplicates for deletion. It returns a slice of resources that should be deleted.
func reduceDuplicates(resources []unstructured.Unstructured, fns ...utils.ReduceFunc) []unstructured.Unstructured {
	resourcesToDelete := make([]unstructured.Unstructured, 0)
	if len(resources) > 1 {
		// we can safely assume that all the resources here share the same GVK, as
		// they have the same spec hash. So, let's pass the first resource to the reducer as a placeholder.
		for _, fn := range fns {
			resourcesToDelete = append(resourcesToDelete, fn(resources)...)
			resources = lo.Filter(resources, func(r unstructured.Unstructured, _ int) bool {
				return !lo.ContainsBy(resourcesToDelete, func(item unstructured.Unstructured) bool {
					return item.GetNamespace() == r.GetNamespace() && item.GetName() == r.GetName()
				})
			})
		}
	}
	return resourcesToDelete
}

// mapOwnedResources filters and groups a slice of unstructured Kubernetes resources by their owner reference and a specific hash label.
// It returns a map where the key is the hash label and the value is a pointer to ownedResourcesWithHits containing the matching resources.
func mapOwnedResources(owner client.Object, resources []unstructured.Unstructured) map[string]*ownedResourcesWithHits {
	ownerRef := k8sutils.GenerateOwnerReferenceForObject(owner)
	result := make(map[string]*ownedResourcesWithHits)
	for _, r := range resources {
		if !hasOwnerRef(r, ownerRef) {
			continue
		}
		labels := r.GetLabels()
		hashLabel, ok := labels[consts.GatewayOperatorHashSpecLabel]
		if len(labels) == 0 || !ok || hashLabel == "" {
			continue
		}
		if result[hashLabel] == nil {
			result[hashLabel] = &ownedResourcesWithHits{
				resources: []unstructured.Unstructured{},
				hits:      0,
			}
		}
		resources := result[hashLabel].resources
		resources = append(resources, r)
		result[hashLabel] = &ownedResourcesWithHits{
			resources: resources,
		}
	}
	return result
}

// hasOwnerRef checks if the given unstructured resource has an owner reference matching the specified OwnerReference.
// Returns true if a matching owner reference is found, otherwise false.
func hasOwnerRef(r unstructured.Unstructured, ownerRef metav1.OwnerReference) bool {
	refs, found, err := unstructured.NestedSlice(r.Object, "metadata", "ownerReferences")
	if !found || err != nil {
		return false
	}
	for _, ref := range refs {
		refMap, ok := ref.(map[string]any)
		if !ok {
			continue
		}
		if refMap["uid"] == string(ownerRef.UID) &&
			refMap["kind"] == ownerRef.Kind &&
			refMap["name"] == ownerRef.Name &&
			refMap["apiVersion"] == ownerRef.APIVersion {
			return true
		}
	}
	return false
}
