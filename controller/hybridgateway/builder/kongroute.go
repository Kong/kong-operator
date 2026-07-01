package builder

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const (
	// KongPathRegexPrefix is the reserved prefix string that instructs Kong 3.0+ to interpret a path as a regex.
	KongPathRegexPrefix = "~"
	// KongHeaderRegexPrefix is a reserved prefix string that Kong uses to determine if it should parse a header value
	// as a regex.
	KongHeaderRegexPrefix = "~*"
	// KongHTTPRouteHeaderOnlyRegexPath is a catch-all regex path used to make regex_priority effective
	// for HTTPRoute matches that only match on headers.
	KongHTTPRouteHeaderOnlyRegexPath = KongPathRegexPrefix + "/(.*)"
	// KongHTTPRoutePathRegexPriorityOffset reserves lower regex_priority values for synthetic
	// catch-all regex routes used by default path header-only matches.
	KongHTTPRoutePathRegexPriorityOffset int64 = 1 << 20
)

// KongRouteBuilder is a builder for configurationv1alpha1.KongRoute resources.
type KongRouteBuilder struct {
	route  configurationv1alpha1.KongRoute
	errors []error
}

// NewKongRoute creates and returns a new KongRouteBuilder instance.
func NewKongRoute() *KongRouteBuilder {
	return &KongRouteBuilder{
		route:  configurationv1alpha1.KongRoute{},
		errors: make([]error, 0),
	}
}

// WithHosts sets the hosts for the KongRoute being built.
func (b *KongRouteBuilder) WithHosts(hosts []string) *KongRouteBuilder {
	b.route.Spec.Hosts = append(b.route.Spec.Hosts, hosts...)
	return b
}

// WithProtocols sets the allowed protocols for the KongRoute being built.
func (b *KongRouteBuilder) WithProtocols(protocols ...sdkkonnectcomp.RouteJSONProtocols) *KongRouteBuilder {
	b.route.Spec.Protocols = append(b.route.Spec.Protocols, protocols...)
	return b
}

// WithHTTPRouteMatch sets the match criteria (path, method, headers) for the KongRoute.
func (b *KongRouteBuilder) WithHTTPRouteMatch(match gwtypes.HTTPRouteMatch, setCaptureGroup bool) *KongRouteBuilder {
	// Path.
	if match.Path != nil && match.Path.Value != nil {
		paths, regexPriority := GenerateKongRoutePathFromHTTPRouteMatch(match.Path, setCaptureGroup)
		b.route.Spec.Paths = append(b.route.Spec.Paths, paths...)
		b.route.Spec.RegexPriority = regexPriority
	}

	// Method
	if match.Method != nil {
		b.route.Spec.Methods = append(b.route.Spec.Methods, string(*match.Method))
		if len(b.route.Spec.Paths) == 0 {
			// Gateway API treats an omitted path as PathPrefix "/". Materialize it so
			// Kong's traditional router prioritizes method-only matches over header-only matches.
			b.route.Spec.Paths = append(b.route.Spec.Paths, "/")
		}
	}

	// Headers
	if len(match.Headers) > 0 {
		if b.route.Spec.Headers == nil {
			b.route.Spec.Headers = make(map[string][]string)
		}
		for _, hdr := range match.Headers {
			value := hdr.Value
			if hdr.Type != nil && *hdr.Type == gatewayv1.HeaderMatchRegularExpression {
				value = KongHeaderRegexPrefix + value
			}
			b.route.Spec.Headers[string(hdr.Name)] = append(b.route.Spec.Headers[string(hdr.Name)], value)
		}
	}
	// Note: QueryParams are not natively supported by KongRoute

	return b
}

// WithRegexPriority sets the regex_priority for the KongRoute being built.
func (b *KongRouteBuilder) WithRegexPriority(priority *int64) *KongRouteBuilder {
	if priority != nil {
		b.route.Spec.RegexPriority = priority
	}
	return b
}

// WithHeaderOnlyRegexPath adds a catch-all regex path when needed so Kong can use
// regex_priority to order overlapping HTTPRoute matches that only specify headers
// and otherwise use the default root path.
func (b *KongRouteBuilder) WithHeaderOnlyRegexPath() *KongRouteBuilder {
	if len(b.route.Spec.Headers) == 0 {
		return b
	}

	if len(b.route.Spec.Paths) == 0 || (len(b.route.Spec.Paths) == 1 && b.route.Spec.Paths[0] == "/") {
		b.route.Spec.Paths = []string{KongHTTPRouteHeaderOnlyRegexPath}
	}
	return b
}

// WithKongService sets the KongService reference for the KongRoute.
func (b *KongRouteBuilder) WithKongService(name string) *KongRouteBuilder {
	if name != "" {
		b.route.Spec.ServiceRef = &configurationv1alpha1.ServiceRef{
			Type: configurationv1alpha1.ServiceRefNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{
				Name: name,
			},
		}
	}
	return b
}

// WithSpecName sets the name field in the KongRoute spec.
func (b *KongRouteBuilder) WithSpecName(name string) *KongRouteBuilder {
	b.route.Spec.Name = &name
	return b
}

// WithStripPath sets the strip path option for the KongRoute.
func (b *KongRouteBuilder) WithStripPath(stripPath bool) *KongRouteBuilder {
	b.route.Spec.StripPath = &stripPath
	return b
}

// WithPreserveHost sets the preserve host option for the KongRoute.
func (b *KongRouteBuilder) WithPreserveHost(preserveHost bool) *KongRouteBuilder {
	b.route.Spec.PreserveHost = &preserveHost
	return b
}

// WithSNIs sets the SNIs for the KongRoute.
func (b *KongRouteBuilder) WithSNIs(snis []string) *KongRouteBuilder {
	b.route.Spec.Snis = snis
	return b
}

// WithOwner sets the owner reference for the KongRoute to the given route.
func (b *KongRouteBuilder) WithOwner(owner client.Object) *KongRouteBuilder {
	if owner == nil {
		b.errors = append(b.errors, errors.New("owner cannot be nil"))
		return b
	}

	err := controllerutil.SetControllerReference(owner, &b.route, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// WithName sets the name field of the KongRoute resource.
func (b *KongRouteBuilder) WithName(name string) *KongRouteBuilder {
	b.route.Name = name
	return b
}

// WithNamespace sets the namespace field of the KongRoute resource.
func (b *KongRouteBuilder) WithNamespace(namespace string) *KongRouteBuilder {
	b.route.Namespace = namespace
	return b
}

// WithLabels sets the labels for the KongRoute resource based on the given route.
func (b *KongRouteBuilder) WithLabels(route client.Object, parentRef *gwtypes.ParentReference) *KongRouteBuilder {
	labels := metadata.BuildLabels(route, parentRef)
	if b.route.Labels == nil {
		b.route.Labels = make(map[string]string)
	}
	maps.Copy(b.route.Labels, labels)
	return b
}

// WithAnnotations sets the annotations for the KongRoute resource based on the given route and parent reference.
func (b *KongRouteBuilder) WithAnnotations(route client.Object, parentRef *gwtypes.ParentReference) *KongRouteBuilder {
	annotations := metadata.BuildAnnotations(route, parentRef)
	if b.route.Annotations == nil {
		b.route.Annotations = make(map[string]string)
	}
	maps.Copy(b.route.Annotations, annotations)
	return b
}

// Build returns the constructed KongRoute resource and any accumulated errors.
func (b *KongRouteBuilder) Build() (configurationv1alpha1.KongRoute, error) {
	if len(b.errors) > 0 {
		return configurationv1alpha1.KongRoute{}, errors.Join(b.errors...)
	}
	return b.route, nil
}

// MustBuild returns the constructed KongRoute resource, panicking on any errors.
// Useful for tests or when you're certain the build will succeed.
func (b *KongRouteBuilder) MustBuild() configurationv1alpha1.KongRoute {
	route, err := b.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build KongRoute: %w", err))
	}
	return route
}

// GenerateKongRoutePathFromHTTPRouteMatch translates the value in HTTPRoute's path match
// to the path used in KongRoute and returns any RegexPriority needed for the KongRoute.
// RegexPriority is set for Exact and non-root PathPrefix translations whose generated regex
// paths must preserve Gateway API path precedence in Kong's traditional-compatible router.
// Root capture-group translations and user-supplied RegularExpression matches keep Kong's
// default priority because they remain catch-all or implementation-specific cases.
func GenerateKongRoutePathFromHTTPRouteMatch(pathMatch *gatewayv1.HTTPPathMatch, setCaptureGroup bool) (paths []string, regexPriority *int64) {
	// The default match type is PathMatchPathPrefix.
	matchType := gatewayv1.PathMatchPathPrefix
	if pathMatch.Type != nil {
		matchType = *pathMatch.Type
	}

	value := *pathMatch.Value

	// The value in `path` on KongRoute matches the path in the request in the following manner:
	// For normal paths, it matches the request when the value is the prefix of the path in the request.
	// For example, '/abc' matches '/abc', '/abc/', '/abc/123' and '/abcd'.
	// For paths starting with the prefix '~', the part after the prefix is interpreted as the regex to match the path in the request.
	// If the prefix of the path in the request matches the request, the path is matched, as '^' prefix is added to the regex but '$' suffix is not.
	// For example, '~/api/[a-z]+' matches '/api/a', '/api/abc', '/api/abc/123' but not '/api/', '/api/123'.
	// So we need to translate the path match to the paths in KongRoute by its type and value.
	switch matchType {
	// Since the path matches request in prefix way, we need to use a regex with the '$' suffix to do the exact match.
	case gatewayv1.PathMatchExact:
		return []string{KongPathRegexPrefix + value + "$"}, regexPriorityForHTTPPathMatch(matchType, value)

	// In HTTPRoute, the prefix match is specified in the "directory" manner but not simple string prefix.
	// For example, '/abc' should match '/abc', '/abc/', '/abc/123' but not '/abcd'.
	// So we split it into 2 items:
	// - One using regex to match the exact path without the trailing '/', e.g: '~/abc$'
	// - The other to match the prefix with the trailing '/', e.g: '/abc/'.
	case gatewayv1.PathMatchPathPrefix:
		// For the '/' path to match all, we just return the item in KongRoute to do the same catch-all match.
		if value == "/" && !setCaptureGroup {
			return []string{"/"}, nil
		}

		paths := make([]string, 0, 2)
		path := value
		// Match the exact path without the trailing '/'.
		paths = append(paths, fmt.Sprintf("%s%s$", KongPathRegexPrefix, path))

		// In case the rule has a RequestRedirect or URLRewrite filter with a ReplacePrefixMatch path,
		// we need to add a capture group in the KongRoute to pass the rest of the path to the filter.
		if setCaptureGroup {
			// If the path is "/", we have to skip capturing the slash as Kong Route's path must begin with a slash.
			if value == "/" {
				// Keep the default priority for the root capture-group form: it remains the catch-all route.
				return append(paths, fmt.Sprintf("%s/(.*)", KongPathRegexPrefix)), nil
			}
			// When there is a prefix in the route path, we capture the slash and the rest of the path after the prefix.
			return append(paths, fmt.Sprintf("%s%s%s", KongPathRegexPrefix, path, "(/.*)")), regexPriorityForHTTPPathMatch(matchType, value)
		}

		if !strings.HasSuffix(path, "/") {
			path = fmt.Sprintf("%s/", path)
		}
		return append(paths, path), regexPriorityForHTTPPathMatch(matchType, value)

	// For RegularExpression path match, we simply use the same regex in the paths of KongRoute.
	case gatewayv1.PathMatchRegularExpression:
		// Gateway API leaves the precedence of regular-expression path matches implementation-specific.
		return []string{KongPathRegexPrefix + value}, nil
	}
	return nil, nil // Should never be reached.
}

func regexPriorityForHTTPPathMatch(matchType gatewayv1.PathMatchType, value string) *int64 {
	priority := KongHTTPRoutePathRegexPriorityOffset + int64(len(value)*2)
	if matchType == gatewayv1.PathMatchExact {
		priority++
	}
	return &priority
}
