package upstream

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// upstreamPolicyForRouteRule returns the KongUpstreamPolicy for the given route rule by
// inspecting the konghq.com/upstream-policy annotation on each backend Service.
//
// All backend Services must reference the same policy (or none). Returns nil when no annotation
// is present. Logs a warning and returns nil when annotations are inconsistent or the referenced
// policy does not exist.
func upstreamPolicyForRouteRule[R gwtypes.SupportedRouteRule](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	namespace string,
	rule R,
) *configurationv1beta1.KongUpstreamPolicy {
	policyNames := map[string]struct{}{}
	var firstPolicyNamespace string
	var firstPolicyName string
	found := false

	var backendRefs []gwtypes.BackendRef
	switch r := any(rule).(type) {
	case gwtypes.HTTPRouteRule:
		backendRefs = utils.HTTPBackendRefsToBackendRefs(r.BackendRefs)
	case gwtypes.TLSRouteRule:
		backendRefs = r.BackendRefs
	default:
		return nil
	}

	for _, backendRef := range backendRefs {
		if !utils.IsBackendRefSupported(backendRef.Group, backendRef.Kind) {
			continue
		}

		svcNamespace := namespace
		if backendRef.Namespace != nil && *backendRef.Namespace != "" {
			svcNamespace = string(*backendRef.Namespace)
		}

		svc := &corev1.Service{}
		if err := cl.Get(ctx, client.ObjectKey{Namespace: svcNamespace, Name: string(backendRef.Name)}, svc); err != nil {
			log.Debug(logger, "Failed to fetch backend Service for upstream-policy annotation check",
				"service", fmt.Sprintf("%s/%s", svcNamespace, backendRef.Name), "error", err)
			continue
		}

		policyName, ok := svc.Annotations[configurationv1beta1.KongUpstreamPolicyAnnotationKey]
		if !ok || policyName == "" {
			continue
		}

		key := svcNamespace + "/" + policyName
		policyNames[key] = struct{}{}
		if !found {
			firstPolicyNamespace = svcNamespace
			firstPolicyName = policyName
			found = true
		}
	}

	if !found {
		return nil
	}

	if len(policyNames) > 1 {
		log.Info(logger, "Inconsistent konghq.com/upstream-policy annotations across backend Services; upstream policy will not be applied",
			"policies", policyNames)
		return nil
	}

	policy := &configurationv1beta1.KongUpstreamPolicy{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: firstPolicyNamespace, Name: firstPolicyName}, policy); err != nil {
		log.Info(logger, "Failed to fetch KongUpstreamPolicy; upstream policy will not be applied",
			"policy", fmt.Sprintf("%s/%s", firstPolicyNamespace, firstPolicyName), "error", err)
		return nil
	}

	return policy
}

// applyPolicyToUpstream applies non-nil fields from the KongUpstreamPolicy spec to the upstream spec.
func applyPolicyToUpstream(upstream *configurationv1alpha1.KongUpstream, policy *configurationv1beta1.KongUpstreamPolicy) {
	if policy == nil {
		return
	}
	translated := translatePolicySpecToUpstreamAPISpec(policy.Spec)

	if translated.Algorithm != nil {
		upstream.Spec.Algorithm = translated.Algorithm
	}
	if translated.Slots != nil {
		upstream.Spec.Slots = translated.Slots
	}
	if translated.HashOn != nil {
		upstream.Spec.HashOn = translated.HashOn
	}
	if translated.HashOnHeader != nil {
		upstream.Spec.HashOnHeader = translated.HashOnHeader
	}
	if translated.HashOnCookie != nil {
		upstream.Spec.HashOnCookie = translated.HashOnCookie
	}
	if translated.HashOnCookiePath != nil {
		upstream.Spec.HashOnCookiePath = translated.HashOnCookiePath
	}
	if translated.HashOnQueryArg != nil {
		upstream.Spec.HashOnQueryArg = translated.HashOnQueryArg
	}
	if translated.HashOnURICapture != nil {
		upstream.Spec.HashOnURICapture = translated.HashOnURICapture
	}
	if translated.HashFallback != nil {
		upstream.Spec.HashFallback = translated.HashFallback
	}
	if translated.HashFallbackHeader != nil {
		upstream.Spec.HashFallbackHeader = translated.HashFallbackHeader
	}
	if translated.HashFallbackQueryArg != nil {
		upstream.Spec.HashFallbackQueryArg = translated.HashFallbackQueryArg
	}
	if translated.HashFallbackURICapture != nil {
		upstream.Spec.HashFallbackURICapture = translated.HashFallbackURICapture
	}
	if translated.Healthchecks != nil {
		upstream.Spec.Healthchecks = translated.Healthchecks
	}
	if translated.StickySessionsCookie != nil {
		upstream.Spec.StickySessionsCookie = translated.StickySessionsCookie
	}
	if translated.StickySessionsCookiePath != nil {
		upstream.Spec.StickySessionsCookiePath = translated.StickySessionsCookiePath
	}
}

// translatePolicySpecToUpstreamAPISpec converts KongUpstreamPolicySpec (v1beta1) to
// KongUpstreamAPISpec (v1alpha1 / Konnect SDK types). Only non-nil fields are populated.
func translatePolicySpecToUpstreamAPISpec(spec configurationv1beta1.KongUpstreamPolicySpec) configurationv1alpha1.KongUpstreamAPISpec {
	result := configurationv1alpha1.KongUpstreamAPISpec{}

	if spec.Algorithm != nil {
		algo := sdkkonnectcomp.UpstreamAlgorithm(*spec.Algorithm)
		result.Algorithm = &algo
	}

	if spec.Slots != nil {
		slots := int64(*spec.Slots)
		result.Slots = &slots
	}

	if spec.HashOn != nil {
		result.HashOn = translateHashOnToSDK(spec.HashOn)
		result.HashOnHeader = spec.HashOn.Header
		result.HashOnCookie = spec.HashOn.Cookie
		result.HashOnCookiePath = spec.HashOn.CookiePath
		result.HashOnQueryArg = spec.HashOn.QueryArg
		result.HashOnURICapture = spec.HashOn.URICapture
	}

	if spec.HashOnFallback != nil {
		result.HashFallback = translateHashFallbackToSDK(spec.HashOnFallback)
		result.HashFallbackHeader = spec.HashOnFallback.Header
		result.HashFallbackQueryArg = spec.HashOnFallback.QueryArg
		result.HashFallbackURICapture = spec.HashOnFallback.URICapture
	}

	if spec.StickySessions != nil {
		result.StickySessionsCookie = &spec.StickySessions.Cookie
		result.StickySessionsCookiePath = spec.StickySessions.CookiePath
	}

	result.Healthchecks = translateHealthchecksToSDK(spec.Healthchecks)

	return result
}

func translateHashOnToSDK(h *configurationv1beta1.KongUpstreamHash) *sdkkonnectcomp.HashOn {
	if h == nil {
		return nil
	}
	var val sdkkonnectcomp.HashOn
	switch {
	case h.Input != nil:
		val = sdkkonnectcomp.HashOn(string(*h.Input))
	case h.Header != nil:
		val = sdkkonnectcomp.HashOnHeader
	case h.Cookie != nil:
		val = sdkkonnectcomp.HashOnCookie
	case h.QueryArg != nil:
		val = sdkkonnectcomp.HashOnQueryArg
	case h.URICapture != nil:
		val = sdkkonnectcomp.HashOnURICapture
	default:
		return nil
	}
	return &val
}

func translateHashFallbackToSDK(h *configurationv1beta1.KongUpstreamHash) *sdkkonnectcomp.HashFallback {
	if h == nil {
		return nil
	}
	var val sdkkonnectcomp.HashFallback
	switch {
	case h.Input != nil:
		val = sdkkonnectcomp.HashFallback(string(*h.Input))
	case h.Header != nil:
		val = sdkkonnectcomp.HashFallbackHeader
	case h.QueryArg != nil:
		val = sdkkonnectcomp.HashFallbackQueryArg
	case h.URICapture != nil:
		val = sdkkonnectcomp.HashFallbackURICapture
	default:
		return nil
	}
	return &val
}

func translateHealthchecksToSDK(h *configurationv1beta1.KongUpstreamHealthcheck) *sdkkonnectcomp.Healthchecks {
	if h == nil {
		return nil
	}
	hc := &sdkkonnectcomp.Healthchecks{
		Active:  translateActiveHealthcheckToSDK(h.Active),
		Passive: translatePassiveHealthcheckToSDK(h.Passive),
	}
	if h.Threshold != nil {
		v := float64(*h.Threshold)
		hc.Threshold = &v
	}
	return hc
}

func translateActiveHealthcheckToSDK(a *configurationv1beta1.KongUpstreamActiveHealthcheck) *sdkkonnectcomp.Active {
	if a == nil {
		return nil
	}
	active := &sdkkonnectcomp.Active{
		Headers:                a.Headers,
		HTTPPath:               a.HTTPPath,
		HTTPSSni:               a.HTTPSSNI,
		HTTPSVerifyCertificate: a.HTTPSVerifyCertificate,
		Healthy:                translateActiveHealthyToSDK(a.Healthy),
		Unhealthy:              translateActiveUnhealthyToSDK(a.Unhealthy),
	}
	if a.Concurrency != nil {
		v := int64(*a.Concurrency)
		active.Concurrency = &v
	}
	if a.Timeout != nil {
		v := float64(*a.Timeout)
		active.Timeout = &v
	}
	if a.Type != nil {
		t := sdkkonnectcomp.UpstreamType(*a.Type)
		active.Type = &t
	}
	return active
}

func translateActiveHealthyToSDK(h *configurationv1beta1.KongUpstreamHealthcheckHealthy) *sdkkonnectcomp.Healthy {
	if h == nil {
		return nil
	}
	healthy := &sdkkonnectcomp.Healthy{
		HTTPStatuses: translateHTTPStatusesToSDK(h.HTTPStatuses),
	}
	if h.Successes != nil {
		v := int64(*h.Successes)
		healthy.Successes = &v
	}
	if h.Interval != nil {
		v := float64(*h.Interval)
		healthy.Interval = &v
	}
	return healthy
}

func translateActiveUnhealthyToSDK(u *configurationv1beta1.KongUpstreamHealthcheckUnhealthy) *sdkkonnectcomp.Unhealthy {
	if u == nil {
		return nil
	}
	unhealthy := &sdkkonnectcomp.Unhealthy{
		HTTPStatuses: translateHTTPStatusesToSDK(u.HTTPStatuses),
	}
	if u.HTTPFailures != nil {
		v := int64(*u.HTTPFailures)
		unhealthy.HTTPFailures = &v
	}
	if u.TCPFailures != nil {
		v := int64(*u.TCPFailures)
		unhealthy.TCPFailures = &v
	}
	if u.Timeouts != nil {
		v := int64(*u.Timeouts)
		unhealthy.Timeouts = &v
	}
	if u.Interval != nil {
		v := float64(*u.Interval)
		unhealthy.Interval = &v
	}
	return unhealthy
}

func translatePassiveHealthcheckToSDK(p *configurationv1beta1.KongUpstreamPassiveHealthcheck) *sdkkonnectcomp.Passive {
	if p == nil {
		return nil
	}
	passive := &sdkkonnectcomp.Passive{
		Healthy:   translatePassiveHealthyToSDK(p.Healthy),
		Unhealthy: translatePassiveUnhealthyToSDK(p.Unhealthy),
	}
	if p.Type != nil {
		t := sdkkonnectcomp.UpstreamHealthchecksType(*p.Type)
		passive.Type = &t
	}
	return passive
}

func translatePassiveHealthyToSDK(h *configurationv1beta1.KongUpstreamHealthcheckHealthy) *sdkkonnectcomp.UpstreamHealthy {
	if h == nil {
		return nil
	}
	healthy := &sdkkonnectcomp.UpstreamHealthy{
		HTTPStatuses: translateHTTPStatusesToSDK(h.HTTPStatuses),
	}
	if h.Successes != nil {
		v := int64(*h.Successes)
		healthy.Successes = &v
	}
	return healthy
}

func translatePassiveUnhealthyToSDK(u *configurationv1beta1.KongUpstreamHealthcheckUnhealthy) *sdkkonnectcomp.UpstreamUnhealthy {
	if u == nil {
		return nil
	}
	unhealthy := &sdkkonnectcomp.UpstreamUnhealthy{
		HTTPStatuses: translateHTTPStatusesToSDK(u.HTTPStatuses),
	}
	if u.HTTPFailures != nil {
		v := int64(*u.HTTPFailures)
		unhealthy.HTTPFailures = &v
	}
	if u.TCPFailures != nil {
		v := int64(*u.TCPFailures)
		unhealthy.TCPFailures = &v
	}
	if u.Timeouts != nil {
		v := int64(*u.Timeouts)
		unhealthy.Timeouts = &v
	}
	return unhealthy
}

func translateHTTPStatusesToSDK(statuses []configurationv1beta1.HTTPStatus) []int64 {
	if statuses == nil {
		return nil
	}
	out := make([]int64, len(statuses))
	for i, s := range statuses {
		out[i] = int64(s)
	}
	return out
}
