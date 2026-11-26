package builder

import (
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// HTTPRouteFilterBuilder is a builder for gateway api HTTPRouteMatch.
// Primarily used for testing.
type HTTPRouteFilterBuilder struct {
	httpRouteFilter gwtypes.HTTPRouteFilter
}

func (b *HTTPRouteFilterBuilder) Build() gwtypes.HTTPRouteFilter {
	return b.httpRouteFilter
}

// NewHTTPRouteRequestRedirectFilter builds a request redirect HTTPRoute filter.
func NewHTTPRouteRequestRedirectFilter() *HTTPRouteFilterBuilder {
	filter := gwtypes.HTTPRouteFilter{
		Type:            gwtypes.HTTPRouteFilterRequestRedirect,
		RequestRedirect: &gwtypes.HTTPRequestRedirectFilter{},
	}
	return &HTTPRouteFilterBuilder{httpRouteFilter: filter}
}

// WithRequestRedirectScheme sets scheme of request redirect filter.
func (b *HTTPRouteFilterBuilder) WithRequestRedirectScheme(scheme string) *HTTPRouteFilterBuilder {
	if b.httpRouteFilter.Type != gwtypes.HTTPRouteFilterRequestRedirect ||
		b.httpRouteFilter.RequestRedirect == nil {
		return b
	}

	b.httpRouteFilter.RequestRedirect.Scheme = new(scheme)
	return b
}

// WithRequestRedirectHost sets host of request redirect filter.
func (b *HTTPRouteFilterBuilder) WithRequestRedirectHost(host string) *HTTPRouteFilterBuilder {
	if b.httpRouteFilter.Type != gwtypes.HTTPRouteFilterRequestRedirect ||
		b.httpRouteFilter.RequestRedirect == nil {
		return b
	}

	preciseHost := (gwtypes.PreciseHostname)(host)
	b.httpRouteFilter.RequestRedirect.Hostname = new(preciseHost)
	return b
}

// WithRequestRedirectStatusCode sets status code of response in request redirect filter.
func (b *HTTPRouteFilterBuilder) WithRequestRedirectStatusCode(code int) *HTTPRouteFilterBuilder {
	if b.httpRouteFilter.Type != gwtypes.HTTPRouteFilterRequestRedirect ||
		b.httpRouteFilter.RequestRedirect == nil {
		return b
	}

	b.httpRouteFilter.RequestRedirect.StatusCode = new(code)
	return b
}

func (b *HTTPRouteFilterBuilder) WithRequestRedirectPathModifier(modifierType gwtypes.HTTPPathModifierType, path string) *HTTPRouteFilterBuilder {
	if b.httpRouteFilter.Type != gwtypes.HTTPRouteFilterRequestRedirect ||
		b.httpRouteFilter.RequestRedirect == nil {
		return b
	}
	if modifierType == gwtypes.FullPathHTTPPathModifier {
		b.httpRouteFilter.RequestRedirect.Path = &gwtypes.HTTPPathModifier{
			Type:            gwtypes.FullPathHTTPPathModifier,
			ReplaceFullPath: new(path),
		}
	}
	if modifierType == gwtypes.PrefixMatchHTTPPathModifier {
		b.httpRouteFilter.RequestRedirect.Path = &gwtypes.HTTPPathModifier{
			Type:               gwtypes.PrefixMatchHTTPPathModifier,
			ReplacePrefixMatch: new(path),
		}
	}
	return b
}

// NewHTTPRouteRequestHeaderModifierFilter builds a request header modifier HTTPRoute filter.
func NewHTTPRouteRequestHeaderModifierFilter() *HTTPRouteFilterBuilder {
	filter := gwtypes.HTTPRouteFilter{
		Type:                  gwtypes.HTTPRouteFilterRequestHeaderModifier,
		RequestHeaderModifier: &gwtypes.HTTPHeaderFilter{},
	}
	return &HTTPRouteFilterBuilder{httpRouteFilter: filter}
}

func (b *HTTPRouteFilterBuilder) WithRequestHeaderAdd(headers []gwtypes.HTTPHeader) *HTTPRouteFilterBuilder {
	if b.httpRouteFilter.Type != gwtypes.HTTPRouteFilterRequestHeaderModifier ||
		b.httpRouteFilter.RequestHeaderModifier == nil {
		return b
	}
	b.httpRouteFilter.RequestHeaderModifier.Add = headers
	return b
}

func (b *HTTPRouteFilterBuilder) WithRequestHeaderSet(headers []gwtypes.HTTPHeader) *HTTPRouteFilterBuilder {
	if b.httpRouteFilter.Type != gwtypes.HTTPRouteFilterRequestHeaderModifier ||
		b.httpRouteFilter.RequestHeaderModifier == nil {
		return b
	}
	b.httpRouteFilter.RequestHeaderModifier.Set = headers
	return b
}

func (b *HTTPRouteFilterBuilder) WithRequestHeaderRemove(headerNames []string) *HTTPRouteFilterBuilder {
	if b.httpRouteFilter.Type != gwtypes.HTTPRouteFilterRequestHeaderModifier ||
		b.httpRouteFilter.RequestHeaderModifier == nil {
		return b
	}
	b.httpRouteFilter.RequestHeaderModifier.Remove = headerNames
	return b
}
