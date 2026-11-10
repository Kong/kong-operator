package generator

import (
	"go/format"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func TestGenerateProducePolicyChainScopeWatch(t *testing.T) {
	const entity = "EventGatewayVirtualClusterProducePolicyChain"
	const policy = "EventGatewayVirtualClusterProducePolicy"
	for _, tt := range []struct {
		name   string
		change func(*config.ReconcilerConfig)
		err    string
	}{
		{name: "same scope"},
		{name: "wrong parent kind", change: func(c *config.ReconcilerConfig) { c.ParentEntityGVK.Kind = "EventGatewayListener" }, err: "matching configured parent"},
		{name: "wrong parent group", change: func(c *config.ReconcilerConfig) { c.ParentEntityGVK.Group = "other" }, err: "matching configured parent"},
		{name: "wrong parent version", change: func(c *config.ReconcilerConfig) { c.ParentEntityGVK.Version = "v2" }, err: "matching configured parent"},
		{name: "custom parent", change: func(c *config.ReconcilerConfig) { c.ParentRef.TypeName = "CustomRef" }, err: "ObjectRef parents"},
		{name: "missing parent", change: func(c *config.ReconcilerConfig) { c.ParentRef = nil }, err: "ObjectRef parents"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parsed := &parser.ParsedSpec{RequestBodies: map[string]*parser.Schema{
				entity: {Properties: []*parser.Property{{Name: "policies", Type: "array", Items: &parser.Property{Type: "string"}}}},
			}}
			g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
				entity: {{Path: "spec.apiSpec.policies", Kinds: []string{policy}, ResolvesTo: "id", WatchSameParent: true}},
			})
			g.config.APIGroupPackagePath = "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
			g.config.APIGroupPackageAlias = "configurationv1alpha1"
			parent := &config.ReconcilerConfig{IsRoot: new(false),
				ParentEntityGVK: &config.EntityGVKConfig{Kind: "EventGatewayVirtualCluster"},
				ParentRef:       &config.ParentRefConfig{FieldName: "eventGatewayVirtualClusterRef"},
			}
			target := &config.ReconcilerConfig{IsRoot: new(false),
				ParentEntityGVK: &config.EntityGVKConfig{Kind: "EventGatewayVirtualCluster"},
				ParentRef:       &config.ParentRefConfig{FieldName: "eventGatewayVirtualClusterRef"},
			}
			if tt.change != nil {
				tt.change(target)
			}
			g.config.ReconcilerConfig = map[string]*config.ReconcilerConfig{entity: parent, policy: target}
			content, err := g.generateWatch(reconcilerEntityMetadata{
				EntityName: entity, ParentEntityName: "EventGatewayVirtualCluster",
				APIGroupPackagePath: g.config.APIGroupPackagePath, APIGroupPackageAlias: g.config.APIGroupPackageAlias,
				ParentAPIGroupPackagePath: g.config.APIGroupPackagePath, ParentAPIGroupPackageAlias: g.config.APIGroupPackageAlias,
			}, parent)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			_, err = format.Source([]byte(content))
			require.NoError(t, err)
			require.Equal(t, 1, strings.Count(content, "func enqueue"+entity+"For"+policy+"("))
			require.Contains(t, content, "index.IndexField"+entity+"OnEventGatewayVirtualClusterRef: ns")
			require.Contains(t, content, "index.IndexField"+entity+"On"+policy+"Ref:")
			require.Contains(t, content, "ref.GetParentRef()")
		})
	}
}
