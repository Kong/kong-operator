package dataplane

import (
	"fmt"
	"sort"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
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
	"KONG_PROXY_LISTEN":           fmt.Sprintf("0.0.0.0:%d reuseport backlog=16384, 0.0.0.0:%d http2 ssl reuseport backlog=16384", consts.DataPlaneProxyPort, consts.DataPlaneProxySSLPort),
	"KONG_STATUS_LISTEN":          fmt.Sprintf("0.0.0.0:%d", consts.DataPlaneStatusPort),

	// TODO: reconfigure following https://github.com/Kong/gateway-operator/issues/7
	"KONG_ADMIN_LISTEN": fmt.Sprintf("0.0.0.0:%d ssl reuseport backlog=16384", consts.DataPlaneAdminAPIPort),

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
// returns true if new envs are actually appended.
func SetDataPlaneDefaults(spec *operatorv1beta1.DataPlaneOptions) bool {
	if spec.Deployment.PodTemplateSpec == nil {
		spec.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
	}

	dataplaneContainer := k8sutils.GetPodContainerByName(&spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	generated := false
	if dataplaneContainer == nil {
		dataplaneContainer = lo.ToPtr(resources.GenerateDataPlaneContainer(""))
		generated = true
	}

	updated := false
	for k, v := range KongDefaults {
		envVar := corev1.EnvVar{Name: k, Value: v}
		if !k8sutils.IsEnvVarPresent(envVar, dataplaneContainer.Env) {
			dataplaneContainer.Env = append(dataplaneContainer.Env, envVar)
			updated = true
		}
	}
	sort.Sort(k8sutils.SortableEnvVars(dataplaneContainer.Env))
	if generated {
		spec.Deployment.PodTemplateSpec.Spec.Containers = append(spec.Deployment.PodTemplateSpec.Spec.Containers, *dataplaneContainer)
	}
	return updated
}
