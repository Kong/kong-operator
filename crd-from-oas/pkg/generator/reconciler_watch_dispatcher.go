package generator

import "github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"

// WatchFileInfo is metadata returned from generateReconcilerFiles so the
// caller (Runner) can assemble the cross-group watch dispatcher.
type WatchFileInfo struct {
	Entity         string // PascalCase entity name, e.g. "Portal"
	APIAlias       string // Go import alias, e.g. "konnectv1alpha1"
	APIPackagePath string // Go import path for the API types package
	IsRoot         bool   // Whether the entity directly references KonnectAPIAuthConfiguration
	Group          string // API group of the entity, e.g. "configuration.konghq.com"
	ParentKind     string // Kind of the parent entity, empty when the entity has none
	ParentGroup    string // API group of the parent entity
	// Gateway kinds the entity's gatewayRef accepts besides the parent kind, e.g. OnPremEventGateway
	GatewayKinds []config.EntityGVKConfig
}

// GenerateWatchDispatcher emits controller/konnect/zz_generated_watch.go with
// reconciliationWatchOptionsForEntity[T,TEnt]. Call after all per-group
// generation has finished.
func GenerateWatchDispatcher(infos []*WatchFileInfo) (*GeneratedFile, error) {
	flat := make([]flatInfo, 0, len(infos))
	for _, info := range infos {
		flat = append(flat, flatInfo{
			Entity:         info.Entity,
			APIAlias:       info.APIAlias,
			APIPackagePath: info.APIPackagePath,
		})
	}
	return buildDispatcherFile("zz_generated_watch.go", watchDispatcherTemplate, "controller/konnect", flat)
}
