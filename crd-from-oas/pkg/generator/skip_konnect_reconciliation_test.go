package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
)

func TestKindsUnderAncestorSkippingKonnectReconciliation(t *testing.T) {
	onPrem := &config.GatewayRefConfig{AdditionalKinds: []config.EntityGVKConfig{{Kind: "OnPremEventGateway"}}}
	g := NewGenerator(Config{ReconcilerConfig: map[string]*config.ReconcilerConfig{
		"Backend":        {GatewayRef: onPrem},
		"VirtualCluster": {ParentEntityGVK: &config.EntityGVKConfig{Kind: "Backend"}},
		"Policy":         {ParentEntityGVK: &config.EntityGVKConfig{Kind: "VirtualCluster"}},
		"KonnectOnly":    {ParentEntityGVK: &config.EntityGVKConfig{Kind: "Gateway"}},
		// A parent cycle must not loop forever.
		"Loop": {ParentEntityGVK: &config.EntityGVKConfig{Kind: "Loop"}},
	}})

	assert.Equal(t, map[string]bool{
		"VirtualCluster": true,
		"Policy":         true,
	}, g.kindsUnderAncestorSkippingKonnectReconciliation([]string{"Backend", "VirtualCluster", "Policy", "KonnectOnly", "Loop", "Unknown"}),
		"only kinds without their own SkipKonnectReconciliation, under one that has it",
	)
}
