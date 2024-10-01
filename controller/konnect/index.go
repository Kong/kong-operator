package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconciliationIndexOption contains required options of index for a kind of object required for reconciliation.
type ReconciliationIndexOption struct {
	IndexObject  client.Object
	IndexField   string
	ExtractValue client.IndexerFunc
}
