/*
Copyright 2025 Kong, Inc.

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

const (
	// FieldManager is the field manager name used for Server-Side Apply operations.
	FieldManager = "gateway-operator"

	// ControllerName is the name used for logging and event recording.
	ControllerName = "keg-dataplane"

	// DefaultKafkaPort is the default port exposed by the Kafka listener.
	DefaultKafkaPort int32 = 9092

	// DefaultHealthPort is the default port used for the keg health endpoint.
	DefaultHealthPort int32 = 8080

	// KonnectCertVolumeName is the name of the volume that holds the Konnect mTLS certificate.
	KonnectCertVolumeName = "konnect-cert"

	// KonnectCertMountPath is the path where the Konnect certificate Secret is mounted in the keg container.
	KonnectCertMountPath = "/var/konnect-client-certificate/"
)

// -----------------------------------------------------------------------------
// Consts - KEG environment variable names
// Konnect settings use the KONG_KONNECT_ prefix; all other keg settings use
// the KEG__ prefix with double underscores for nesting.
// -----------------------------------------------------------------------------

const (
	// EnvKonnectRegion is the keg environment variable for the Konnect region.
	EnvKonnectRegion = "KONG_KONNECT_REGION"
	// EnvKonnectGatewayClusterID is the keg environment variable for the Konnect gateway cluster ID.
	EnvKonnectGatewayClusterID = "KONG_KONNECT_GATEWAY_CLUSTER_ID"
	// EnvKonnectClientCertPath is the keg environment variable for the Konnect mTLS client certificate path.
	EnvKonnectClientCertPath = "KONG_KONNECT_CLIENT_CERT_PATH"
	// EnvKonnectClientKeyPath is the keg environment variable for the Konnect mTLS client key path.
	EnvKonnectClientKeyPath = "KONG_KONNECT_CLIENT_KEY_PATH"
	// EnvKonnectDomain is the keg environment variable for the Konnect domain override.
	EnvKonnectDomain = "KONG_KONNECT_DOMAIN"
	// EnvKonnectAPIRequestTimeout is the keg environment variable for the Konnect API request timeout.
	EnvKonnectAPIRequestTimeout = "KONG_KONNECT_API_REQUEST_TIMEOUT"
	// EnvKonnectInsecureSkipVerify is the keg environment variable to skip TLS verification.
	// For testing only.
	EnvKonnectInsecureSkipVerify = "KONG_KONNECT_INSECURE_SKIP_VERIFY"

	// EnvConfigPollInterval is the keg environment variable for the config poll interval.
	EnvConfigPollInterval = "KEG__CONFIG_POLL_INTERVAL"
	// EnvEnableDebugEndpoints is the keg environment variable to enable debug endpoints.
	EnvEnableDebugEndpoints = "KEG__ENABLE_DEBUG_ENDPOINTS"

	// EnvObsLogFlags is the keg environment variable for log level flags.
	EnvObsLogFlags = "KEG__OBSERVABILITY__LOG_FLAGS"
	// EnvObsLogFormat is the keg environment variable for the log output format.
	EnvObsLogFormat = "KEG__OBSERVABILITY__LOG_FORMAT"
	// EnvObsMetricsRollupAllowMap is the keg environment variable for the metrics rollup allow map.
	EnvObsMetricsRollupAllowMap = "KEG__OBSERVABILITY__METRICS_ROLLUP_ALLOW_MAP"
	// EnvObsPolicyErrorsInfoLogInterval is the keg environment variable for the policy errors info log interval.
	EnvObsPolicyErrorsInfoLogInterval = "KEG__OBSERVABILITY__POLICY_ERRORS_INFO_LOG_INTERVAL"

	// EnvRuntimeHealthAddr is the keg environment variable for the health listener address and port.
	EnvRuntimeHealthAddr = "KEG__RUNTIME__HEALTH_LISTENER_ADDRESS_PORT"
	// EnvRuntimeDrainDuration is the keg environment variable for the drain duration.
	EnvRuntimeDrainDuration = "KEG__RUNTIME__DRAIN_DURATION"
	// EnvRuntimeShutdownTimeout is the keg environment variable for the graceful shutdown timeout.
	EnvRuntimeShutdownTimeout = "KEG__RUNTIME__SHUTDOWN_TIMEOUT"
)
