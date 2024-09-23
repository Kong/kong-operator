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
func ReconciliationIndexOptionsForEntity[
	TEnt constraints.EntityType[T],
	T constraints.SupportedKonnectEntityType,
]() []ReconciliationIndexOption {
	var e TEnt
	switch any(e).(type) {
	case *configurationv1alpha1.KongPluginBinding:
		return IndexOptionsForKongPluginBinding()
	case *configurationv1alpha1.KongCredentialBasicAuth:
		return IndexOptionsForCredentialsBasicAuth()
	}
	return nil
}
