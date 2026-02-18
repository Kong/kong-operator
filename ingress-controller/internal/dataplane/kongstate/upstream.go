package kongstate

import (
	"github.com/kong/go-kong/kong"
	corev1 "k8s.io/api/core/v1"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1beta1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
)

// Upstream is a wrapper around Upstream object in Kong.
type Upstream struct {
	kong.Upstream
	Targets []Target
	// Service this upstream is associated with.
	Service Service
}

func (u *Upstream) overrideHostHeader(anns map[string]string) {
	if u == nil {
		return
	}
	host := annotations.ExtractHostHeader(anns)
	if host == "" {
		return
	}
	u.HostHeader = kong.String(host)
}

// overrideByAnnotation modifies the Kong upstream based on annotations
// on the Kubernetes service.
func (u *Upstream) overrideByAnnotation(anns map[string]string) {
	if u == nil {
		return
	}
	u.overrideHostHeader(anns)
}

func (u *Upstream) overrideByKongUpstreamPolicy(policy *configurationv1beta1.KongUpstreamPolicy) {
	if u == nil {
		return
	}

	kongUpstreamOverrides := TranslateKongUpstreamPolicy(policy.Spec)
	if kongUpstreamOverrides.Algorithm != nil {
		u.Algorithm = kongUpstreamOverrides.Algorithm
	}
	if kongUpstreamOverrides.Slots != nil {
		u.Slots = kongUpstreamOverrides.Slots
	}
	if kongUpstreamOverrides.Healthchecks != nil {
		u.Healthchecks = kongUpstreamOverrides.Healthchecks
	}
	if kongUpstreamOverrides.HashOn != nil {
		u.HashOn = kongUpstreamOverrides.HashOn
	}
	if kongUpstreamOverrides.HashFallback != nil {
		u.HashFallback = kongUpstreamOverrides.HashFallback
	}
	if kongUpstreamOverrides.HashOnHeader != nil {
		u.HashOnHeader = kongUpstreamOverrides.HashOnHeader
	}
	if kongUpstreamOverrides.HashFallbackHeader != nil {
		u.HashFallbackHeader = kongUpstreamOverrides.HashFallbackHeader
	}
	if kongUpstreamOverrides.HashOnCookie != nil {
		u.HashOnCookie = kongUpstreamOverrides.HashOnCookie
	}
	if kongUpstreamOverrides.HashOnCookiePath != nil {
		u.HashOnCookiePath = kongUpstreamOverrides.HashOnCookiePath
	}
	if kongUpstreamOverrides.HashOnQueryArg != nil {
		u.HashOnQueryArg = kongUpstreamOverrides.HashOnQueryArg
	}
	if kongUpstreamOverrides.HashFallbackQueryArg != nil {
		u.HashFallbackQueryArg = kongUpstreamOverrides.HashFallbackQueryArg
	}
	if kongUpstreamOverrides.HashOnURICapture != nil {
		u.HashOnURICapture = kongUpstreamOverrides.HashOnURICapture
	}
	if kongUpstreamOverrides.HashFallbackURICapture != nil {
		u.HashFallbackURICapture = kongUpstreamOverrides.HashFallbackURICapture
	}
	if kongUpstreamOverrides.StickySessionsCookie != nil {
		u.StickySessionsCookie = kongUpstreamOverrides.StickySessionsCookie
	}
	if kongUpstreamOverrides.StickySessionsCookiePath != nil {
		u.StickySessionsCookiePath = kongUpstreamOverrides.StickySessionsCookiePath
	}
}

// override sets Upstream fields by k8s Service's annotations.
func (u *Upstream) override(svc *corev1.Service) {
	if u == nil {
		return
	}

	if svc != nil {
		u.overrideByAnnotation(svc.Annotations)
	}
}
