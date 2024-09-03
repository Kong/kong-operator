package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/constraints"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// ReconciliationIndexOption contains required options of index for a kind of object required for reconciliation.
type ReconciliationIndexOption struct {
	IndexObject  client.Object
	IndexField   string
	ExtractValue client.IndexerFunc
}

// ReconciliationIndexOptionsForEntity returns required index options for controller reconciliing the entity.
func ReconciliationIndexOptionsForEntity[T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T]](ent TEnt) []ReconciliationIndexOption {
	switch any(ent).(type) { //nolint:gocritic // TODO: add index options required for other entities
	case *configurationv1alpha1.KongPluginBinding:
		return IndexOptionsForKongPluginBinding()
	}
	return nil
}
