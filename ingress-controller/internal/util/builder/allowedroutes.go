package builder

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func NewAllowedRoutesFromSameNamespaces() *gwtypes.AllowedRoutes {
	return &gwtypes.AllowedRoutes{
		Namespaces: NewRouteNamespaces().FromSame().Build(),
	}
}

func NewAllowedRoutesFromAllNamespaces() *gwtypes.AllowedRoutes {
	return &gwtypes.AllowedRoutes{
		Namespaces: NewRouteNamespaces().FromAll().Build(),
	}
}

func NewAllowedRoutesFromSelectorNamespace(selector *metav1.LabelSelector) *gwtypes.AllowedRoutes {
	return &gwtypes.AllowedRoutes{
		Namespaces: NewRouteNamespaces().FromSelector(selector).Build(),
	}
}
