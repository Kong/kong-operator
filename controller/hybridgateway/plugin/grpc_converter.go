package plugin

import (
	"encoding/json"
	"errors"
	"fmt"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// translateGRPCFromFilter translates a GRPCRouteFilter into one or more KongPlugin
// configurations. The generated KongPlugin(s) are filled with the pluginName and json config
// only, leaving to the caller the responsibility to set metadata (name, namespace, labels,
// annotations) as needed.
//
// Unlike HTTPRoute (translateRuleFilters), the caller does not need to merge configs produced by
// multiple filters within a rule into a single KongPlugin: GRPCRouteRule's Filters are
// CEL-validated to at most one RequestHeaderModifier and at most one ResponseHeaderModifier per
// rule, and GRPCRouteFilter has no URLRewrite/RequestRedirect (those are HTTPRoute-only filter
// types that additionally map to request-transformer there). So no two filters in a GRPCRoute
// rule can ever target the same Kong plugin type, and each filter here maps 1:1 to its own
// KongPlugin.
//
// Supported filter types and their corresponding Kong pluginConfs:
//   - GRPCRouteFilterRequestHeaderModifier -> request-transformer
//   - GRPCRouteFilterResponseHeaderModifier -> response-transformer
func translateGRPCFromFilter(filter gatewayv1.GRPCRouteFilter) ([]kongPluginConfig, error) {
	pluginConfs := []kongPluginConfig{}

	switch filter.Type {
	case gatewayv1.GRPCRouteFilterRequestHeaderModifier:
		pConf := kongPluginConfig{name: pluginRequestTransformer}

		config, err := translateGRPCRequestModifier(filter.RequestHeaderModifier)
		if err != nil {
			return nil, fmt.Errorf("translating RequestHeaderModifier filter: %w", err)
		}
		configJSON, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %q plugin config: %w", pConf.name, err)
		}
		pConf.config = configJSON
		pluginConfs = append(pluginConfs, pConf)
	case gatewayv1.GRPCRouteFilterResponseHeaderModifier:
		pConf := kongPluginConfig{name: pluginResponseTransformer}

		config, err := translateGRPCResponseModifier(filter.ResponseHeaderModifier)
		if err != nil {
			return nil, fmt.Errorf("translating ResponseHeaderModifier filter: %w", err)
		}
		configJSON, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %q plugin config: %w", pConf.name, err)
		}
		pConf.config = configJSON
		pluginConfs = append(pluginConfs, pConf)
	default:
		// TODO: translate GRPCRouteFilterRequestMirror into a Kong request-mirror-equivalent plugin.
		return nil, fmt.Errorf("unsupported filter type: %s", filter.Type)
	}
	return pluginConfs, nil
}

// translateGRPCRequestModifier mirrors translateRequestModifier for GRPCRoute's
// RequestHeaderModifier filter, which shares the same HTTPHeaderFilter config type as HTTPRoute's.
func translateGRPCRequestModifier(hf *gatewayv1.HTTPHeaderFilter) (transformerData, error) {
	plugin := transformerData{}

	if hf == nil {
		return plugin, errors.New("RequestHeaderModifier filter config is missing")
	}

	// In order to overwrite an header of add if not present (GWAPI Set) we should do a Kong Plugin
	// Replace (so it will overwrite it if found) + Add (so if not found, will add it).
	for _, v := range hf.Set {
		plugin.Replace.Headers = append(plugin.Replace.Headers, string(v.Name)+":"+v.Value)
		plugin.Add.Headers = append(plugin.Add.Headers, string(v.Name)+":"+v.Value)
	}
	// Add for GWAPI equals "append" for Kong Plugins (it will add another instance of the header).
	for _, v := range hf.Add {
		plugin.Append.Headers = append(plugin.Append.Headers, string(v.Name)+":"+v.Value)
	}
	if len(hf.Remove) > 0 {
		plugin.Remove.Headers = append(plugin.Remove.Headers, hf.Remove...)
	}

	if len(plugin.Add.Headers)+len(plugin.Append.Headers)+
		len(plugin.Remove.Headers)+len(plugin.Replace.Headers) == 0 {
		return transformerData{}, errors.New("RequestHeaderModifier filter config is empty")
	}
	return plugin, nil
}

// translateGRPCResponseModifier mirrors translateResponseModifier for GRPCRoute's
// ResponseHeaderModifier filter, which shares the same HTTPHeaderFilter config type as HTTPRoute's.
func translateGRPCResponseModifier(hf *gatewayv1.HTTPHeaderFilter) (transformerData, error) {
	plugin := transformerData{}

	if hf == nil {
		return plugin, errors.New("ResponseHeaderModifier filter config is missing")
	}

	for _, v := range hf.Set {
		plugin.Replace.Headers = append(plugin.Replace.Headers, string(v.Name)+":"+v.Value)
		plugin.Add.Headers = append(plugin.Add.Headers, string(v.Name)+":"+v.Value)
	}
	for _, v := range hf.Add {
		plugin.Append.Headers = append(plugin.Append.Headers, string(v.Name)+":"+v.Value)
	}
	if len(hf.Remove) > 0 {
		plugin.Remove.Headers = append(plugin.Remove.Headers, hf.Remove...)
	}

	if len(plugin.Add.Headers)+len(plugin.Append.Headers)+len(plugin.Remove.Headers)+len(plugin.Replace.Headers) == 0 {
		return transformerData{}, errors.New("ResponseHeaderModifier filter config is empty")
	}
	return plugin, nil
}
