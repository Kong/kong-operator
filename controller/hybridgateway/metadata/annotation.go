package metadata

import (
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"

	routeconst "github.com/kong/kong-operator/controller/hybridgateway/const/route"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

const (
	// Annotation constants matching those in the ingress controller
	annotationPrefix = "konghq.com"
	stripPathKey     = "/strip-path"
)

// ExtractStripPath extracts the strip-path annotation value and returns a boolean.
// Returns true by default if the annotation is not present or cannot be parsed.
func ExtractStripPath(anns map[string]string) bool {
	if anns == nil {
		return true
	}

	val := anns[annotationPrefix+stripPathKey]
	if val == "" {
		return true // Default to true when not specified
	}

	stripPath, err := strconv.ParseBool(val)
	if err != nil {
		return true // Default to true when invalid value
	}

	return stripPath
}

// BuildAnnotations creates the standard annotations map for Kong resources managed by HTTPRoute.
func BuildAnnotations(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) map[string]string {
	gwObjKey := client.ObjectKey{
		Name: string(parentRef.Name),
	}
	if parentRef.Namespace != nil && *parentRef.Namespace != "" {
		gwObjKey.Namespace = string(*parentRef.Namespace)
	} else {
		gwObjKey.Namespace = route.GetNamespace()
	}

	return map[string]string{
		consts.GatewayOperatorHybridRouteAnnotation:    routeconst.HTTPRouteKey + "|" + client.ObjectKeyFromObject(route).String(),
		consts.GatewayOperatorHybridGatewaysAnnotation: gwObjKey.String(),
	}
}
