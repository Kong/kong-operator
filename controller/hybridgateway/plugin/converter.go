package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// translateFromFilter translates a HTTPRouteFilter into one or more KongPlugin resources.
// The generated KongPlugin(s) are filled with the pluginName and json config only leaving to the caller
// the responsibility to set metadata (name, namespace, labels, annotations) as needed.
//
// Supported filter types and their corresponding Kong pluginConfs:
//   - HTTPRouteFilterRequestHeaderModifier -> request-transformer
//   - HTTPRouteFilterResponseHeaderModifier -> response-transformer
//   - HTTPRouteFilterRequestRedirect -> redirect
//   - HTTPRouteFilterURLRewrite -> request-transformer
//
// Parameters:
//   - rule: The HTTPRouteRule containing the filter.
//   - filter: The HTTPRouteFilter to translate.
//
// Returns:
//   - []KongPlugin: Slice of translated KongPlugin resources.
//   - error: Any error encountered during translation.

type kongPluginConfig struct {
	name   string
	config json.RawMessage
}

func translateFromFilter(rule gwtypes.HTTPRouteRule, filter gwtypes.HTTPRouteFilter) ([]kongPluginConfig, error) {
	pluginConfs := []kongPluginConfig{}

	switch filter.Type {
	case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
		pConf := kongPluginConfig{name: "request-transformer"}

		config, err := translateRequestModifier(filter)
		if err != nil {
			return nil, fmt.Errorf("translating RequestHeaderModifier filter: %w", err)
		}
		configJSON, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %q plugin config: %w", pConf.name, err)
		}
		pConf.config = configJSON
		pluginConfs = append(pluginConfs, pConf)
	case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
		pData := kongPluginConfig{name: "response-transformer"}

		config, err := translateResponseModifier(filter)
		if err != nil {
			return nil, fmt.Errorf("translating ResponseHeaderModifier filter: %w", err)
		}
		configJSON, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %q plugin config: %w", pData.name, err)
		}
		pData.config = configJSON
		pluginConfs = append(pluginConfs, pData)
	case gatewayv1.HTTPRouteFilterRequestRedirect:
		pData := kongPluginConfig{name: "redirect"}

		config, err := translateRequestRedirect(filter)
		if err != nil {
			return nil, fmt.Errorf("translating RequestRedirect filter: %w", err)
		}
		configJSON, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %q plugin config: %w", pData.name, err)
		}
		pData.config = configJSON
		pluginConfs = append(pluginConfs, pData)
	case gatewayv1.HTTPRouteFilterURLRewrite:
		pData := kongPluginConfig{name: "request-transformer"}

		path := getPathPrefixMatchValue(rule)

		config, err := translateURLRewrite(filter, path)
		if err != nil {
			return nil, fmt.Errorf("translating URLRewrite filter: %w", err)
		}

		configJSON, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %q plugin config: %w", pData.name, err)
		}
		pData.config = configJSON
		pluginConfs = append(pluginConfs, pData)
	default:
		return nil, fmt.Errorf("unsupported filter type: %s", filter.Type)
	}
	return pluginConfs, nil
}

// internal functions and types for translating HTTPRouteFilter to KongPlugin configurations

func getPathPrefixMatchValue(rule gwtypes.HTTPRouteRule) string {
	for _, match := range rule.Matches {
		if match.Path != nil &&
			match.Path.Type != nil && *match.Path.Type == gatewayv1.PathMatchPathPrefix &&
			match.Path.Value != nil {
			return *match.Path.Value
		}
	}
	return ""
}

type transformerTargetSlice struct {
	Headers []string `json:"headers,omitempty"`
}

type transformerTargetSliceReplace struct {
	transformerTargetSlice
	Uri string `json:"uri,omitempty"`
}

type transformerData struct {
	// Add: adds an header only if the header is not existent.
	// Append: adds a new header even if the header is already existent (adds a new instance).
	// Remove: removes an entry.
	// Replace: overwrites an header value only if the header exists.
	Add     transformerTargetSlice        `json:"add,omitzero"`
	Append  transformerTargetSlice        `json:"append,omitzero"`
	Remove  transformerTargetSlice        `json:"remove,omitzero"`
	Replace transformerTargetSliceReplace `json:"replace,omitzero"`
}

func translateRequestModifier(filter gwtypes.HTTPRouteFilter) (transformerData, error) {
	var err error
	plugin := transformerData{}

	if filter.RequestHeaderModifier == nil {
		err = errors.New("RequestHeaderModifier filter config is missing")
		return plugin, err
	}

	// In order to overwrite an header of add if not present (GWAPI Set) we should do a Kong Plugin
	// Replace (so it will overwrite it if found) + Add (so if not found, will add it).
	if len(filter.RequestHeaderModifier.Set) > 0 {
		for _, v := range filter.RequestHeaderModifier.Set {
			plugin.Replace.Headers = append(plugin.Replace.Headers, string(v.Name)+":"+v.Value)
			plugin.Add.Headers = append(plugin.Add.Headers, string(v.Name)+":"+v.Value)
		}
	}
	// Add for GWAPI equals "append" for Kong Plugins (it will add another instance of the header).
	if len(filter.RequestHeaderModifier.Add) > 0 {
		for _, v := range filter.RequestHeaderModifier.Add {
			plugin.Append.Headers = append(plugin.Append.Headers, string(v.Name)+":"+v.Value)
		}
	}
	if len(filter.RequestHeaderModifier.Remove) > 0 {
		plugin.Remove.Headers = append(plugin.Remove.Headers, filter.RequestHeaderModifier.Remove...)
	}

	if len(plugin.Add.Headers)+len(plugin.Append.Headers)+
		len(plugin.Remove.Headers)+len(plugin.Replace.Headers) == 0 {
		err = errors.New("RequestHeaderModifier filter config is empty")
		plugin = transformerData{}
	}
	return plugin, err
}

func translateResponseModifier(filter gwtypes.HTTPRouteFilter) (transformerData, error) {
	var err error
	plugin := transformerData{}

	if filter.ResponseHeaderModifier == nil {
		err = errors.New("ResponseHeaderModifier filter config is missing")
		return plugin, err
	}

	if len(filter.ResponseHeaderModifier.Set) > 0 {
		for _, v := range filter.ResponseHeaderModifier.Set {
			plugin.Replace.Headers = append(plugin.Replace.Headers, string(v.Name)+":"+v.Value)
			plugin.Add.Headers = append(plugin.Add.Headers, string(v.Name)+":"+v.Value)
		}
	}
	if len(filter.ResponseHeaderModifier.Add) > 0 {
		for _, v := range filter.ResponseHeaderModifier.Add {
			plugin.Append.Headers = append(plugin.Append.Headers, string(v.Name)+":"+v.Value)
		}
	}
	if len(filter.ResponseHeaderModifier.Remove) > 0 {
		plugin.Remove.Headers = append(plugin.Remove.Headers, filter.ResponseHeaderModifier.Remove...)
	}

	if len(plugin.Add.Headers)+len(plugin.Append.Headers)+len(plugin.Remove.Headers)+len(plugin.Replace.Headers) == 0 {
		err = errors.New("ResponseHeaderModifier filter config is empty")
		plugin = transformerData{}
	}
	return plugin, err
}

type requestRedirectConfig struct {
	KeepIncomingPath bool   `json:"keep_incoming_path"`
	Location         string `json:"location"`
	StatusCode       int    `json:"status_code"`
}

func translateRequestRedirect(filter gwtypes.HTTPRouteFilter) (requestRedirectConfig, error) {
	rr := filter.RequestRedirect

	if rr == nil {
		return requestRedirectConfig{}, errors.New("RequestRedirect filter config is missing")
	}

	// GWAPI default status code is 302.
	plugin := requestRedirectConfig{StatusCode: 302}

	if rr.StatusCode != nil {
		plugin.StatusCode = *rr.StatusCode
	}

	locHost := translateRequestRedirectHostname(rr)
	locPath, err := translateRequestRedirectPath(rr)
	if err != nil {
		return requestRedirectConfig{}, err
	}
	if locPath == "" {
		plugin.KeepIncomingPath = true
		locPath = "/"
	}

	plugin.Location = locHost + locPath
	return plugin, nil
}

func translateRequestRedirectHostname(rr *gatewayv1.HTTPRequestRedirectFilter) string {
	if rr.Hostname == nil || *rr.Hostname == "" {
		return ""
	}

	// when no scheme specified we assume `http` (as KIC does) but we should preserve the actual scheme instead.
	// this cannot be done with direct filter -> kong plugin conversion.
	// See https://github.com/Kong/kong-operator/issues/2466.
	host := lo.FromPtrOr(rr.Scheme, "http") + "://"
	host += string(lo.FromPtrOr((rr.Hostname), ""))
	if rr.Port != nil {
		host += fmt.Sprintf(":%d", *rr.Port)
	}
	return host
}

func translateRequestRedirectPath(rr *gatewayv1.HTTPRequestRedirectFilter) (string, error) {
	path := ""
	var err error

	if rr.Path == nil {
		return path, nil
	}

	pathModifier := rr.Path
	switch pathModifier.Type {
	case gatewayv1.FullPathHTTPPathModifier:
		path = translatePathReplaceFullPath(pathModifier.ReplaceFullPath)
	case gatewayv1.PrefixMatchHTTPPathModifier:
		path = translateRequestRedirectPathPrefixMatch(pathModifier.ReplacePrefixMatch)
	default:
		err = errors.New("unsupported RequestRedirect path modifier type: " + string(pathModifier.Type))
	}
	return path, err
}

func translatePathReplaceFullPath(replaceFullPath *string) string {
	if replaceFullPath == nil || *replaceFullPath == "" {
		return "/"
	}
	return *replaceFullPath
}

func translateRequestRedirectPathPrefixMatch(prefixMatch *string) string {
	// Not implemented yet - Kong does not have a direct equivalent for prefix match replacement.
	// KIC in Konnect just ignores PrefixMatch filters, let's do the same.
	// Tracker: https://github.com/Kong/kong-operator/issues/2466
	return "/"
}

func translateURLRewrite(filter gwtypes.HTTPRouteFilter, path string) (transformerData, error) {
	ur := filter.URLRewrite
	pluginConf := transformerData{}

	if ur == nil {
		return pluginConf, errors.New("URLRewrite filter config is missing")

	}

	if ur.Hostname != nil {
		headers := []string{"host:" + string(*ur.Hostname)}
		pluginConf.Replace.Headers = headers
		pluginConf.Add.Headers = headers
	}
	if ur.Path != nil {
		switch ur.Path.Type {
		case gatewayv1.FullPathHTTPPathModifier:
			pluginConf.Replace.Uri = translatePathReplaceFullPath(ur.Path.ReplaceFullPath)
		case gatewayv1.PrefixMatchHTTPPathModifier:
			pluginConf.Replace.Uri = translatePathReplacePrefixMatch(
				normalizePath(ur.Path.ReplacePrefixMatch),
				normalizePath(&path))
		default:
			return pluginConf, fmt.Errorf("unsupported URLRewrite path modifier type: %s", ur.Path.Type)
		}
	}

	return pluginConf, nil
}

func normalizePath(path *string) string {
	if path == nil || *path == "" || *path == "/" {
		return "/"
	}
	return strings.TrimSuffix(*path, "/")
}

// translatePathReplacePrefixMatch generates the replacement URI for the request-transformer
// plugin for the URLRewrite filter with a PrefixMatchHTTPPathModifier.
// The logic here is copied from KIC's implementation to ensure consistent behavior, see:
// https://github.com/Kong/kubernetes-ingress-controller/blob/main/internal/dataplane/translator/subtranslator/httproute.go#L1434.
func translatePathReplacePrefixMatch(replacePrefixMatch string, path string) string {
	// Trim the trailing slash from the ReplacePrefixMatch to avoid double slashes in the final URI.
	replacePrefixMatch = strings.TrimSuffix(replacePrefixMatch, "/")
	pathIsRoot := path == "/"

	// In the case of an empty replacePrefixMatch, we need to make sure that the path will always start with a slash,
	// even if we have no capture group from the incoming request's URI.
	if replacePrefixMatch == "" {
		// If path is "/", we need to add a slash before URI captures because the capture group won't include
		// the leading slash.
		if pathIsRoot {
			// The below is a Lua ternary operator that checks if the captured group is nil, and if so, replaces it with
			// a slash. Otherwise, it appends the captured group to a slash.
			return `$(uri_captures[1] == nil and "/" or "/" .. uri_captures[1])`
		}

		// Otherwise, we do not need to add a leading slash before URI captures.
		// The below is a Lua ternary operator that checks if the captured group is nil, and if so, replaces it with
		// a slash. Otherwise, it returns the captured group (in this case the captured group will always have a
		// leading slash).
		return `$(uri_captures[1] == nil and "/" or uri_captures[1])`
	}

	// Otherwise, we concatenate the replacement URI with the captured group.
	// If path is "/", we need to add a slash before URI captures because the capture group won't include the
	// leading slash.
	if pathIsRoot {
		// The below Lua ternary operator checks if the captured group is nil, and if so, replaces it with
		// an empty string (as we already know replacePrefixMatch is not empty so the resulting path will always have
		// a leading slash). Otherwise, it appends the captured group to a slash (as the captured group won't
		// have the leading slash).
		return fmt.Sprintf(`%s$(uri_captures[1] == nil and "" or "/" .. uri_captures[1])`, replacePrefixMatch)
	}
	// Simply concatenate the replacement URI with the captured group as the captured group will always have a
	// leading slash.
	return fmt.Sprintf(`%s$(uri_captures[1])`, replacePrefixMatch)
}
