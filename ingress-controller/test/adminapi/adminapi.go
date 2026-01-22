package adminapi

import (
	internal "github.com/kong/kong-operator/ingress-controller/internal/adminapi"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Discoverer = internal.Discoverer
type DiscoveredAdminAPI = internal.DiscoveredAdminAPI

func NewDiscoverer(adminAPIPortNames sets.Set[string]) (*Discoverer, error) {
	return internal.NewDiscoverer(adminAPIPortNames)
}
