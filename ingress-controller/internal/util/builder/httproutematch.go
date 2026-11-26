package builder

import (
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// HTTPRouteMatchBuilder is a builder for gateway api HTTPRouteMatch.
// Primarily used for testing.
// Please note that some methods are not provided yet, as we
// don't need them yet. Feel free to add them as needed.
type HTTPRouteMatchBuilder struct {
	httpRouteMatch gwtypes.HTTPRouteMatch
}

func NewHTTPRouteMatch() *HTTPRouteMatchBuilder {
	return &HTTPRouteMatchBuilder{
		httpRouteMatch: gwtypes.HTTPRouteMatch{},
	}
}

func (b *HTTPRouteMatchBuilder) Build() gwtypes.HTTPRouteMatch {
	return b.httpRouteMatch
}

func (b *HTTPRouteMatchBuilder) ToSlice() []gwtypes.HTTPRouteMatch {
	return []gwtypes.HTTPRouteMatch{b.Build()}
}

func (b *HTTPRouteMatchBuilder) WithPathPrefix(pathPrefix string) *HTTPRouteMatchBuilder {
	return b.WithPathType(&pathPrefix, new(gwtypes.PathMatchPathPrefix))
}

func (b *HTTPRouteMatchBuilder) WithPathRegex(pathRegexp string) *HTTPRouteMatchBuilder {
	return b.WithPathType(&pathRegexp, new(gwtypes.PathMatchRegularExpression))
}

func (b *HTTPRouteMatchBuilder) WithPathExact(pathRegexp string) *HTTPRouteMatchBuilder {
	return b.WithPathType(&pathRegexp, new(gwtypes.PathMatchExact))
}

func (b *HTTPRouteMatchBuilder) WithPathType(pathValuePtr *string, pathTypePtr *gwtypes.PathMatchType) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.Path = &gwtypes.HTTPPathMatch{
		Type:  pathTypePtr,
		Value: pathValuePtr,
	}
	return b
}

func (b *HTTPRouteMatchBuilder) WithQueryParam(name, value string) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.QueryParams = append(b.httpRouteMatch.QueryParams, gwtypes.HTTPQueryParamMatch{
		Name:  gwtypes.HTTPHeaderName(name),
		Value: value,
	})
	return b
}

func (b *HTTPRouteMatchBuilder) WithQueryParamRegex(name, value string) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.QueryParams = append(b.httpRouteMatch.QueryParams, gwtypes.HTTPQueryParamMatch{
		Type:  new(gwtypes.QueryParamMatchRegularExpression),
		Name:  gwtypes.HTTPHeaderName(name),
		Value: value,
	})
	return b
}

func (b *HTTPRouteMatchBuilder) WithMethod(method gwtypes.HTTPMethod) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.Method = &method
	return b
}

func (b *HTTPRouteMatchBuilder) WithHeader(name, value string) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.Headers = append(b.httpRouteMatch.Headers, gwtypes.HTTPHeaderMatch{
		Name:  gwtypes.HTTPHeaderName(name),
		Value: value,
	})
	return b
}

func (b *HTTPRouteMatchBuilder) WithHeaderRegex(name, value string) *HTTPRouteMatchBuilder {
	b.httpRouteMatch.Headers = append(b.httpRouteMatch.Headers, gwtypes.HTTPHeaderMatch{
		Name:  gwtypes.HTTPHeaderName(name),
		Value: value,
		Type:  new(gwtypes.HeaderMatchRegularExpression),
	})
	return b
}
