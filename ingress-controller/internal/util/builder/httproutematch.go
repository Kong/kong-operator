package builder

import (
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
)

// HTTPRouteMatchBuilder is a builder for gateway api HTTPRouteMatch.
// Primarily used for testing.
// Please note that some methods are not provided yet, as we
// don't need them yet. Feel free to add them as needed.
type HTTPRouteMatchBuilder struct {
	httpRouteMatch gatewayapi.HTTPRouteMatch
}

func NewHTTPRouteMatch() *HTTPRouteMatchBuilder {
	return &HTTPRouteMatchBuilder{
		httpRouteMatch: gatewayapi.HTTPRouteMatch{},
	}
}

func (b *HTTPRouteMatchBuilder) Build() gatewayapi.HTTPRouteMatch {
	return b.httpRouteMatch
}

func (b *HTTPRouteMatchBuilder) ToSlice() []gatewayapi.HTTPRouteMatch {
	return []gatewayapi.HTTPRouteMatch{b.Build()}
}

func (b *HTTPRouteMatchBuilder) WithPathPrefix(pathPrefix string) *HTTPRouteMatchBuilder {
	return b.WithPathType(&pathPrefix, new(gatewayapi.PathMatchPathPrefix))
}

func (b *HTTPRouteMatchBuilder) WithPathRegex(pathRegexp string) *HTTPRouteMatchBuilder {
	return b.WithPathType(&pathRegexp, new(gatewayapi.PathMatchRegularExpression))
}

func (b *HTTPRouteMatchBuilder) WithPathExact(pathRegexp string) *HTTPRouteMatchBuilder {
	return b.WithPathType(&pathRegexp, new(gatewayapi.PathMatchExact))
}

func (b *HTTPRouteMatchBuilder) WithPathType(pathValuePtr *string, pathTypePtr *gatewayapi.PathMatchType) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.Path = &gatewayapi.HTTPPathMatch{
		Type:  pathTypePtr,
		Value: pathValuePtr,
	}
	return b
}

func (b *HTTPRouteMatchBuilder) WithQueryParam(name, value string) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.QueryParams = append(b.httpRouteMatch.QueryParams, gatewayapi.HTTPQueryParamMatch{
		Name:  gatewayapi.HTTPHeaderName(name),
		Value: value,
	})
	return b
}

func (b *HTTPRouteMatchBuilder) WithQueryParamRegex(name, value string) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.QueryParams = append(b.httpRouteMatch.QueryParams, gatewayapi.HTTPQueryParamMatch{
		Type:  new(gatewayapi.QueryParamMatchRegularExpression),
		Name:  gatewayapi.HTTPHeaderName(name),
		Value: value,
	})
	return b
}

func (b *HTTPRouteMatchBuilder) WithMethod(method gatewayapi.HTTPMethod) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.Method = &method
	return b
}

func (b *HTTPRouteMatchBuilder) WithHeader(name, value string) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.Headers = append(b.httpRouteMatch.Headers, gatewayapi.HTTPHeaderMatch{
		Name:  gatewayapi.HTTPHeaderName(name),
		Value: value,
	})
	return b
}

func (b *HTTPRouteMatchBuilder) WithHeaderRegex(name, value string) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.Headers = append(b.httpRouteMatch.Headers, gatewayapi.HTTPHeaderMatch{
		Name:  gatewayapi.HTTPHeaderName(name),
		Value: value,
		Type:  new(gatewayapi.HeaderMatchRegularExpression),
	})
	return b
}
