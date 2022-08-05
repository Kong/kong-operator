package dataplane

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// Dataplane Utils - Config Vars & Consts
// -----------------------------------------------------------------------------

const (
	// DefaultHTTPPort is the default port used for HTTP ingress network traffic
	// from outside clusters.
	DefaultHTTPPort = 80

	// DefaultHTTPSPort is the default port used for HTTPS ingress network traffic
	// from outside clusters.
	DefaultHTTPSPort = 443

	// DefaultKongHTTPPort is the Kong proxy's default port used for HTTP traffic
	DefaultKongHTTPPort = 8000

	// DefaultKongHTTPSPort is the Kong proxy's default port used for HTTPS traffic
	DefaultKongHTTPSPort = 8443

	// DefaultKongHTTPSPort is the default port used for Kong Admin API traffic
	DefaultKongAdminPort = 8444

	// DefaultKongStatusPort is the default port used for Kong proxy status
	DefaultKongStatusPort = 8100
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
	"KONG_PLUGINS":                "bundled",
	"KONG_PORTAL_API_ACCESS_LOG":  "/dev/stdout",
	"KONG_PORTAL_API_ERROR_LOG":   "/dev/stderr",
	"KONG_PORT_MAPS":              "80:8000, 443:8443",
	"KONG_PROXY_ACCESS_LOG":       "/dev/stdout",
	"KONG_PROXY_ERROR_LOG":        "/dev/stderr",
	"KONG_PROXY_LISTEN":           fmt.Sprintf("0.0.0.0:%d reuseport backlog=16384, 0.0.0.0:%d http2 ssl reuseport backlog=16384", DefaultKongHTTPPort, DefaultKongHTTPSPort),
	"KONG_STATUS_LISTEN":          fmt.Sprintf("0.0.0.0:%d", DefaultKongStatusPort),

	// TODO: reconfigure following https://github.com/Kong/gateway-operator/issues/7
	"KONG_ADMIN_LISTEN": fmt.Sprintf("0.0.0.0:%d ssl reuseport backlog=16384", DefaultKongAdminPort),

	// MTLS
	"KONG_ADMIN_SSL_CERT":                     "/var/cluster-certificate/tls.crt",
	"KONG_ADMIN_SSL_CERT_KEY":                 "/var/cluster-certificate/tls.key",
	"KONG_NGINX_ADMIN_SSL_CLIENT_CERTIFICATE": "/var/cluster-certificate/ca.crt",
	"KONG_NGINX_ADMIN_SSL_VERIFY_CLIENT":      "on",
	"KONG_NGINX_ADMIN_SSL_VERIFY_DEPTH":       "3",
}

// -----------------------------------------------------------------------------
// DataPlane Utils - Config
// -----------------------------------------------------------------------------

// SetDataPlaneDefaults sets any unset default configuration options on the
// DataPlane. No configuration is overridden. EnvVars are sorted
// lexographically as a side effect.
func SetDataPlaneDefaults(spec *operatorv1alpha1.DataPlaneDeploymentOptions) {
	for k, v := range KongDefaults {
		envVar := corev1.EnvVar{Name: k, Value: v}
		if !k8sutils.IsEnvVarPresent(envVar, spec.Env) {
			spec.Env = append(spec.Env, envVar)
		}
	}
	sort.Sort(k8sutils.SortableEnvVars(spec.Env))
}
