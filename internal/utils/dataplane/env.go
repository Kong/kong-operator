package dataplane

import (
	"fmt"
	"strings"

	"github.com/kong/gateway-operator/pkg/consts"
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

// KongInKonnectDefaults are the baseline Kong proxy configuration options needed for
// the proxy to function when configured in Konnect.
var kongInKonnectDefaultsTemplate = map[string]string{
	"KONG_ROLE":                          "data_plane",
	"KONG_CLUSTER_MTLS":                  "pki",
	"KONG_CLUSTER_CONTROL_PLANE":         "<CP-ID>.<REGION>.cp0.<SERVER>:443",
	"KONG_CLUSTER_SERVER_NAME":           "<CP-ID>.<REGION>.cp0.<SERVER>",
	"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "<CP-ID>.<REGION>.tp0.<SERVER>:443",
	"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "<CP-ID>.<REGION>.tp0.<SERVER>",
	"KONG_CLUSTER_CERT":                  "/etc/secrets/kong-cluster-cert/tls.crt",
	"KONG_CLUSTER_CERT_KEY":              "/etc/secrets/kong-cluster-cert/tls.key",
	"KONG_LUA_SSL_TRUSTED_CERTIFICATE":   "system",
	"KONG_KONNECT_MODE":                  "on",
	"KONG_VITALS":                        "off",
}

// KongInKonnectDefaults returnes the map of Konnect-related env vars properly configured.
func KongInKonnectDefaults(
	controlPlane,
	region,
	server string,
) map[string]string {
	newEnvSet := make(map[string]string, len(kongInKonnectDefaultsTemplate))
	for k, v := range kongInKonnectDefaultsTemplate {
		v = strings.ReplaceAll(v, "<CP-ID>", controlPlane)
		v = strings.ReplaceAll(v, "<REGION>", region)
		v = strings.ReplaceAll(v, "<SERVER>", server)
		newEnvSet[k] = v
	}
	return newEnvSet
}
