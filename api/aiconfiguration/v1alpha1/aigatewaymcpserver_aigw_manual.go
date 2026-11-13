package v1alpha1

// This file hand-translates AIGatewayMCPServer into ai-deck-converter's aigw.MCPServer,
// for on-prem (dbless) config rendering. See aigatewaymodel_aigw_manual.go's package comment
// for the shared background and the plan to generate this some day.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kong/ai-deck-converter/aigw"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToAIGWMCPServer converts the AIGatewayMCPServer into ai-deck-converter's aigw.MCPServer,
// resolving the spec's CR references (policies, the listener's sources, the access block's
// ACL consumer groups and auth strategies, every tool's access ACLs, and the selected
// variant's upstream CA certificates and client certificates) to the referenced entities'
// names, and the config's secretRefs (the session and token-vault Redis passwords and the
// passthrough proxy's basic-auth password) to their Secret values. The config certificate
// references must target the server's own OnPremAIGateway.
func (obj *AIGatewayMCPServer) ToAIGWMCPServer(ctx context.Context, cl client.Client) (*aigw.MCPServer, error) {
	if err := CheckCrossNamespaceSecretRefsGranted(ctx, cl, obj); err != nil {
		return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: %w", obj.Namespace, obj.Name, err)
	}
	if err := CheckCrossNamespaceConsumerGroupRefsGranted(ctx, cl, obj); err != nil {
		return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	// Resolve secretRefs against a copy: sdkOpsAPISpec deep-copies the spec, so the
	// caller's object is not mutated.
	spec, err := obj.sdkOpsAPISpec(ctx, cl)
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s secrets: %w", obj.Namespace, obj.Name, noteSecretLabelRequirement(err))
	}
	if spec.AIGatewayMCPServerConfig == nil {
		return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: spec.apiSpec is required", obj.Namespace, obj.Name)
	}

	// The variant-specific fields the payload decode cannot carry: CR references (stripped
	// from the payload, resolved here) and the access block's reference surface.
	var (
		policies        []AIGatewayPolicyRef
		sources         []AIGatewayMCPServerRef
		tools           any // one of the three generated tool slices, see mcpToolACLRefs
		acls            mcpServerACLRefs
		defaultToolAcls AIGatewayMCPACLs
		authStrategies  []AIGatewayAuthStrategyRef
		caCertificates  []AIGatewayCACertificateRef
		certificates    []AIGatewayCertificateRef
	)
	switch spec.Type {
	case AIGatewayMCPServerConfigTypeConversionOnly:
		if spec.ConversionOnly == nil {
			return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: spec.apiSpec.conversion-only is required for type conversion-only", obj.Namespace, obj.Name)
		}
		policies, tools = spec.ConversionOnly.Policies, spec.ConversionOnly.Tools
		caCertificates, certificates = spec.ConversionOnly.Config.CaCertificates, spec.ConversionOnly.Config.Certificates
	case AIGatewayMCPServerConfigTypeConversionListener:
		if spec.ConversionListener == nil {
			return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: spec.apiSpec.conversion-listener is required for type conversion-listener", obj.Namespace, obj.Name)
		}
		policies, tools = spec.ConversionListener.Policies, spec.ConversionListener.Tools
		acls, defaultToolAcls, authStrategies = mcpAccessRefs(spec.ConversionListener.Access)
		caCertificates, certificates = spec.ConversionListener.Config.CaCertificates, spec.ConversionListener.Config.Certificates
	case AIGatewayMCPServerConfigTypeListener:
		if spec.Listener == nil {
			return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: spec.apiSpec.listener is required for type listener", obj.Namespace, obj.Name)
		}
		// A listener exposes the sources' tools, so it declares no tools of its own.
		policies, sources = spec.Listener.Policies, spec.Listener.Sources
		acls, defaultToolAcls, authStrategies = mcpAccessRefs(spec.Listener.Access)
	case AIGatewayMCPServerConfigTypePassthroughListener:
		if spec.PassthroughListener == nil {
			return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: spec.apiSpec.passthrough-listener is required for type passthrough-listener", obj.Namespace, obj.Name)
		}
		policies, tools = spec.PassthroughListener.Policies, spec.PassthroughListener.Tools
		acls, defaultToolAcls, authStrategies = mcpAccessRefs(spec.PassthroughListener.Access)
		caCertificates, certificates = spec.PassthroughListener.Config.CaCertificates, spec.PassthroughListener.Config.Certificates
	case AIGatewayMCPServerConfigTypeUpstreamServer:
		if spec.UpstreamServer == nil {
			return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: spec.apiSpec.upstream-server is required for type upstream-server", obj.Namespace, obj.Name)
		}
		policies, tools = spec.UpstreamServer.Policies, spec.UpstreamServer.Tools
		caCertificates, certificates = spec.UpstreamServer.Config.CaCertificates, spec.UpstreamServer.Config.Certificates
	default:
		return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: unsupported spec.apiSpec.type %q", obj.Namespace, obj.Name, spec.Type)
	}

	data, err := spec.marshalAIGWMCPServerPayload()
	if err != nil {
		return nil, fmt.Errorf("marshaling AIGatewayMCPServer %s/%s: %w", obj.Namespace, obj.Name, err)
	}

	var mcp aigw.MCPServer
	if err := yaml.Unmarshal(data, &mcp); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayMCPServer %s/%s as aigw.MCPServer: %w", obj.Namespace, obj.Name, err)
	}

	if mcp.Policies, err = resolveEntityNames[AIGatewayPolicy](ctx, cl, obj.Namespace, policyRefs(policies)); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s policies: %w", obj.Namespace, obj.Name, err)
	}

	if len(sources) > 0 {
		names, err := resolveEntityNames[AIGatewayMCPServer](ctx, cl, obj.Namespace, mcpServerRefs(sources))
		if err != nil {
			return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s sources: %w", obj.Namespace, obj.Name, err)
		}
		mcp.Config.Sources = names
	}

	if mcp.Config.CACertificates, err = resolveEntityNamesTargeting[AIGatewayCACertificate](
		ctx, cl, obj.Namespace, obj.Spec.AIGatewayRef, caCertificateRefs(caCertificates),
	); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s caCertificates: %w", obj.Namespace, obj.Name, err)
	}
	if mcp.Config.Certificates, err = resolveEntityNamesTargeting[AIGatewayCertificate](
		ctx, cl, obj.Namespace, obj.Spec.AIGatewayRef, certificateRefs(certificates),
	); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s certificates: %w", obj.Namespace, obj.Name, err)
	}

	// The access union's reference surface. Resolved current-key refs come
	// first, mirroring AIGatewayModel.
	if mcp.Access.ACLs.Allow, err = resolveEntityNamesCrossNamespace[AIGatewayConsumerGroup](ctx, cl, obj.Namespace, mcpACLRefs(acls.allow)); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s access.acls.allow: %w", obj.Namespace, obj.Name, err)
	}
	if mcp.Access.ACLs.Deny, err = resolveEntityNamesCrossNamespace[AIGatewayConsumerGroup](ctx, cl, obj.Namespace, mcpACLRefs(acls.deny)); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s access.acls.deny: %w", obj.Namespace, obj.Name, err)
	}
	if mcp.Access.DefaultToolACLs.Allow, err = resolveEntityNamesCrossNamespace[AIGatewayConsumerGroup](ctx, cl, obj.Namespace, mcpACLRefs(defaultToolAcls.Allow)); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s access.defaultToolAcls.allow: %w", obj.Namespace, obj.Name, err)
	}
	if mcp.Access.DefaultToolACLs.Deny, err = resolveEntityNamesCrossNamespace[AIGatewayConsumerGroup](ctx, cl, obj.Namespace, mcpACLRefs(defaultToolAcls.Deny)); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s access.defaultToolAcls.deny: %w", obj.Namespace, obj.Name, err)
	}
	authStrategyNames, err := resolveEntityNames[AIGatewayAuthStrategy](ctx, cl, obj.Namespace, authStrategyRefs(authStrategies))
	if err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s access.authStrategies: %w", obj.Namespace, obj.Name, err)
	}
	mcp.Access.AuthStrategies = append(authStrategyNames, mcp.Access.AuthStrategies...)

	if err := resolveDatastoreRefs(ctx, cl, obj.Namespace, AIGatewayMCPServerRefsToAIGatewayDatastore(obj), &mcp.Datastores); err != nil {
		return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s datastores: %w", obj.Namespace, obj.Name, err)
	}

	// Per-tool access ACLs, resolved in payload order.
	toolACLs := mcpToolACLRefs(tools)
	if len(toolACLs) != len(mcp.Tools) {
		return nil, fmt.Errorf("AIGatewayMCPServer %s/%s: decoded %d tools, expected %d", obj.Namespace, obj.Name, len(mcp.Tools), len(toolACLs))
	}
	for i := range mcp.Tools {
		if mcp.Tools[i].Access.ACLs.Allow, err = resolveEntityNamesCrossNamespace[AIGatewayConsumerGroup](ctx, cl, obj.Namespace, mcpACLRefs(toolACLs[i].Allow)); err != nil {
			return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s tools[%d].access.acls.allow: %w", obj.Namespace, obj.Name, i, err)
		}
		if mcp.Tools[i].Access.ACLs.Deny, err = resolveEntityNamesCrossNamespace[AIGatewayConsumerGroup](ctx, cl, obj.Namespace, mcpACLRefs(toolACLs[i].Deny)); err != nil {
			return nil, fmt.Errorf("resolving AIGatewayMCPServer %s/%s tools[%d].access.acls.deny: %w", obj.Namespace, obj.Name, i, err)
		}
	}

	return &mcp, nil
}

// mcpAccessRefs projects a listener-family variant's access union onto its reference fields.
// Only the conversion-listener, listener, and passthrough-listener variants declare access;
// a conversion-only server serves no MCP traffic of its own and an upstream-server proxies
// verbatim, so neither can (the CRD has no access field for them). The union's consumer and
// oauthAccessToken inner types carry the same ACL and auth-strategy surface as distinct
// generated structs with no shared interface, hence the switches. A nil pointer (typed or
// untyped) means no access.
func mcpAccessRefs(
	access any,
) (acls mcpServerACLRefs, defaultToolAcls AIGatewayMCPACLs, authStrategies []AIGatewayAuthStrategyRef) {
	switch a := access.(type) {
	case *AIGatewayMCPServerConversionListenerAccess:
		if a != nil {
			return mcpAccessInnerRefs(a.Consumer, a.Oauth)
		}
	case *AIGatewayMCPServerListenerAccess:
		if a != nil {
			return mcpAccessInnerRefs(a.Consumer, a.Oauth)
		}
	case *AIGatewayMCPServerPassthroughListenerAccess:
		if a != nil {
			return mcpAccessInnerRefs(a.Consumer, a.Oauth)
		}
	}
	return
}

// mcpAccessInnerRefs extracts the shared reference surface of the access union's inner
// variants.
func mcpAccessInnerRefs(
	consumer *AIGatewayMCPServerListenerConsumer,
	oauth *AIGatewayMCPServerListenerOauth,
) (acls mcpServerACLRefs, defaultToolAcls AIGatewayMCPACLs, authStrategies []AIGatewayAuthStrategyRef) {
	switch {
	case consumer != nil:
		return mcpServerACLs(consumer.Acls), consumer.DefaultToolAcls, consumer.AuthStrategies
	case oauth != nil:
		return mcpServerACLs(oauth.Acls), oauth.DefaultToolAcls, oauth.AuthStrategies
	}
	return
}

// mcpServerACLRefs holds the refs of the access union's selected server-level ACL
// variant. The union allows exactly one of allow/deny/allow_when/deny_when; only
// allow and deny carry references.
type mcpServerACLRefs struct {
	allow, deny []AIGatewayACLRef //nolint:kubeapilinter
}

// mcpServerACLs projects an access union onto its selected variant's refs. The
// consumer and oauth arms are distinct generated structs with the same shape.
func mcpServerACLs(acls any) mcpServerACLRefs {
	switch a := acls.(type) {
	case *AIGatewayMCPServerListenerConsumerAcls:
		if a != nil {
			return mcpServerACLRefs{allow: a.Allow, deny: a.Deny}
		}
	case *AIGatewayMCPServerListenerOauthAcls:
		if a != nil {
			return mcpServerACLRefs{allow: a.Allow, deny: a.Deny}
		}
	}
	return mcpServerACLRefs{}
}

// mcpToolACLRefs extracts each tool's access.acls for resolution, in payload order. The
// conversion, passthrough, and upstream tool shapes carry the same AIGatewayMCPToolAccess
// as distinct generated structs with no shared interface, hence the switch.
func mcpToolACLRefs(tools any) []AIGatewayMCPACLs {
	switch ts := tools.(type) {
	case []AIGatewayMCPConversionTool:
		out := make([]AIGatewayMCPACLs, len(ts))
		for i := range ts {
			out[i] = ts[i].Access.Acls
		}
		return out
	case []AIGatewayMCPPassthroughTool:
		out := make([]AIGatewayMCPACLs, len(ts))
		for i := range ts {
			out[i] = ts[i].Access.Acls
		}
		return out
	case []AIGatewayMCPUpstreamTool:
		out := make([]AIGatewayMCPACLs, len(ts))
		for i := range ts {
			out[i] = ts[i].Access.Acls
		}
		return out
	}
	return nil
}

// mcpACLRefs and mcpServerRefs project the MCP-specific reference types onto the common
// shape resolveEntityNames needs, like policyRefs/authStrategyRefs/consumerGroupRefs in
// aigatewaymodel_aigw_manual.go.
func mcpACLRefs(refs []AIGatewayACLRef) []namespacedRef {
	out := make([]namespacedRef, len(refs))
	for i, r := range refs {
		out[i] = namespacedRef{Name: r.Name, Namespace: r.Namespace}
	}
	return out
}

func mcpServerRefs(refs []AIGatewayMCPServerRef) []namespacedRef {
	out := make([]namespacedRef, len(refs))
	for i, r := range refs {
		out[i] = namespacedRef{Name: r.Name, Namespace: r.Namespace}
	}
	return out
}

// marshalAIGWMCPServerPayload builds the snake_case JSON bytes for the selected config
// variant, shaped for direct yaml.Unmarshal into an aigw.MCPServer. It reuses
// marshalSDKOpsPayload/selectedSDKOpsPayload (generated for the Konnect SDK payload, which
// needs the same camelCase->snake_case key and discriminator-value renaming, bool-enum
// normalization, and union flattening) and edits the flattened variant — editing after the
// flatten, not before, so every key sits at its final position (the access block is a union
// nested inside the variant, and its member fields only land at the access top level during
// the flatten):
//
//   - drops the Konnect-only managed_by bookkeeping,
//   - strips the CR-reference keys (policies, sources, access.acls,
//     access.default_tool_acls, access.auth_strategies, tools[].access.acls,
//     config.ca_certificates, config.certificates: they carry {kind,name} references,
//     aigw wants plain resolved names, re-attached in ToAIGWMCPServer). The deprecated
//     access.identity_providers is not supported: aigw rejects the key, so a CR carrying
//     it fails translation,
//   - moves the variant's config.url to the top-level upstream_url the aigw document
//     carries it under (the listener variant has no config.url and needs no move),
//   - drops config.cache and config.allowed_versions: they have no aigw slot, the on-prem
//     document format cannot express them.
func (spec *AIGatewayMCPServerAPISpec) marshalAIGWMCPServerPayload() ([]byte, error) {
	payload, err := spec.marshalSDKOpsPayload()
	if err != nil {
		return nil, err
	}
	data, _, err := spec.selectedSDKOpsPayload(payload)
	if err != nil {
		return nil, err
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decoding AIGatewayMCPServer SDK payload: %w", err)
	}

	// Konnect-only bookkeeping: no on-prem equivalent.
	delete(cfg, "managed_by")

	// These carry {kind,name} CR references; aigw wants plain resolved names. Stripped
	// here, re-attached resolved in ToAIGWMCPServer.
	delete(cfg, "policies")
	delete(cfg, "sources")
	delete(cfg, "datastores")
	if a, ok := cfg["access"].(map[string]any); ok {
		delete(a, "auth_strategies")
		delete(a, "acls")
		delete(a, "default_tool_acls")
	}
	if ts, ok := cfg["tools"].([]any); ok {
		for _, t := range ts {
			if tm, ok := t.(map[string]any); ok {
				if a, ok := tm["access"].(map[string]any); ok {
					delete(a, "acls")
				}
			}
		}
	}

	if c, ok := cfg["config"].(map[string]any); ok {
		if url, ok := c["url"]; ok {
			delete(c, "url")
			cfg["upstream_url"] = url
		}
		delete(c, "cache")
		delete(c, "allowed_versions")
		delete(c, "ca_certificates")
		delete(c, "certificates")
	}

	return json.Marshal(cfg)
}
