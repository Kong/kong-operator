package config

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/ipfamily"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/internal/utils/konnectendpoint"
)

// ListenValue renders a Kong listen value for the given port, bound to the
// wildcard address(es) of the given IP family.
//
// It returns an error for ipfamily.Auto and any unrecognized family: the IP
// family must be resolved to a concrete value (see ipfamily.Resolve) before
// listen values are rendered, because silently assuming a concrete family
// (e.g. IPv4) would render DataPlanes' Kong listens unreachable on clusters
// of a different family.
func ListenValue(family ipfamily.IPFamily, port int, options ...string) (string, error) {
	var suffix string
	if len(options) > 0 {
		suffix = " " + strings.Join(options, " ")
	}
	p := strconv.Itoa(port)
	ipv4 := net.JoinHostPort(consts.ListenAddressIPv4, p) + suffix
	ipv6 := net.JoinHostPort(consts.ListenAddressIPv6, p) + suffix

	switch family {
	case ipfamily.IPv6:
		return ipv6, nil
	case ipfamily.Dual:
		return fmt.Sprintf("%s, %s", ipv4, ipv6), nil
	case ipfamily.IPv4:
		return ipv4, nil
	default:
		return "", fmt.Errorf("cannot render Kong listen value for IP family %q: the IP family must be resolved before rendering listen values", family)
	}
}

// KongDefaults returns the baseline Kong proxy configuration options needed
// for the proxy to function, with its listen addresses rendered for the
// given IP family. It returns an error for ipfamily.Auto and any
// unrecognized family (see ListenValue).
func KongDefaults(family ipfamily.IPFamily) (map[string]string, error) {
	proxyListenHTTP, err := ListenValue(family, consts.DataPlaneProxyPort, "http2", "reuseport", "backlog=16384")
	if err != nil {
		return nil, err
	}
	proxyListenHTTPS, err := ListenValue(family, consts.DataPlaneProxySSLPort, "http2", "ssl", "reuseport", "backlog=16384")
	if err != nil {
		return nil, err
	}
	statusListen, err := ListenValue(family, consts.DataPlaneStatusPort)
	if err != nil {
		return nil, err
	}
	adminListen, err := ListenValue(family, consts.DataPlaneAdminAPIPort, "ssl", "reuseport", "backlog=16384")
	if err != nil {
		return nil, err
	}

	defaults := map[string]string{
		"KONG_ADMIN_ACCESS_LOG":       "/dev/stdout",
		"KONG_ADMIN_ERROR_LOG":        "/dev/stderr",
		"KONG_ADMIN_GUI_ACCESS_LOG":   "/dev/stdout",
		"KONG_ADMIN_GUI_ERROR_LOG":    "/dev/stderr",
		"KONG_CLUSTER_LISTEN":         "off",
		"KONG_DATABASE":               "off",
		"KONG_NGINX_WORKER_PROCESSES": "2",
		kongPluginsEnvVarName:         kongPluginsDefaultValue,
		"KONG_PORTAL_API_ACCESS_LOG":  "/dev/stdout",
		"KONG_PORTAL_API_ERROR_LOG":   "/dev/stderr",
		"KONG_PORT_MAPS":              "80:8000, 443:8443",
		"KONG_PROXY_ACCESS_LOG":       "/dev/stdout",
		"KONG_PROXY_ERROR_LOG":        "/dev/stderr",
		"KONG_PROXY_LISTEN": strings.Join([]string{
			proxyListenHTTP,
			proxyListenHTTPS,
		}, ", "),
		"KONG_STATUS_LISTEN":                  statusListen,
		"KONG_USE_STANDARD_GRPC_STATUS_CODES": "on",

		"KONG_ADMIN_LISTEN": adminListen,

		// MTLS
		"KONG_ADMIN_SSL_CERT":                     "/var/cluster-certificate/tls.crt",
		"KONG_ADMIN_SSL_CERT_KEY":                 "/var/cluster-certificate/tls.key",
		"KONG_NGINX_ADMIN_SSL_CLIENT_CERTIFICATE": "/var/cluster-certificate/ca.crt",
		"KONG_NGINX_ADMIN_SSL_VERIFY_CLIENT":      "on",
		"KONG_NGINX_ADMIN_SSL_VERIFY_DEPTH":       "3",
	}

	switch family {
	case ipfamily.IPv6, ipfamily.Dual:
		defaults["KONG_DNS_ORDER"] = "LAST,SRV,AAAA,A,CNAME"
	case ipfamily.IPv4, ipfamily.Auto:
	}

	return defaults, nil
}

// DisableAdminAPI turns off the Kong Admin API listener and drops the related
// admin TLS/mTLS settings from the given env map. It mutates env in place, so
// callers must not pass a shared map.
func DisableAdminAPI(env map[string]string) {
	if env == nil {
		return
	}
	env["KONG_ADMIN_LISTEN"] = "off"
	for _, k := range []string{
		"KONG_ADMIN_SSL_CERT",
		"KONG_ADMIN_SSL_CERT_KEY",
		"KONG_NGINX_ADMIN_SSL_CLIENT_CERTIFICATE",
		"KONG_NGINX_ADMIN_SSL_VERIFY_CLIENT",
		"KONG_NGINX_ADMIN_SSL_VERIFY_DEPTH",
	} {
		delete(env, k)
	}
}

// kongInKonnectClusterTypeControlPlane are the baseline Kong proxy configuration options needed for
// the proxy to function when configured in Konnect Hybrid ControlPlanes.
var kongInKonnectClusterTypeControlPlane = map[string]string{
	"KONG_ROLE":                          "data_plane",
	"KONG_CLUSTER_MTLS":                  "pki",
	"KONG_CLUSTER_CONTROL_PLANE":         "<CONTROL-PLANE-ADDRESS>",
	"KONG_CLUSTER_SERVER_NAME":           "<CONTROL-PLANE-SERVER-NAME>",
	"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "<TELEMETRY-ADDRESS>",
	"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "<TELEMETRY-SERVER-NAME>",
	"KONG_CLUSTER_CERT":                  "/etc/secrets/kong-cluster-cert/tls.crt",
	"KONG_CLUSTER_CERT_KEY":              "/etc/secrets/kong-cluster-cert/tls.key",
	"KONG_INCREMENTAL_SYNC":              "on",
	"KONG_LUA_SSL_TRUSTED_CERTIFICATE":   "system",
	"KONG_KONNECT_MODE":                  "on",
	"KONG_VITALS":                        "off",
}

// kongInKonnectClusterTypeIngressController are the baseline Kong proxy configuration options needed for
// the proxy to function when configured in Konnect K8s Ingress Controllers ControlPlanes.
var kongInKonnectClusterTypeIngressController = map[string]string{
	"KONG_ROLE":                          "traditional",
	"KONG_CLUSTER_MTLS":                  "pki",
	"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "<TELEMETRY-ADDRESS>",
	"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "<TELEMETRY-SERVER-NAME>",
	"KONG_CLUSTER_CERT":                  "/etc/secrets/kong-cluster-cert/tls.crt",
	"KONG_CLUSTER_CERT_KEY":              "/etc/secrets/kong-cluster-cert/tls.key",
	"KONG_LUA_SSL_TRUSTED_CERTIFICATE":   "system",
	"KONG_KONNECT_MODE":                  "on",
	"KONG_VITALS":                        "off",
}

// KongInKonnectDefaults returns the map of Konnect-related env vars properly configured.
// It returns an error when the control plane or telemetry endpoint value cannot be used.
func KongInKonnectDefaults(
	dpLabels map[string]konnectv1alpha2.DataPlaneLabelValue,
	konnectExtensionStatus konnectv1alpha2.KonnectExtensionStatus,
) (map[string]string, error) {
	newEnvSet := make(map[string]string, len(kongInKonnectClusterTypeControlPlane))
	var template map[string]string

	switch konnectExtensionStatus.Konnect.ClusterType {
	case konnectv1alpha2.ClusterTypeControlPlane, konnectv1alpha2.ClusterTypeControlPlaneGroup:
		template = kongInKonnectClusterTypeControlPlane
	case konnectv1alpha2.ClusterTypeK8sIngressController:
		template = kongInKonnectClusterTypeIngressController
	default:
		// default never happens as the validation is at the CRD level
		panic(fmt.Sprintf("unsupported Konnect cluster type: %s", konnectExtensionStatus.Konnect.ClusterType))
	}

	cpEndpoint, err := konnectendpoint.Parse(konnectExtensionStatus.Konnect.Endpoints.ControlPlaneEndpoint)
	if err != nil {
		return nil, fmt.Errorf("control plane endpoint %q: %w", konnectExtensionStatus.Konnect.Endpoints.ControlPlaneEndpoint, err)
	}
	tpEndpoint, err := konnectendpoint.Parse(konnectExtensionStatus.Konnect.Endpoints.TelemetryEndpoint)
	if err != nil {
		return nil, fmt.Errorf("telemetry endpoint %q: %w", konnectExtensionStatus.Konnect.Endpoints.TelemetryEndpoint, err)
	}
	endpointValues := map[string]string{
		"<CONTROL-PLANE-ADDRESS>":     cpEndpoint.ControlPlaneAddress(),
		"<CONTROL-PLANE-SERVER-NAME>": cpEndpoint.ServerName(),
		"<TELEMETRY-ADDRESS>":         tpEndpoint.TelemetryAddress(),
		"<TELEMETRY-SERVER-NAME>":     tpEndpoint.ServerName(),
	}

	for k, v := range template {
		for placeholder, value := range endpointValues {
			v = strings.ReplaceAll(v, placeholder, value)
		}
		newEnvSet[k] = v
	}

	if len(dpLabels) > 0 {
		newEnvSet["KONG_CLUSTER_DP_LABELS"] = clusterDataPlaneLabelStringFromLabels(dpLabels)
	}

	return newEnvSet, nil
}

func clusterDataPlaneLabelStringFromLabels(labels map[string]konnectv1alpha2.DataPlaneLabelValue) string {
	labelStrings := lo.MapToSlice(
		labels, func(k string, v konnectv1alpha2.DataPlaneLabelValue) string {
			return fmt.Sprintf("%s:%s", k, v)
		},
	)
	sort.Strings(labelStrings)
	return strings.Join(labelStrings, ",")
}
