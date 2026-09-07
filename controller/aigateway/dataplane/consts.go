/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataplane

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

const (
	// ControllerName is the name used for logging and event recording.
	ControllerName = "aigw-dataplane"

	// DefaultIngressPort is the default port exposed by the AI Gateway ingress listener.
	DefaultIngressPort int32 = 8443

	// KonnectCertVolumeName is the name of the volume that holds the Konnect mTLS certificate.
	KonnectCertVolumeName = "konnect-cert"

	// KonnectCertMountPath is the path where the Konnect certificate Secret is mounted in the AI Gateway container.
	KonnectCertMountPath = "/var/konnect-client-certificate/"
)

// -----------------------------------------------------------------------------
// Consts - AI Gateway environment variable names
// -----------------------------------------------------------------------------

const (
	// EnvKongClusterControlPlane is the AI Gateway environment variable for the Konnect control plane endpoint (host:port).
	EnvKongClusterControlPlane = "KONG_CLUSTER_CONTROL_PLANE"
	// EnvKongClusterServerName is the AI Gateway environment variable for the Konnect control plane TLS server name.
	EnvKongClusterServerName = "KONG_CLUSTER_SERVER_NAME"
	// EnvKongClusterTelemetryEndpoint is the AI Gateway environment variable for the Konnect telemetry endpoint (host:port).
	EnvKongClusterTelemetryEndpoint = "KONG_CLUSTER_TELEMETRY_ENDPOINT"
	// EnvKongClusterTelemetryServerName is the AI Gateway environment variable for the Konnect telemetry TLS server name.
	EnvKongClusterTelemetryServerName = "KONG_CLUSTER_TELEMETRY_SERVER_NAME"
	// EnvClientCertPath is the AI Gateway environment variable for the Konnect mTLS client certificate path.
	EnvClientCertPath = "KONG_CLUSTER_CERT"
	// EnvKonnectClientCertKey is the AI Gateway environment variable for the Konnect mTLS client key path.
	EnvKonnectClientCertKey = "KONG_CLUSTER_CERT_KEY"
)

// RequiredHardcodedEnvVars returns a slice of corev1.EnvVar containing
// the required variables to boot and connect the AI Gateway to Konnect.
func RequiredHardcodedEnvVars() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "KONG_ROLE", Value: "data_plane"},
		{Name: "KONG_DATABASE", Value: "off"},
		{Name: "KONG_CLUSTER_MTLS", Value: "pki"},
		{Name: "KONG_VITALS", Value: "off"},
		{Name: "KONG_KONNECT_MODE", Value: "on"},
		{Name: "KONG_LUA_SSL_TRUSTED_CERTIFICATE", Value: "system"},
		{Name: "KONG_STATUS_LISTEN", Value: fmt.Sprintf("0.0.0.0:%d", consts.DataPlaneStatusPort)},
		{Name: "KONG_PROXY_ACCESS_LOG", Value: "/dev/stdout"},
		{Name: "KONG_PROXY_ERROR_LOG", Value: "/dev/stderr"},
		{Name: "KONG_ADMIN_ACCESS_LOG", Value: "/dev/stdout"},
		{Name: "KONG_ADMIN_ERROR_LOG", Value: "/dev/stderr"},
		{Name: "KONG_ADMIN_GUI_ACCESS_LOG", Value: "/dev/stdout"},
		{Name: "KONG_ADMIN_GUI_ERROR_LOG", Value: "/dev/stderr"},
	}
}
