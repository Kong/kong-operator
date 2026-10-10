package kongstate

// KIC standalone support for v1alpha1 Kong Gateway entity CRDs.
//
// This file implements translation from api/configuration/v1alpha1 types
// (KongService, KongRoute, KongUpstream, KongTarget, KongCertificate,
// KongCACertificate, KongSNI, KongPluginBinding) to the kong.* types
// used in KongState, enabling db-less mode without a Konnect control plane.
//
// The Konnect reconciliation path (sdk-konnect-go) is untouched.
// Enabled by feature gate: KongServiceV1Alpha1.

import (
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kong/go-kong/kong"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/failures"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
)

// FillFromKongServicesV1Alpha1 translates KongService, KongRoute, KongUpstream,
// and KongTarget CRDs from the store into KongState (kong.* types) for KIC standalone.
func (ks *KongState) FillFromKongServicesV1Alpha1(
	logger logr.Logger,
	s store.Storer,
	failuresCollector *failures.ResourceFailuresCollector,
) {
	// --- KongUpstream → Upstream ---
	// Build a map of upstream name → kongstate.Upstream for later KongTarget attachment.
	upstreamByName := map[string]*Upstream{}
	for _, ku := range s.ListKongUpstreamsV1Alpha1() {
		name := ku.Spec.Name
		if name == "" {
			name = ku.Name
		}
		u := translateKongUpstreamV1Alpha1(ku)
		ks.Upstreams = append(ks.Upstreams, u)
		upstreamByName[name] = &ks.Upstreams[len(ks.Upstreams)-1]
	}

	// --- KongTarget → Upstream.Targets ---
	for _, kt := range s.ListKongTargetsV1Alpha1() {
		upstreamName := kt.Spec.UpstreamRef.Name
		upstream, ok := upstreamByName[upstreamName]
		if !ok {
			failuresCollector.PushResourceFailure(
				fmt.Sprintf("KongTarget %q: referenced KongUpstream %q not found", kt.Name, upstreamName),
				kt,
			)
			continue
		}
		target := kong.Target{
			Target: kong.String(kt.Spec.Target),
			Weight: kong.Int(kt.Spec.Weight),
			Tags:   util.GenerateTagsForObject(kt),
		}
		upstream.Targets = append(upstream.Targets, Target{Target: target})
	}

	// --- KongService → Service ---
	// Build a map of k8s name → kongstate.Service for later KongRoute attachment.
	svcByName := map[string]*Service{}
	for _, ks2 := range s.ListKongServicesV1Alpha1() {
		svc := translateKongServiceV1Alpha1(ks2)
		ks.Services = append(ks.Services, svc)
		svcByName[ks2.Name] = &ks.Services[len(ks.Services)-1]
	}

	// --- KongRoute → Service.Routes ---
	for _, kr := range s.ListKongRoutesV1Alpha1() {
		route := translateKongRouteV1Alpha1(kr)
		if kr.Spec.ServiceRef != nil && kr.Spec.ServiceRef.NamespacedRef != nil {
			// Route attached to a specific KongService.
			svcName := kr.Spec.ServiceRef.NamespacedRef.Name
			svc, ok := svcByName[svcName]
			if !ok {
				failuresCollector.PushResourceFailure(
					fmt.Sprintf("KongRoute %q: referenced KongService %q not found", kr.Name, svcName),
					kr,
				)
				continue
			}
			svc.Routes = append(svc.Routes, route)
		} else {
			// Serviceless route: wrap in a placeholder service.
			placeholder := Service{
				Service: kong.Service{
					Name: kong.String("serviceless." + kr.Namespace + "." + kr.Name),
					Host: kong.String("0.0.0.0"),
					Port: kong.Int(1),
					// Kong requires a valid protocol even for serviceless routes.
					Protocol: kong.String("http"),
				},
				Parent: kr,
				Routes: []Route{route},
			}
			ks.Services = append(ks.Services, placeholder)
		}
	}
}

// FillFromKongCertificatesV1Alpha1 translates KongCertificate, KongCACertificate,
// and KongSNI CRDs from the store into KongState for KIC standalone.
func (ks *KongState) FillFromKongCertificatesV1Alpha1(
	logger logr.Logger,
	s store.Storer,
	failuresCollector *failures.ResourceFailuresCollector,
) {
	// --- KongCertificate → Certificate ---
	// Build a map of k8s name → index in ks.Certificates for SNI attachment.
	certIndexByName := map[string]int{}
	for _, kc := range s.ListKongCertificatesV1Alpha1() {
		cert, err := translateKongCertificateV1Alpha1(kc, s)
		if err != nil {
			failuresCollector.PushResourceFailure(
				fmt.Sprintf("KongCertificate %q: %v", kc.Name, err),
				kc,
			)
			continue
		}
		certIndexByName[kc.Name] = len(ks.Certificates)
		ks.Certificates = append(ks.Certificates, Certificate{Certificate: cert})
	}

	// --- KongCACertificate → CACertificates ---
	for _, kca := range s.ListKongCACertificatesV1Alpha1() {
		caCert, err := translateKongCACertificateV1Alpha1(kca, s)
		if err != nil {
			failuresCollector.PushResourceFailure(
				fmt.Sprintf("KongCACertificate %q: %v", kca.Name, err),
				kca,
			)
			continue
		}
		ks.CACertificates = append(ks.CACertificates, caCert)
	}

	// --- KongSNI → Certificate.SNIs ---
	for _, ksni := range s.ListKongSNIsV1Alpha1() {
		certName := ksni.Spec.CertificateRef.Name
		idx, ok := certIndexByName[certName]
		if !ok {
			failuresCollector.PushResourceFailure(
				fmt.Sprintf("KongSNI %q: referenced KongCertificate %q not found", ksni.Name, certName),
				ksni,
			)
			continue
		}
		sniName := ksni.Spec.Name
		ks.Certificates[idx].SNIs = append(ks.Certificates[idx].SNIs, kong.String(sniName))
	}
}

// FillFromKongPluginBindingsV1Alpha1 translates KongPluginBinding CRDs from the
// store into KongState.Plugins for KIC standalone.
func (ks *KongState) FillFromKongPluginBindingsV1Alpha1(
	logger logr.Logger,
	s store.Storer,
	failuresCollector *failures.ResourceFailuresCollector,
) {
	for _, kpb := range s.ListKongPluginBindingsV1Alpha1() {
		plugins, err := translateKongPluginBindingV1Alpha1(kpb, s, logger)
		if err != nil {
			failuresCollector.PushResourceFailure(
				fmt.Sprintf("KongPluginBinding %q: %v", kpb.Name, err),
				kpb,
			)
			continue
		}
		for _, p := range plugins {
			ks.Plugins = append(ks.Plugins, Plugin{
				Plugin:    p,
				K8sParent: kpb,
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Translation helpers
// ---------------------------------------------------------------------------

func translateKongServiceV1Alpha1(ks *configurationv1alpha1.KongService) Service {
	spec := ks.Spec.KongServiceAPISpec
	name := ks.Name
	if spec.Name != nil {
		name = *spec.Name
	}
	svc := kong.Service{
		Name:     kong.String(name),
		Host:     kong.String(spec.Host),
		Tags:     tagsFromV1Alpha1(spec.Tags, util.GenerateTagsForObject(ks)),
	}
	if spec.Port != 0 {
		p := int(spec.Port)
		svc.Port = &p
	}
	if spec.Path != nil {
		svc.Path = spec.Path
	}
	if spec.Protocol != "" {
		svc.Protocol = kong.String(string(spec.Protocol))
	}
	if spec.ConnectTimeout != nil {
		v := int(*spec.ConnectTimeout)
		svc.ConnectTimeout = &v
	}
	if spec.ReadTimeout != nil {
		v := int(*spec.ReadTimeout)
		svc.ReadTimeout = &v
	}
	if spec.WriteTimeout != nil {
		v := int(*spec.WriteTimeout)
		svc.WriteTimeout = &v
	}
	if spec.Retries != nil {
		v := int(*spec.Retries)
		svc.Retries = &v
	}
	if spec.Enabled != nil {
		svc.Enabled = spec.Enabled
	}
	if spec.TLSVerify != nil {
		svc.TLSVerify = spec.TLSVerify
	}
	if spec.TLSVerifyDepth != nil {
		v := int(*spec.TLSVerifyDepth)
		svc.TLSVerifyDepth = &v
	}
	return Service{
		Service: svc,
		Parent:  ks,
	}
}

func translateKongRouteV1Alpha1(kr *configurationv1alpha1.KongRoute) Route {
	spec := kr.Spec.KongRouteAPISpec
	name := kr.Name
	if spec.Name != nil {
		name = *spec.Name
	}
	route := kong.Route{
		Name:  kong.String(name),
		Hosts: stringSliceToKongStringSlice(spec.Hosts),
		Paths: stringSliceToKongStringSlice(spec.Paths),
		Tags:  tagsFromV1Alpha1(spec.Tags, util.GenerateTagsForObject(kr)),
	}
	if spec.Methods != nil {
		route.Methods = stringSliceToKongStringSlice(spec.Methods)
	}
	if spec.StripPath != nil {
		route.StripPath = spec.StripPath
	}
	if spec.PreserveHost != nil {
		route.PreserveHost = spec.PreserveHost
	}
	if spec.RegexPriority != nil {
		v := int(*spec.RegexPriority)
		route.RegexPriority = &v
	}
	if spec.RequestBuffering != nil {
		route.RequestBuffering = spec.RequestBuffering
	}
	if spec.ResponseBuffering != nil {
		route.ResponseBuffering = spec.ResponseBuffering
	}
	if spec.PathHandling != nil {
		route.PathHandling = kong.String(string(*spec.PathHandling))
	}
	if spec.HTTPSRedirectStatusCode != nil {
		v := int(*spec.HTTPSRedirectStatusCode)
		route.HTTPSRedirectStatusCode = &v
	}
	// Protocols: []sdkkonnectcomp.RouteJSONProtocols → []*string
	if len(spec.Protocols) > 0 {
		for _, p := range spec.Protocols {
			ps := string(p)
			route.Protocols = append(route.Protocols, &ps)
		}
	}
	// Snis
	if len(spec.Snis) > 0 {
		route.SNIs = stringSliceToKongStringSlice(spec.Snis)
	}
	// Headers: map[string][]string → map[string][]string (same)
	if len(spec.Headers) > 0 {
		route.Headers = spec.Headers
	}
	// Sources/Destinations: use JSON bridge for struct conversion.
	if len(spec.Sources) > 0 {
		_ = convertViaJSON(spec.Sources, &route.Sources)
	}
	if len(spec.Destinations) > 0 {
		_ = convertViaJSON(spec.Destinations, &route.Destinations)
	}
	return Route{Route: route}
}

func translateKongUpstreamV1Alpha1(ku *configurationv1alpha1.KongUpstream) Upstream {
	spec := ku.Spec.KongUpstreamAPISpec
	name := ku.Spec.Name
	if name == "" {
		name = ku.Name
	}
	u := kong.Upstream{
		Name: kong.String(name),
		Tags: tagsFromV1Alpha1(spec.Tags, util.GenerateTagsForObject(ku)),
	}
	if spec.Algorithm != nil {
		u.Algorithm = kong.String(string(*spec.Algorithm))
	}
	if spec.Slots != nil {
		v := int(*spec.Slots)
		u.Slots = &v
	}
	if spec.HashOn != nil {
		u.HashOn = kong.String(string(*spec.HashOn))
	}
	if spec.HashFallback != nil {
		u.HashFallback = kong.String(string(*spec.HashFallback))
	}
	if spec.HashOnHeader != nil {
		u.HashOnHeader = spec.HashOnHeader
	}
	if spec.HashFallbackHeader != nil {
		u.HashFallbackHeader = spec.HashFallbackHeader
	}
	if spec.HashOnCookie != nil {
		u.HashOnCookie = spec.HashOnCookie
	}
	if spec.HashOnCookiePath != nil {
		u.HashOnCookiePath = spec.HashOnCookiePath
	}
	if spec.HashOnQueryArg != nil {
		u.HashOnQueryArg = spec.HashOnQueryArg
	}
	if spec.HashFallbackQueryArg != nil {
		u.HashFallbackQueryArg = spec.HashFallbackQueryArg
	}
	if spec.HashOnURICapture != nil {
		u.HashOnURICapture = spec.HashOnURICapture
	}
	if spec.HashFallbackURICapture != nil {
		u.HashFallbackURICapture = spec.HashFallbackURICapture
	}
	if spec.HostHeader != nil {
		u.HostHeader = spec.HostHeader
	}
	if spec.UseSrvName != nil {
		u.UseSrvName = spec.UseSrvName
	}
	// Healthchecks: sdkkonnectcomp.Healthchecks → kong.Healthcheck via JSON round-trip.
	if spec.Healthchecks != nil {
		var hc kong.Healthcheck
		if err := convertViaJSON(spec.Healthchecks, &hc); err == nil {
			u.Healthchecks = &hc
		}
	}
	return Upstream{Upstream: u}
}

func translateKongCertificateV1Alpha1(kc *configurationv1alpha1.KongCertificate, s store.Storer) (kong.Certificate, error) {
	cert := kong.Certificate{
		Tags: tagsFromV1Alpha1(kc.Spec.Tags, util.GenerateTagsForObject(kc)),
	}
	sourceType := configurationv1alpha1.KongCertificateSourceTypeInline
	if kc.Spec.Type != nil {
		sourceType = *kc.Spec.Type
	}
	switch sourceType {
	case configurationv1alpha1.KongCertificateSourceTypeInline:
		cert.Cert = kong.String(kc.Spec.Cert)
		cert.Key = kong.String(kc.Spec.Key)
		if kc.Spec.CertAlt != "" {
			cert.CertAlt = kong.String(kc.Spec.CertAlt)
		}
		if kc.Spec.KeyAlt != "" {
			cert.KeyAlt = kong.String(kc.Spec.KeyAlt)
		}
	case configurationv1alpha1.KongCertificateSourceTypeSecretRef:
		if kc.Spec.SecretRef == nil {
			return cert, fmt.Errorf("secretRef is required when type is 'secretRef'")
		}
		ns := kc.Namespace
		if kc.Spec.SecretRef.Namespace != nil && *kc.Spec.SecretRef.Namespace != "" {
			ns = *kc.Spec.SecretRef.Namespace
		}
		secret, err := s.GetSecret(ns, kc.Spec.SecretRef.Name)
		if err != nil {
			return cert, fmt.Errorf("failed to get secret %s/%s: %w", ns, kc.Spec.SecretRef.Name, err)
		}
		tlsCrt, ok := secret.Data["tls.crt"]
		if !ok {
			return cert, fmt.Errorf("secret %s/%s missing key tls.crt", ns, kc.Spec.SecretRef.Name)
		}
		tlsKey, ok := secret.Data["tls.key"]
		if !ok {
			return cert, fmt.Errorf("secret %s/%s missing key tls.key", ns, kc.Spec.SecretRef.Name)
		}
		cert.Cert = kong.String(string(tlsCrt))
		cert.Key = kong.String(string(tlsKey))
		// Alt cert from optional SecretRefAlt.
		if kc.Spec.SecretRefAlt != nil {
			altNS := kc.Namespace
			if kc.Spec.SecretRefAlt.Namespace != nil && *kc.Spec.SecretRefAlt.Namespace != "" {
				altNS = *kc.Spec.SecretRefAlt.Namespace
			}
			altSecret, err := s.GetSecret(altNS, kc.Spec.SecretRefAlt.Name)
			if err == nil {
				if v, ok := altSecret.Data["tls.crt"]; ok {
					cert.CertAlt = kong.String(string(v))
				}
				if v, ok := altSecret.Data["tls.key"]; ok {
					cert.KeyAlt = kong.String(string(v))
				}
			}
		}
	}
	return cert, nil
}

func translateKongCACertificateV1Alpha1(kca *configurationv1alpha1.KongCACertificate, s store.Storer) (kong.CACertificate, error) {
	caCert := kong.CACertificate{
		Tags: tagsFromV1Alpha1(kca.Spec.Tags, util.GenerateTagsForObject(kca)),
	}
	sourceType := configurationv1alpha1.KongCACertificateSourceTypeInline
	if kca.Spec.Type != nil {
		sourceType = *kca.Spec.Type
	}
	switch sourceType {
	case configurationv1alpha1.KongCACertificateSourceTypeInline:
		caCert.Cert = kong.String(kca.Spec.Cert)
	case configurationv1alpha1.KongCACertificateSourceTypeSecretRef:
		if kca.Spec.SecretRef == nil {
			return caCert, fmt.Errorf("secretRef is required when type is 'secretRef'")
		}
		ns := kca.Namespace
		if kca.Spec.SecretRef.Namespace != nil && *kca.Spec.SecretRef.Namespace != "" {
			ns = *kca.Spec.SecretRef.Namespace
		}
		secret, err := s.GetSecret(ns, kca.Spec.SecretRef.Name)
		if err != nil {
			return caCert, fmt.Errorf("failed to get secret %s/%s: %w", ns, kca.Spec.SecretRef.Name, err)
		}
		tlsCrt, ok := secret.Data["ca.crt"]
		if !ok {
			return caCert, fmt.Errorf("secret %s/%s missing key ca.crt", ns, kca.Spec.SecretRef.Name)
		}
		caCert.Cert = kong.String(string(tlsCrt))
	}
	return caCert, nil
}

func translateKongPluginBindingV1Alpha1(
	kpb *configurationv1alpha1.KongPluginBinding,
	s store.Storer,
	_ logr.Logger,
) ([]kong.Plugin, error) {
	// Lookup the plugin definition.
	pluginRef := kpb.Spec.PluginReference
	kind := "KongPlugin"
	if pluginRef.Kind != nil {
		kind = *pluginRef.Kind
	}

	var pluginConfig kong.Configuration
	var pluginName string

	switch kind {
	case "KongPlugin":
		ns := kpb.Namespace
		if pluginRef.Namespace != "" {
			ns = pluginRef.Namespace
		}
		kp, err := s.GetKongPlugin(ns, pluginRef.Name)
		if err != nil {
			return nil, fmt.Errorf("KongPlugin %s/%s not found: %w", ns, pluginRef.Name, err)
		}
		pluginName = kp.PluginName
		pluginConfig, err = RawConfigToConfiguration(kp.Config.Raw)
		if err != nil {
			return nil, fmt.Errorf("KongPlugin %s/%s config parse error: %w", ns, pluginRef.Name, err)
		}
	case "KongClusterPlugin":
		kcp, err := s.GetKongClusterPlugin(pluginRef.Name)
		if err != nil {
			return nil, fmt.Errorf("KongClusterPlugin %s not found: %w", pluginRef.Name, err)
		}
		pluginName = kcp.PluginName
		pluginConfig, err = RawConfigToConfiguration(kcp.Config.Raw)
		if err != nil {
			return nil, fmt.Errorf("KongClusterPlugin %s config parse error: %w", pluginRef.Name, err)
		}
	default:
		return nil, fmt.Errorf("unsupported plugin kind: %s", kind)
	}

	p := kong.Plugin{
		Name:   kong.String(pluginName),
		Config: pluginConfig,
		Tags:   util.GenerateTagsForObject(kpb),
	}

	if kpb.Spec.Scope == configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane {
		// Global plugin — no target associations.
		return []kong.Plugin{p}, nil
	}

	// Target associations: we generate one plugin entry per target combination.
	// For KIC db-less, the plugin must reference the service/route by name.
	if kpb.Spec.Targets == nil {
		return []kong.Plugin{p}, nil
	}
	targets := kpb.Spec.Targets

	// Build association object references.
	if targets.ServiceReference != nil {
		svcName := targets.ServiceReference.Name
		p.Service = &kong.Service{Name: kong.String(svcName)}
	}
	if targets.RouteReference != nil {
		routeName := targets.RouteReference.Name
		p.Route = &kong.Route{Name: kong.String(routeName)}
	}
	if targets.ConsumerReference != nil {
		p.Consumer = &kong.Consumer{Username: kong.String(targets.ConsumerReference.Name)}
	}
	if targets.ConsumerGroupReference != nil {
		p.ConsumerGroup = &kong.ConsumerGroup{Name: kong.String(targets.ConsumerGroupReference.Name)}
	}

	return []kong.Plugin{p}, nil
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// convertViaJSON marshals src to JSON then unmarshals into dst.
// This acts as a bridge between sdkkonnectcomp.* and kong.* types
// that share the same JSON field names (both generated from Kong API spec).
func convertViaJSON(src, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// tagsFromV1Alpha1 merges CRD-level tags with generated k8s metadata tags.
func tagsFromV1Alpha1(specTags []string, k8sTags []*string) []*string {
	result := make([]*string, 0, len(specTags)+len(k8sTags))
	for i := range specTags {
		result = append(result, &specTags[i])
	}
	result = append(result, k8sTags...)
	return result
}

// stringSliceToKongStringSlice converts []string → []*string.
func stringSliceToKongStringSlice(in []string) []*string {
	out := make([]*string, len(in))
	for i := range in {
		s := in[i]
		out[i] = &s
	}
	return out
}

