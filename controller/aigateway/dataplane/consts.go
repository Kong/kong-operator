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

	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

const (
	// ControllerName is the name used for logging and event recording.
	ControllerName = "aigw-dataplane"

	// DefaultIngressPort is the default port exposed by the AI Gateway ingress listener.
	DefaultIngressPort int32 = 8443

	// DefaultAdminPort is the default port exposed by the AI Gateway Admin API
	// listener, SSL-terminated. Only exposed when the AIGatewayDataPlane
	// references an OnPremAIGateway.
	DefaultAdminPort int32 = 8444

	// AdminServiceNameSuffix is appended to the AIGatewayDataPlane name to
	// form the admin Service name.
	AdminServiceNameSuffix = "-admin"

	// KonnectCertVolumeName is the name of the volume that holds the Konnect mTLS certificate.
	KonnectCertVolumeName = shareddataplane.KonnectCertVolumeName

	// KonnectCertMountPath is the path where the Konnect certificate Secret is mounted in the AI Gateway container.
	KonnectCertMountPath = shareddataplane.KonnectCertMountPath

	// AdminCertVolumeName is the name of the volume that holds the Admin API TLS server certificate.
	AdminCertVolumeName = shareddataplane.AdminCertVolumeName

	// AdminCertMountPath is the path where the Admin API certificate Secret is mounted in the AI Gateway container.
	AdminCertMountPath = shareddataplane.AdminCertMountPath
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
	// EnvKongAdminListen is the AI Gateway environment variable for the Admin API listener.
	EnvKongAdminListen = "KONG_ADMIN_LISTEN"
	// EnvKongAdminSSLCert is the AI Gateway environment variable for the Admin API TLS server certificate path.
	EnvKongAdminSSLCert = "KONG_ADMIN_SSL_CERT"
	// EnvKongAdminSSLCertKey is the AI Gateway environment variable for the Admin API TLS server key path.
	EnvKongAdminSSLCertKey = "KONG_ADMIN_SSL_CERT_KEY"
	// EnvKongKonnectMode is the AI Gateway environment variable toggling Konnect mode.
	EnvKongKonnectMode = "KONG_KONNECT_MODE"
	// EnvKongNginxAdminSSLClientCertificate is the AI Gateway environment variable for the CA
	// that the Admin API listener verifies client certificates against.
	EnvKongNginxAdminSSLClientCertificate = "KONG_NGINX_ADMIN_SSL_CLIENT_CERTIFICATE"
	// EnvKongNginxAdminSSLVerifyClient is the AI Gateway environment variable enabling
	// client certificate verification on the Admin API listener.
	EnvKongNginxAdminSSLVerifyClient = "KONG_NGINX_ADMIN_SSL_VERIFY_CLIENT"
)

// requiredEnvVars returns the environment variables required to boot the
// AI Gateway regardless of the control plane kind. konnectMode toggles the
// Konnect connectivity mode; the Konnect and on-prem control plane variants
// (RequiredHardcodedEnvVars and RequiredOnPremEnvVars) layer their kind
// specific variables on top of it.
func requiredEnvVars(konnectMode string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "KONG_DATABASE", Value: "off"},
		{Name: "KONG_VITALS", Value: "off"},
		{Name: EnvKongKonnectMode, Value: konnectMode},
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

// RequiredHardcodedEnvVars returns a slice of corev1.EnvVar containing
// the required variables to boot and connect the AI Gateway to Konnect.
func RequiredHardcodedEnvVars() []corev1.EnvVar {
	return append(
		requiredEnvVars("on"),
		corev1.EnvVar{Name: "KONG_ROLE", Value: "data_plane"},
		corev1.EnvVar{Name: "KONG_CLUSTER_MTLS", Value: "pki"},
	)
}

// RequiredOnPremEnvVars returns a slice of corev1.EnvVar containing the
// required variables to boot the AI Gateway with an on-prem control plane
// (an OnPremAIGateway) that pushes configuration to the DataPlane's Admin
// API: no Konnect connectivity and no cluster (hybrid) role. The Admin API
// listener itself (KONG_ADMIN_LISTEN and its TLS certificate) is wired by
// buildAIGatewayEnvVars only when the admin certificate Secret has been
// provisioned, so the SSL listener is never announced without a certificate.
func RequiredOnPremEnvVars() []corev1.EnvVar {
	return requiredEnvVars("off")
}
