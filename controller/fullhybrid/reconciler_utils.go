package fullhybrid

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/fullhybrid/converter"
	"github.com/kong/kong-operator/controller/fullhybrid/utils"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// ownedResourcesWithHits is a struct that holds an unstructured resource and the number of times it has been accessed.
type ownedResourcesWithHits struct {
	resources []unstructured.Unstructured
	hits      int
}

// EnforceState ensures that the actual state of resources in the cluster matches the desired state
// defined by the provided APIConverter. It creates missing resources, marks obsolete or duplicate
// resources for deletion, and returns whether a requeue is needed along with any error encountered.
// The function is generic over types implementing converter.RootObject.
func EnforceState[t converter.RootObject](ctx context.Context, cl client.Client, conv converter.APIConverter[t]) (requeue bool, err error) {
	store := conv.GetStore(ctx)
	rootObject := conv.GetRootObject()

	// Get the resources owned by the root object
	resources, err := conv.ListExistingObjects(ctx)
	if err != nil {
		return true, err
	}

	// Create a map of the owned resources using the hash spec as index.
	ownedResourceMap := mapOwnedResources(rootObject, resources)
	for _, expectedObject := range store {
		expectedHash := expectedObject.GetLabels()[consts.GatewayOperatorHashSpecLabel]
		existingObject, found := ownedResourceMap[expectedHash]
		if found {
			existingObject.hits++
		} else {
			if err := cl.Create(ctx, &expectedObject); err != nil {
				return true, err
			}
		}

		// TODO: ensure the spec is up to date. This print is meant
		// to act as a placeholder for the actual update logic.
		// https://github.com/Kong/kong-operator/issues/2171
		fmt.Println(existingObject)
	}

	resourcesToDelete := make([]unstructured.Unstructured, 0)
	for _, v := range ownedResourceMap {
		// mark for deletion all the resources with no hits.
		if v.hits == 0 {
			resourcesToDelete = append(resourcesToDelete, v.resources...)
		}
		// mark for deletion all the resources with duplicates
		reduceDuplicates(v.resources, conv.Reduce(v.resources[0])...)
	}

	// delete all the resources marked for deletion
	for _, resource := range resourcesToDelete {
		if err := cl.Delete(ctx, &resource); err != nil {
			return true, err
		}
	}

	return false, nil
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
