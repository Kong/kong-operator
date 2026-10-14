package subtranslator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kong/go-kong/kong"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/translator/atc"
)

// ListenerPortHeader is the request header that carries the port of the Kong
// listener which accepted the request, set by the plugin returned from
// ListenerPortPlugin. Expression routes match on it to bind a route to the
// listener ports of the Gateway it is attached to.
//
// Matching on `net.dst.port` is not an option, because for HTTP requests Kong
// takes it from the port in the Host header when present, e.g. a request with
// `Host: example.com:1234` received on port 80 has `net.dst.port == 1234`.
// Gateway API requires the Host header port to be ignored for routing.
const ListenerPortHeader = "X-Kong-Listener-Port"

const listenerPortPluginName = "pre-function"

var (
	// listenerPortRewriteCode sets ListenerPortHeader in the rewrite phase of a global
	// plugin, i.e. before the router runs. kong.request.get_forwarded_port() returns the
	// listener port translated through Kong's port_maps (e.g. 80 for a Kong listening on
	// 16384 with port_maps 80:16384), and is available in the untrusted Lua sandbox,
	// unlike ngx.ctx and ngx.var. It honors X-Forwarded-Port only for trusted_ips.
	// ngx.req.set_header overwrites the header if the client sent it.
	listenerPortRewriteCode = fmt.Sprintf(
		`ngx.req.set_header(%q, tostring(kong.request.get_forwarded_port()))`, ListenerPortHeader,
	)
	// listenerPortAccessCode removes ListenerPortHeader once routing is done so it doesn't leak upstream.
	listenerPortAccessCode = fmt.Sprintf(`kong.service.request.clear_header(%q)`, ListenerPortHeader)
)

// listenerPortMatcher returns a matcher that matches requests received by any of
// the given Kong listener ports. It returns an empty matcher for no ports.
func listenerPortMatcher(ports []int32) atc.Matcher {
	portMatchers := make([]atc.Matcher, 0, len(ports))
	for _, p := range ports {
		portMatchers = append(portMatchers,
			atc.NewPredicateHTTPHeader(ListenerPortHeader, atc.OpEqual, strconv.Itoa(int(p))),
		)
	}
	return atc.Or(portMatchers...)
}

// ExpressionUsesListenerPort returns true if the given route expression
// matches on ListenerPortHeader, i.e. it requires the plugin returned from
// ListenerPortPlugin to be configured.
func ExpressionUsesListenerPort(expression string) bool {
	return strings.Contains(expression, atc.HTTPHeaderField{HeaderName: ListenerPortHeader}.String())
}

// ListenerPortPlugin returns the global plugin that populates ListenerPortHeader
// for routes using listener port matching. Kong allows only one global plugin
// of a given name, so when existingPlugins already contain a global pre-function
// plugin, its copy with the listener port code prepended is returned along with
// its index (-1 otherwise), and the caller is expected to replace it.
func ListenerPortPlugin(existingPlugins []kongstate.Plugin) (kongstate.Plugin, int) {
	for i, p := range existingPlugins {
		if !isGlobalPlugin(p.Plugin) || p.Name == nil || *p.Name != listenerPortPluginName {
			continue
		}
		merged := p.DeepCopy()
		if merged.Config == nil {
			merged.Config = kong.Configuration{}
		}
		merged.Config["rewrite"] = prependCode(merged.Config["rewrite"], listenerPortRewriteCode)
		merged.Config["access"] = prependCode(merged.Config["access"], listenerPortAccessCode)
		return merged, i
	}

	return kongstate.Plugin{
		Name: new(listenerPortPluginName),
		Config: kong.Configuration{
			"rewrite": []any{listenerPortRewriteCode},
			"access":  []any{listenerPortAccessCode},
		},
	}, -1
}

func isGlobalPlugin(p kong.Plugin) bool {
	return p.Route == nil && p.Service == nil && p.Consumer == nil && p.ConsumerGroup == nil
}

// prependCode prepends code to a pre-function phase config value (an array of Lua chunks).
func prependCode(phase any, code string) []any {
	ret := []any{code}
	switch v := phase.(type) {
	case []any:
		ret = append(ret, v...)
	case []string:
		for _, s := range v {
			ret = append(ret, s)
		}
	}
	return ret
}
