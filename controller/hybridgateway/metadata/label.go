package metadata

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// IsHybridGatewayService identifies route-generated Services without treating external
// KongCertificates as owned resources.
func IsHybridGatewayService(service *configurationv1alpha1.KongService) bool {
	kind := service.Labels[consts.GatewayOperatorManagedByLabel]
	return HybridRouteAnnotationKeyForKind(kind) != "" &&
		service.Annotations[consts.GatewayOperatorHybridGatewaysAnnotation] != ""
}

// BuildLabels creates the standard labels map for Kong resources managed by the given object and ParentReference.
// It generates labels that identify which route and gateway the Kong resources belong to, enabling
// proper resource tracking and cleanup.
func BuildLabels(obj client.Object, pRef *gwtypes.ParentReference) map[string]string {
	return buildManagedByLabels(obj, pRef)
}

// buildManagedByLabels returns the identifying labels for resources managed by a given object and its ParentReference.
// The labels include information about both the managing route and the target gateway.
func buildManagedByLabels(obj client.Object, pRef *gwtypes.ParentReference) map[string]string {
	// If no ParentReference is provided, only include labels for the managing object.
	if pRef == nil {
		return map[string]string{
			consts.GatewayOperatorManagedByLabel: obj.GetObjectKind().GroupVersionKind().Kind,
		}
	}

	return map[string]string{
		consts.GatewayOperatorManagedByLabel: obj.GetObjectKind().GroupVersionKind().Kind,
	}
}

// LabelSelectorForOwnedResources returns a label selector for listing resources managed by the given object and ParentReference.
// This can be used to find all Kong resources associated with a specific route-gateway pair.
func LabelSelectorForOwnedResources(obj client.Object, pRef *gwtypes.ParentReference) client.ListOption {
	return client.MatchingLabels(buildManagedByLabels(obj, pRef))
}
