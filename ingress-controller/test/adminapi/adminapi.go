package adminapi

import (
	"k8s.io/apimachinery/pkg/util/sets"

	internal "github.com/kong/kong-operator/v2/ingress-controller/internal/adminapi"
)

type Discoverer = internal.Discoverer
type DiscoveredAdminAPI = internal.DiscoveredAdminAPI

func NewDiscoverer(adminAPIPortNames sets.Set[string]) (*Discoverer, error) {
	return internal.NewDiscoverer(adminAPIPortNames)
}
