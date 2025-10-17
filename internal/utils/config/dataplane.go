package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/samber/lo"

	"github.com/kong/kong-operator/pkg/consts"

	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

// KongDefaults are the baseline Kong proxy configuration options needed for
// the proxy to function.
var KongDefaults = map[string]string{
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
	"KONG_PROXY_LISTEN":           fmt.Sprintf("0.0.0.0:%d reuseport backlog=16384, 0.0.0.0:%d http2 ssl reuseport backlog=16384", consts.DataPlaneProxyPort, consts.DataPlaneProxySSLPort),
	"KONG_STATUS_LISTEN":          fmt.Sprintf("0.0.0.0:%d", consts.DataPlaneStatusPort),

	"KONG_ADMIN_LISTEN": fmt.Sprintf("0.0.0.0:%d ssl reuseport backlog=16384", consts.DataPlaneAdminAPIPort),

	// MTLS
	"KONG_ADMIN_SSL_CERT":                     "/var/cluster-certificate/tls.crt",
	"KONG_ADMIN_SSL_CERT_KEY":                 "/var/cluster-certificate/tls.key",
	"KONG_NGINX_ADMIN_SSL_CLIENT_CERTIFICATE": "/var/cluster-certificate/ca.crt",
	"KONG_NGINX_ADMIN_SSL_VERIFY_CLIENT":      "on",
	"KONG_NGINX_ADMIN_SSL_VERIFY_DEPTH":       "3",
}

// kongInKonnectClusterTypeControlPlane are the baseline Kong proxy configuration options needed for
// the proxy to function when configured in Konnect Hybrid ControlPlanes.
var kongInKonnectClusterTypeControlPlane = map[string]string{
	"KONG_ROLE":                          "data_plane",
	"KONG_CLUSTER_MTLS":                  "pki",
	"KONG_CLUSTER_CONTROL_PLANE":         "<CONTROL-PLANE-ENDPOINT>:443",
	"KONG_CLUSTER_SERVER_NAME":           "<CONTROL-PLANE-ENDPOINT>",
	"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "<TELEMETRY-ENDPOINT>:443",
	"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "<TELEMETRY-ENDPOINT>",
	"KONG_CLUSTER_CERT":                  "/etc/secrets/kong-cluster-cert/tls.crt",
	"KONG_CLUSTER_CERT_KEY":              "/etc/secrets/kong-cluster-cert/tls.key",
	"KONG_LUA_SSL_TRUSTED_CERTIFICATE":   "system",
	"KONG_KONNECT_MODE":                  "on",
	"KONG_VITALS":                        "off",
}

// kongInKonnectClusterTypeIngressController are the baseline Kong proxy configuration options needed for
// the proxy to function when configured in Konnect K8s Ingress Controllers ControlPlanes.
var kongInKonnectClusterTypeIngressController = map[string]string{
	"KONG_ROLE":                          "traditional",
	"KONG_CLUSTER_MTLS":                  "pki",
	"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "<TELEMETRY-ENDPOINT>:443",
	"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "<TELEMETRY-ENDPOINT>",
	"KONG_CLUSTER_CERT":                  "/etc/secrets/kong-cluster-cert/tls.crt",
	"KONG_CLUSTER_CERT_KEY":              "/etc/secrets/kong-cluster-cert/tls.key",
	"KONG_LUA_SSL_TRUSTED_CERTIFICATE":   "system",
	"KONG_KONNECT_MODE":                  "on",
	"KONG_VITALS":                        "off",
}

// KongInKonnectDefaults returns the map of Konnect-related env vars properly configured.
func KongInKonnectDefaults(
	dpLabels map[string]konnectv1alpha2.DataPlaneLabelValue,
	konnectExtensionStatus konnectv1alpha2.KonnectExtensionStatus,
) map[string]string {
	newEnvSet := make(map[string]string, len(kongInKonnectClusterTypeControlPlane))
	var template map[string]string

	switch konnectExtensionStatus.Konnect.ClusterType {
	case konnectv1alpha2.ClusterTypeControlPlane:
		template = kongInKonnectClusterTypeControlPlane
	case konnectv1alpha2.ClusterTypeK8sIngressController:
		template = kongInKonnectClusterTypeIngressController
	default:
		// default never happens as the validation is at the CRD level
		panic(fmt.Sprintf("unsupported Konnect cluster type: %s", konnectExtensionStatus.Konnect.ClusterType))
	}

	for k, v := range template {
		v = strings.ReplaceAll(v, "<CONTROL-PLANE-ENDPOINT>", sanitizeEndpoint(konnectExtensionStatus.Konnect.Endpoints.ControlPlaneEndpoint))
		v = strings.ReplaceAll(v, "<TELEMETRY-ENDPOINT>", sanitizeEndpoint(konnectExtensionStatus.Konnect.Endpoints.TelemetryEndpoint))
		newEnvSet[k] = v
	}

	if len(dpLabels) > 0 {
		newEnvSet["KONG_CLUSTER_DP_LABELS"] = clusterDataPlaneLabelStringFromLabels(dpLabels)
	}

	if konnect := konnectExtensionStatus.Konnect; konnect != nil {
		// "HOUDINI_APIGW_KONNECT_API_HOSTNAME":    "eu.control-plane.konghq.tech",             //
		if konnect.Endpoints.ServerURL != "" {
			// newEnvSet["HOUDINI_APIGW_KONNECT_API_HOSTNAME"] = sanitizeEndpoint(konnect.Endpoints.ServerURL)
			newEnvSet["HOUDINI_APIGW_KONNECT_API_HOSTNAME"] = replaceRegionAPI(sanitizeEndpoint(konnect.Endpoints.ServerURL))
		}
		// "HOUDINI_APIGW_KONNECT_GATEWAY_ID":      "${HOUDINI_APIGW_KONNECT_GATEWAY_ID}",      //
		if konnect.ControlPlaneID != "" {
			// newEnvSet["HOUDINI_APIGW_KONNECT_GATEWAY_ID"] = konnect.ControlPlaneID
			newEnvSet["HOUDINI_APIGW_KONNECT_GATEWAY_ID"] = "0199ed01-7064-752f-8225-d1d961aa583f"
		}
	}

	return newEnvSet
}

// replaceRegionAPI replaces "<region>.api.konghq.tech" with "<region>.control-plane.konghq.tech"
// Example: "eu.api.konghq.tech" -> "eu.control-plane.konghq.tech"
// It preserves the region part and works for multiple occurrences.
func replaceRegionAPI(s string) string {
	re := regexp.MustCompile(`\b([a-z0-9-]+)\.api\.konghq\.tech\b`)
	return re.ReplaceAllString(s, "${1}.control-plane.konghq.tech")
}

func sanitizeEndpoint(endpoint string) string {
	return strings.TrimPrefix(endpoint, "https://")
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
