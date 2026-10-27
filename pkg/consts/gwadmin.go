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

package consts

import "time"

// -----------------------------------------------------------------------------
// Consts - GWAdmin Labels and Annotations
// -----------------------------------------------------------------------------

const (
	// GWAdminManagedByLabelValue is the managed-by label value for GWAdmin owned resources.
	GWAdminManagedByLabelValue = "gw-admin"

	// GWAdminServiceRoleLabel labels a Service of a GWAdmin with its role:
	// only the API Service carries it, with GWAdminServiceRoleAPI.
	GWAdminServiceRoleLabel = OperatorLabelPrefix + "gwadmin-service-role"
	// GWAdminServiceRoleAPI is the value of GWAdminServiceRoleLabel on the API
	// Service of a GWAdmin.
	GWAdminServiceRoleAPI = "api"

	// GWAdminTLSChecksumAnnotation records a checksum of the TLS material
	// mounted into the gw-admin pods on the Pod template, so that rotating any
	// of it rolls the Deployment.
	GWAdminTLSChecksumAnnotation = "konghq.com/gw-admin-tls-checksum" //nolint:gosec

	// GWAdminTrustChecksumAnnotation records a checksum of the trust bundles
	// mounted into the gw-admin pods on the Pod template, so that Kong
	// Operator knows when the pods trust a new certificate authority.
	GWAdminTrustChecksumAnnotation = "konghq.com/gw-admin-trust-checksum" //nolint:gosec

	// GWAdminRotateCAsAnnotation is the annotation that, set on a GWAdmin,
	// rotates every certificate authority Kong Operator creates for it
	// whenever its value changes, e.g. to replace a compromised one.
	GWAdminRotateCAsAnnotation = OperatorLabelPrefix + "rotate-certificate-authorities"

	// GWAdminUIDLabel labels the Secrets of a GWAdmin with its UID: no owner
	// reference can cross namespaces to those kept in the operator namespace,
	// and it tells the GWAdmin's Secrets the cache doesn't show apart from
	// those of others without reading them.
	GWAdminUIDLabel = OperatorLabelPrefix + "gwadmin-uid"

	// GWAdminRotationSkipDataPlanesAnnotation is the annotation that, set to
	// "true" on a GWAdmin, makes its rotations stop waiting for the data
	// planes to roll out, e.g.
	// to stop trusting a compromised certificate authority right away: data
	// planes may then fail to connect until they roll out.
	GWAdminRotationSkipDataPlanesAnnotation = OperatorLabelPrefix + "rotation-skip-data-planes"

	// GWAdminCleanupFinalizer deletes the Secrets of a GWAdmin kept in the
	// operator namespace.
	GWAdminCleanupFinalizer = OperatorLabelPrefix + "gwadmin-cleanup"

	// GWAdminDataPlaneNamespaceLabel is the label that, set on a data plane
	// Deployment, holds the namespace of the GWAdmin it pulls the
	// configuration from: its rollouts gate the rotations of that GWAdmin.
	// The controller managing the data plane sets it, with
	// GWAdminDataPlaneNameLabel, when it creates the Deployment, and never
	// removes them while the data plane runs: a GWAdmin with no data plane
	// rotates without waiting.
	GWAdminDataPlaneNamespaceLabel = OperatorLabelPrefix + "gwadmin-namespace"
	// GWAdminDataPlaneNameLabel is the label that, set on a data plane
	// Deployment, holds the name of the GWAdmin it pulls the configuration from.
	GWAdminDataPlaneNameLabel = OperatorLabelPrefix + "gwadmin-name"

	// GWAdminDataPlaneServerBundleAnnotation records, on the Pod template of
	// a data plane Deployment, the SHA-256 (hex) of the ca.crt of the cp
	// server bundle its pods mount.
	GWAdminDataPlaneServerBundleAnnotation = "konghq.com/gw-admin-cp-server-bundle-checksum"
	// GWAdminDataPlaneClientCAAnnotation records, on the Pod template of a
	// data plane Deployment, the SHA-256 fingerprint (hex, of the DER) of
	// the cp client CA that issued the client certificate its pods mount.
	GWAdminDataPlaneClientCAAnnotation = "konghq.com/gw-admin-cp-client-ca"
)

// -----------------------------------------------------------------------------
// Consts - GWAdmin Container Parameters
// -----------------------------------------------------------------------------

const (
	// GWAdminContainerName is the name of the gw-admin container.
	GWAdminContainerName = "gw-admin"

	// RelatedImageGWAdminEnvVar is the environment variable name for the
	// gw-admin container image, following the operator-framework convention
	// for related images.
	RelatedImageGWAdminEnvVar = "RELATED_IMAGE_GW_ADMIN"

	// DefaultGWAdminBaseImage is the base image name for the gw-admin container.
	DefaultGWAdminBaseImage = "kong/kong-gw-admin-cp-dev"
	// DefaultGWAdminTag is the default image tag for the gw-admin container.
	// gw-admin has no release yet: the tag pins the dev image by digest, the
	// build of its pull request 1502 serving version 1.11.0 of its API and
	// deleting the data plane certificates of a gateway with it, and must be
	// bumped by hand, to a main-<commit> build once that is merged.
	// TODO: move to a released image with a renovate annotation once gw-admin is released.
	DefaultGWAdminTag = "pr-1502@sha256:eb9db0feab9291065ba185f6fb5841661f39ce93868c311346ae3599d8d0522b"
	// DefaultGWAdminImage is the full default image reference for the gw-admin container.
	DefaultGWAdminImage = DefaultGWAdminBaseImage + ":" + DefaultGWAdminTag

	// GWAdminAPIPortName is the name of the port Kong Operator pushes the configuration to.
	GWAdminAPIPortName = "api"
	// GWAdminAPIPort is the port Kong Operator pushes the configuration to.
	GWAdminAPIPort = 8083
	// GWAdminCPPortName is the name of the port the data planes pull the configuration from.
	GWAdminCPPortName = "cp"
	// GWAdminCPPort is the port the data planes pull the configuration from.
	GWAdminCPPort = 8099
	// GWAdminOpsPortName is the name of the port serving the health probes.
	GWAdminOpsPortName = "ops"
	// GWAdminOpsPort is the port serving the health probes.
	GWAdminOpsPort = 9090

	// GWAdminLivenessPath is the path of the liveness probe.
	GWAdminLivenessPath = "/health/probes/liveness"
	// GWAdminReadinessPath is the path of the readiness probe.
	GWAdminReadinessPath = "/health/probes/readiness"

	// GWAdminAPITLSMountPath is where the TLS material of the API listener is mounted.
	GWAdminAPITLSMountPath = "/etc/gw-admin/api-tls"
	// GWAdminCPTLSMountPath is where the TLS material of the cp listener is mounted.
	GWAdminCPTLSMountPath = "/etc/gw-admin/cp-tls"

	// GWAdminAPITLSCertFileEnvVar points gw-admin at the API listener certificate.
	GWAdminAPITLSCertFileEnvVar = "GW_ADMIN_ONPREM_API_TLS_CERT_FILE"
	// GWAdminAPITLSKeyFileEnvVar points gw-admin at the API listener key.
	GWAdminAPITLSKeyFileEnvVar = "GW_ADMIN_ONPREM_API_TLS_KEY_FILE"
	// GWAdminAPITLSClientCAFileEnvVar points gw-admin at the CAs trusted for API clients.
	GWAdminAPITLSClientCAFileEnvVar = "GW_ADMIN_ONPREM_API_TLS_CLIENT_CA_FILE"
	// GWAdminCPTLSCertFileEnvVar points gw-admin at the cp listener certificate.
	GWAdminCPTLSCertFileEnvVar = "GW_ADMIN_ONPREM_CP_TLS_CERT_FILE"
	// GWAdminCPTLSKeyFileEnvVar points gw-admin at the cp listener key.
	GWAdminCPTLSKeyFileEnvVar = "GW_ADMIN_ONPREM_CP_TLS_KEY_FILE"
	// GWAdminCPTLSClientCAFileEnvVar points gw-admin at the CAs trusted for cp clients.
	GWAdminCPTLSClientCAFileEnvVar = "GW_ADMIN_ONPREM_CP_TLS_CLIENT_CA_FILE"
)

// -----------------------------------------------------------------------------
// Consts - GWAdmin Certificates
// -----------------------------------------------------------------------------

const (
	// GWAdminCATTL is the default validity of the certificate authorities
	// Kong Operator creates for a GWAdmin (--gwadmin-ca-ttl). They are
	// renewed once two thirds of it have elapsed, so that rotations run
	// regularly.
	GWAdminCATTL = 365 * 24 * time.Hour
	// GWAdminLeafTTL is the default validity of the certificates Kong
	// Operator issues for a GWAdmin (--gwadmin-cert-ttl). They are renewed
	// once two thirds of it have elapsed.
	GWAdminLeafTTL = 90 * 24 * time.Hour
	// GWAdminMinTTL is the shortest validity --gwadmin-ca-ttl and
	// --gwadmin-cert-ttl accept: certificates are backdated by 5 minutes
	// against clock skew, which a shorter validity would mostly consume.
	GWAdminMinTTL = 10 * time.Minute
	// GWAdminOperatorClientCommonName is the common name of the client
	// certificate Kong Operator authenticates to gw-admin with.
	GWAdminOperatorClientCommonName = "kong-operator"
)
